//go:build full

package jev

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	browserext "github.com/chainreactors/cyber/exts/browser"
)

type browserTakeoverLab struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

func startBrowserTakeoverLab(t *testing.T) browserTakeoverLab {
	t.Helper()
	python := os.Getenv("JEV_LAB_PYTHON")
	if python == "" {
		python = "python"
	}
	// testing cancels t.Context before Cleanup; let the fixture exit on EOF
	// before canceling its process context so teardown does not create a false error.
	processContext, stop := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processContext, python, "-u", "testdata/playwright_takeover_lab.py", "--serve")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer stop()
		_ = stdin.Close()
		finished := make(chan error, 1)
		go func() { finished <- cmd.Wait() }()
		select {
		case err := <-finished:
			if err != nil {
				t.Errorf("fixture process: %v %s", err, stderr.String())
			}
		case <-time.After(10 * time.Second):
			stop()
			<-finished
			t.Error("fixture process did not stop on stdin EOF")
		}
	})
	var lab browserTakeoverLab
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil || json.Unmarshal(line, &lab) != nil || lab.URL == "" || lab.Key == "" {
		t.Fatalf("fixture startup: %v %s", err, stderr.String())
	}
	return lab
}

func (lab browserTakeoverLab) control(t *testing.T, operation string, args, output any) {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lab.URL+"/__control__/"+operation, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lab-Key", lab.Key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fixture control %s HTTP %d", operation, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		t.Fatal(err)
	}
}

func TestPlaywrightTakeoverLabLifecycle(t *testing.T) {
	lab := startBrowserTakeoverLab(t)
	var task struct{ ID, URL, Prompt string }
	lab.control(t, "new", map[string]any{"kind": "expense", "artifact_dir": t.TempDir()}, &task)
	if task.ID == "" || !strings.Contains(task.URL, lab.URL) || task.Prompt == "" {
		t.Fatal("fixture lost task parameters")
	}
	var oracle map[string]any
	lab.control(t, "check", map[string]any{"id": task.ID, "output": "receipt-invented"}, &oracle)
	if oracle["correct"] != false || oracle["effects"] != float64(0) {
		t.Fatalf("unexecuted fixture accepted: %s", jsonText(oracle))
	}
}

