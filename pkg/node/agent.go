package node

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"strings"

	"github.com/chainreactors/cyber/agent"
	agentsession "github.com/chainreactors/cyber/agent/session"
	aop "github.com/chainreactors/cyber/aop"
	filepb "github.com/chainreactors/cyber/aop/file"
	"github.com/chainreactors/cyber/core/telemetry"
	types "github.com/chainreactors/cyber/core/types"
	cfg "github.com/chainreactors/cyber/pkg/config"
	"github.com/chainreactors/cyber/pkg/console"
	profile "github.com/chainreactors/cyber/pkg/profile"
)

func RunWebSocket(ctx context.Context, newProfile func(profile.Request) (profile.Profile, error), option *cfg.Option, logger telemetry.Logger) error {
	return RunWebSocketWithCapabilities(ctx, newProfile, option, logger)
}

// RunWebSocketWithCapabilities is the profile-aware transport entry point.
// Capabilities travel in the enrollment hello and remain independent of the
// core node protocol.
func RunWebSocketWithCapabilities(ctx context.Context, newProfile func(profile.Request) (profile.Profile, error), option *cfg.Option, logger telemetry.Logger, capabilities ...string) error {
	if err := cfg.ResolveAgentServerURLs(option); err != nil {
		return fmt.Errorf("resolve remote agent URLs: %w", err)
	}
	nodeID, err := webNodeID(option)
	if err != nil {
		return err
	}
	if newProfile == nil {
		return fmt.Errorf("profile constructor is required")
	}
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	build := func(options *cfg.Option, mode profile.ProviderMode) (profile.Profile, error) {
		p, err := newProfile(profile.Request{Option: options, ProviderMode: mode, Logger: logger, Session: &agentsession.Config{PrimarySessionID: console.MainREPLName, Loop: agent.StandardLoop{}}})
		if err == nil && p == nil {
			err = fmt.Errorf("profile constructor returned nil")
		}
		if err == nil {
			err = p.Load(ctx)
		}
		if err == nil {
			_, err = p.Runtime()
		}
		if err == nil {
			_, err = p.Processes()
		}
		if err != nil {
			if p != nil {
				_ = p.Close(context.Background())
			}
			return nil, err
		}
		return p, nil
	}
	// Enroll without initializing or probing a local model. Only a server
	// configuration can enable the provider and release the startup task.
	current, err := build(option, profile.ProviderDisabled)
	if err != nil {
		return err
	}
	defer func() { _ = current.Close(context.Background()) }()
	currentOption := option
	currentConfig, err := cfg.SharedFromOption(currentOption)
	if err != nil {
		return err
	}
	configured := false
	started := false
	for {
		var candidate profile.Profile
		var candidateOption *cfg.Option
		var candidateConfig *types.DistributeConfig
		err := func() error {
			connectionCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			rt, err := current.Runtime()
			if err != nil {
				return err
			}
			processes, err := current.Processes()
			if err != nil {
				return err
			}
			repl, err := console.StartPersistent(rt, processes, currentOption, current.ConsoleBindings())
			if err != nil {
				return err
			}
			defer repl.Close()
			progress, err := current.Progress()
			if err != nil {
				return err
			}
			events, err := current.Events()
			if err != nil {
				return err
			}
			startTask := func() error {
				if started {
					return nil
				}
				providers, err := current.Providers()
				if err != nil {
					return err
				}
				if active, _ := providers.Current(); active == nil {
					return nil
				}
				task, err := webAgentTask(option)
				if err != nil {
					return err
				}
				if task != "" {
					if _, err := rt.EnsureSession(agentsession.SessionOptions{ID: "startup"}); err != nil {
						return err
					}
					if _, err := rt.RunSession(connectionCtx, "startup", agentsession.RunInput{TurnID: "startup", Content: []*aop.Content{aop.Text(task)}}); err != nil {
						return err
					}
				}
				started = true
				return nil
			}
			// Graph changes prepare a complete candidate; provider-only changes
			// are applied in place so sessions retain their conversation history.
			reload := func(distributed *types.DistributeConfig) (*types.ReloadResult, *aop.AgentStatus) {
				nextOption, err := cfg.ResolveDistributedRuntime(distributed, option)
				if err != nil {
					return &types.ReloadResult{Error: err.Error()}, nil
				}
				nextConfig, err := cfg.SharedFromOption(nextOption)
				if err != nil {
					return &types.ReloadResult{Error: err.Error()}, nil
				}
				unchanged := proto.Equal(currentConfig, nextConfig)
				if configured && unchanged {
					currentOption = nextOption
					return reloadStatus(current)
				}
				if mode, onlyMode := cfg.GuardrailModeChange(currentConfig, nextConfig); configured && onlyMode {
					if target, ok := current.(interface{ SetGuardrailMode(string) error }); ok {
						if err := target.SetGuardrailMode(mode); err != nil {
							return &types.ReloadResult{Error: err.Error()}, nil
						}
						currentOption, currentConfig = nextOption, nextConfig
						return reloadStatus(current)
					}
				}
				// Rebuilding a profile for a model/API update cancels live runs and
				// drops their in-memory transcript. Delegate to the provider owner;
				// active turns keep their provider snapshot and later turns use the
				// new one.
				before, after := proto.CloneOf(currentConfig), proto.CloneOf(nextConfig)
				before.Llm, after.Llm = nil, nil
				if !unchanged && proto.Equal(before, after) {
					if reloader, ok := current.(interface {
						ReloadProvider(context.Context, agent.ProviderConfig) error
					}); ok {
						if reloadErr := reloader.ReloadProvider(connectionCtx, cfg.ProviderConfig(nextOption)); reloadErr != nil {
							return &types.ReloadResult{Error: reloadErr.Error()}, nil
						}
						currentOption, currentConfig, configured = nextOption, nextConfig, true
						if err := startTask(); err != nil {
							return &types.ReloadResult{Error: err.Error()}, nil
						}
						return reloadStatus(current)
					}
					return &types.ReloadResult{Error: "provider extension does not support live configuration; restart the node to apply it"}, nil
				}
				mode := profile.ProviderOptional
				if len(distributed.GetLlm().GetProviders()) == 0 {
					mode = profile.ProviderDisabled
				}
				next, err := build(nextOption, mode)
				if err != nil {
					return &types.ReloadResult{Error: err.Error()}, nil
				}
				result, status := reloadStatus(next)
				if status.GetConfigError() != "" {
					result.Ok = false
					result.Error = status.ConfigError
				}
				if !result.Ok {
					_ = next.Close(context.Background())
					return result, nil
				}
				candidate, candidateOption, candidateConfig = next, nextOption, nextConfig
				return result, status
			}
			commit := func() {
				if candidate != nil {
					cancel()
				}
			}
			if err := startTask(); err != nil {
				return err
			}
			return connect(connectionCtx, connectionConfig{
				ServerURL: option.ServerURL, Name: rt.NodeName(), Executor: rt.Tools(), Events: events,
				Progress: progress, Logger: logger, Upload: uploadNodeFile, ReloadConfig: reload, CommitReload: commit, NodeID: nodeID, Runtime: DefaultRuntimeInfo(),
				Capabilities: capabilities,
				Status:       current.AgentStatus, Menu: func() []*types.CommandSpec { return CommandSpecs(rt) }, RegisterNamespaces: current.RegisterNamespaces,
			})
		}()
		if candidate == nil {
			return err
		}
		if err := current.Close(context.Background()); err != nil {
			logger.Warnf("close previous profile: %v", err)
		}
		current, currentOption, currentConfig, configured = candidate, candidateOption, candidateConfig, true
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.Infof("configuration activated; previous profile and connections closed")
	}
}