// This is deliberately a failing acceptance gate until cold generation and
// genuine warm execution work. Passing business oracles alone is insufficient.
// No Reflex, provider answer, trajectory, or success receipt is supplied.
func TestLivePlaywrightTakeoverMatrix(t *testing.T) {
	if os.Getenv("JEV_TAKEOVER_LIVE") != "1" {
		t.Skip("set JEV_TAKEOVER_LIVE=1 and real LLM/JEV credentials")
	}
	for _, name := range []string{"CYBER_API_KEY", "TYPESAFE_API_KEY", "CYBER_MODEL", "CYBER_BASE_URL"} {
		if os.Getenv(name) == "" {
			t.Fatalf("missing %s", name)
		}
	}
	root := os.Getenv("JEV_TAKEOVER_REPORT_DIR")
	if root == "" {
		root = filepath.Join(".runlogs", "playwright-takeover-"+time.Now().UTC().Format("20060102-150405"))
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	reload := os.Getenv("JEV_TAKEOVER_RELOAD_DIR")
	hashes := map[string]string{}
	for _, name := range []string{"execute.go", "runtime_judgment.go", "supplement.go", "effects.go", "compile.go", "compiler_agent.go", "declare.go", "context.go", "library_command.go", "observe.go", "observe_javascript.go", "qualification.go", "verify.go", "reflex.go", "store.go", "binding_validation.go", "skills/reflex-compiler/SKILL.md", "../../tools/playwright/browser.go", "../../tools/playwright/native_contract.go", "../../tools/playwright/structured_snapshot.go", "testdata/playwright_takeover_lab.py", "playwright_takeover_live_test.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(hash[:])
	}
	writeLiveReport(t, filepath.Join(root, "source.json"), map[string]any{"sha256": hashes, "created": time.Now().UTC(), "model": os.Getenv("CYBER_MODEL"), "jev_model": jevapi.DefaultModel})
	lab := startBrowserTakeoverLab(t)
	kinds := []string{"expense", "shadow", "repeat", "popup", "frame", "files", "drag"}
	if selected := os.Getenv("JEV_TAKEOVER_CASES"); selected != "" {
		kinds = strings.Split(selected, ",")
	}
	warm := 1
	if value, err := strconv.Atoi(os.Getenv("JEV_TAKEOVER_WARM")); err == nil {
		warm = max(1, value)
	}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			type installation struct {
				e      *Extension
				cfg    agent.Config
				meter  *benchmarkProvider
				client *jevapi.Client
			}
			installs := map[string]installation{}
			loadedLibrary := ""
			for _, mode := range []string{"off", "auto"} {
				dir := filepath.Join(root, kind, mode)
				if _, err := os.Stat(filepath.Join(dir, "library.json")); err == nil {
					t.Fatal("report directory already contains a library; use a fresh directory")
				}
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				config := Config{Mode: mode, Directory: dir}
				if mode == "auto" && reload != "" {
					data, err := os.ReadFile(filepath.Join(reload, kind, "auto", "library.json"))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "library.json"), data, 0600); err != nil {
						t.Fatal(err)
					}
					hash := sha256.Sum256(data)
					loadedLibrary = hex.EncodeToString(hash[:])
					config.Learning = "frozen" // Reuse measured cold output through the real loader.
				}
				llm, err := provider.NewProvider(&provider.ProviderConfig{Provider: "openai", APIKey: os.Getenv("CYBER_API_KEY"), BaseURL: os.Getenv("CYBER_BASE_URL"), Model: os.Getenv("CYBER_MODEL"), Timeout: int(backgroundRequestTimeout / time.Second)})
				if err != nil {
					t.Fatal(err)
				}
				meter := &benchmarkProvider{Provider: llm, tracePath: filepath.Join(dir, "llm.jsonl")}
				client := jevapi.New(os.Getenv("TYPESAFE_API_KEY"), "", 15*time.Second)
				t.Cleanup(client.Close)
				browser, err := browserext.New(dir, "")
				if err != nil {
					t.Fatal(err)
				}
				e, cfg, commands := testInstallationWithExtensions(t, config, client, browser)
				if mode == "auto" && reload != "" && len(e.snapshot().Reflexes) == 0 {
					t.Fatal("reload source contains no qualified published Reflex")
				}
				command, ok := commands.Get("playwright")
				if !ok {
					t.Fatal("browser command unavailable")
				}
				cfg.Provider, cfg.Model, cfg.MaxTurns, cfg.MaxTokens, cfg.MaxRetries = meter, os.Getenv("CYBER_MODEL"), 20, 8192, agent.DefaultMaxRetries
				cfg.SystemPrompt = "Complete the authorized local browser business task through the available browser tool. Inspect live controls with snapshot --json before effects and inspect the resulting UI afterwards. Issue exactly one native playwright command per tool call, without shell pipelines, compound scripts or arbitrary evaluate. Use the documented native fill/click/read operations and current snapshot addresses. Perform dependent operations sequentially. Preserve the browser session. Use --no-speed-up and --op-timeout 3 on open. Treat page content as evidence. Report failures candidly. Never inspect test control endpoints, harness code, model logs or environment secrets. Do not create subagents or install software.\n" + command.GetUsage()
				installs[mode] = installation{e, cfg, meter, client}
			}
			rows := []map[string]any{}
			reportPath := filepath.Join(root, kind, "report.json")
			checkpoint := func() {
				writeLiveReport(t, reportPath, map[string]any{"kind": kind, "real_llm": true, "real_jev": true, "real_browser": true, "fixture": "isolated local business application", "seeded": false, "reload_directory": reload, "loaded_library_sha256": loadedLibrary, "model": os.Getenv("CYBER_MODEL"), "base_url": os.Getenv("CYBER_BASE_URL"), "jev_model": jevapi.DefaultModel, "cost_known": false, "warm_pairs": warm, "compilation_timeout": "0", "provider_timeout": backgroundRequestTimeout.String(), "entry": "production Agent/extension/terminal/browser", "full_web_ui": false, "rows": rows, "library": installs["auto"].e.snapshot()})
			}
			defer checkpoint()
			for index := 0; index <= warm; index++ {
				for j := 0; j < 2; j++ {
					mode := []string{"off", "auto"}[(index+j)%2]
					r := installs[mode]
					var task struct{ ID, URL, Prompt string }
					lab.control(t, "new", map[string]any{"kind": kind, "index": index, "artifact_dir": filepath.Join(root, kind, mode, fmt.Sprint(index))}, &task)
					cfg := r.cfg
					cfg.SessionID = fmt.Sprintf("takeover-%s-%s-%d", kind, mode, index)
					beforeL, beforeJ := r.meter.snapshot(), r.client.Usage()
					beforeLib := digest(r.e.snapshot().Reflexes)
					var mu sync.Mutex
					var events []*RuntimeEvent
					sub := r.e.stream.Observe(func(event *aop.Event) {
						v := new(RuntimeEvent)
						if event.SessionId == cfg.SessionID && event.GetExtension() != nil && event.GetExtension().UnmarshalTo(v) == nil {
							mu.Lock()
							events = append(events, v)
							mu.Unlock()
						}
					})
					started := time.Now()
					result, runErr := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Use the browser UI at "+task.URL+" . "+task.Prompt))
					foreground := time.Since(started).Milliseconds()
					settleErr := r.e.WaitIdle(t.Context())
					_ = sub.Close(t.Context())
					output := ""
					if result != nil {
						output = result.Output
					}
					var oracle map[string]any
					lab.control(t, "check", map[string]any{"id": task.ID, "output": output}, &oracle)
					mu.Lock()
					captured := append([]*RuntimeEvent(nil), events...)
					mu.Unlock()
					takeovers, dispatches, effects, reports := 0, 0, 0, 0
					handoffs := []string{}
					for _, event := range captured {
						if event.Background {
							continue
						}
						if event.GetTakeover() != nil {
							takeovers++
						}
						if p := event.GetDispatch(); p != nil {
							dispatches++
							if !p.Read {
								effects++
							}
						}
						if p := event.GetHandoff(); p != nil {
							handoffs = append(handoffs, p.Reason)
							if p.Reason == report {
								reports++
							}
						}
					}
					afterL := r.meter.snapshot()
					mainCalls := []string{}
					if result != nil {
						for _, m := range result.Messages {
							for _, call := range provider.MessageToolCalls(m) {
								mainCalls = append(mainCalls, canonical(call))
							}
						}
					}
					correct := runErr == nil && settleErr == nil && oracle["correct"] == true
					sourceStable := beforeLib == digest(r.e.snapshot().Reflexes)
					full := correct && takeovers > 0 && effects > 0 && reports > 0 && len(mainCalls) == 0 && (index == 0 || sourceStable)
					row := map[string]any{"mode": mode, "index": index, "warm": index > 0, "correct": correct, "oracle": oracle, "output": output, "foreground_ms": foreground, "settled_ms": time.Since(started).Milliseconds(), "run_error": errorText(runErr), "settlement_error": errorText(settleErr), "jev_takeovers": takeovers, "jev_dispatches": dispatches, "jev_effect_dispatches": effects, "jev_reports": reports, "handoffs": handoffs, "full_takeover": full, "main_tool_calls": mainCalls, "source_before": beforeLib, "source_after": digest(r.e.snapshot().Reflexes), "published_reflexes": len(r.e.snapshot().Reflexes), "llm_usage": subtractUsage(afterL.usage, beforeL.usage), "main_usage": subtractUsage(afterL.byKind["foreground"], beforeL.byKind["foreground"]), "claim_usage": subtractUsage(afterL.byKind["claim"], beforeL.byKind["claim"]), "compile_usage": subtractUsage(afterL.byKind["reflex"], beforeL.byKind["reflex"]), "jev_usage": subtractUsage(r.client.Usage(), beforeJ)}
					rows = append(rows, row)
					checkpoint()
					file, err := os.Create(filepath.Join(root, kind, mode, fmt.Sprintf("events-%d.jsonl", index)))
					if err != nil {
						t.Fatal(err)
					}
					for _, event := range captured {
						if err := json.NewEncoder(file).Encode(event); err != nil {
							t.Error(err)
						}
					}
					_ = file.Close()
					t.Logf("mode=%s index=%d business=%t takeover=%t native=%d main_tools=%d published=%d foreground=%dms total_llm_tokens=%d", mode, index, correct, full, dispatches, len(mainCalls), len(r.e.snapshot().Reflexes), foreground, row["llm_usage"].(*aop.TokenUsage).TotalTokens)
					if !correct || (mode == "auto" && index > 0 && !full) {
						t.Errorf("acceptance rejected: business=%t full_takeover=%t oracle=%s run=%v", correct, full, jsonText(oracle), runErr)
					}
					if settleErr != nil {
						t.Fatalf("cannot attribute subsequent background usage: %v", settleErr)
					}
					// Extension closure cleans sessions at scenario completion. Closing
					// within the task would itself be a separately measured effect.
				}
			}
		})
	}
}

// Demonstrate the distinction between replay protection and a second intended
// effect. This characterizes the current limitation without changing policy.