func reloadStatus(p profile.Profile) (*types.ReloadResult, *aop.AgentStatus) {
	providers, err := p.Providers()
	if err != nil {
		return &types.ReloadResult{Error: err.Error()}, nil
	}
	active, resolved := providers.Current()
	result := &types.ReloadResult{Ok: true, Model: resolved.Model}
	if active != nil {
		result.Provider = active.Name()
	}
	return result, p.AgentStatus()
}

func uploadNodeFile(req *filepb.UploadRequest) (*filepb.Result, error) {
	if req == nil {
		return nil, fmt.Errorf("upload request is required")
	}
	filename := filepath.Base(strings.TrimSpace(req.Filename))
	if filename == "." || filename == "" {
		filename = "upload"
	}
	dir := filepath.Join(os.TempDir(), "cyber-uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dest := filepath.Join(dir, filename)
	if err := os.WriteFile(dest, req.Data, 0o644); err != nil {
		return nil, err
	}
	return &filepb.Result{Filename: filename, Path: dest, Size: int64(len(req.Data))}, nil
}

// ---------------------------------------------------------------------------
// Startup helpers
// ---------------------------------------------------------------------------

func webAgentTask(option *cfg.Option) (string, error) {
	if option == nil {
		return "", nil
	}
	if strings.TrimSpace(option.Prompt) == "" && option.TaskFile == "" && len(option.Inputs) == 0 {
		return "", nil
	}
	return cfg.ResolveTask(option)
}

func webNodeID(option *cfg.Option) (string, error) {
	if option == nil {
		return "", fmt.Errorf("web node configuration is required")
	}
	if nodeID := strings.TrimSpace(option.NodeID); nodeID != "" {
		return nodeID, nil
	}
	if nodeID := strings.TrimSpace(option.NodeName); nodeID != "" {
		return nodeID, nil
	}
	return "", fmt.Errorf("node_id is required; set --node-id or --node-name")
}
