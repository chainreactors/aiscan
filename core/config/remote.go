package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/webproto"
)

// FetchRemoteConfig contacts the aiscan web server and returns an Option
// populated with the server-managed configuration. The caller merges it
// with local config (local wins).
func FetchRemoteConfig(webURL string) (*Option, error) {
	url := strings.TrimRight(webURL, "/") + "/api/config/distribute"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch remote config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote config: HTTP %d", resp.StatusCode)
	}

	var dc webproto.DistributeConfig
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("decode remote config: %w", err)
	}
	return DistributeToOption(&dc), nil
}

// DistributeToOption converts a server-managed config payload into runtime
// options. It is the shared conversion path for initial remote fetches and
// in-process config pushes.
func DistributeToOption(d *webproto.DistributeConfig) *Option {
	if d == nil {
		return &Option{}
	}
	opt := ApplyDistributeConfig(Option{}, *d)
	return &opt
}

// ApplyDistributeConfig overlays a server-managed config payload onto base. IOA
// launch identity is preserved when the payload omits it, which lets web-spawned
// agents keep their existing registration name across global config pushes.
func ApplyDistributeConfig(base Option, d webproto.DistributeConfig) Option {
	base.Provider = d.LLM.Provider
	base.BaseURL = d.LLM.BaseURL
	base.APIKey = d.LLM.APIKey
	base.Model = d.LLM.Model
	base.LLMProxy = d.LLM.Proxy
	base.CyberhubURL = d.Cyberhub.URL
	base.CyberhubKey = d.Cyberhub.Key
	base.CyberhubMode = d.Cyberhub.Mode
	base.Proxy = d.Cyberhub.Proxy
	base.FofaEmail = d.Recon.FofaEmail
	base.FofaKey = d.Recon.FofaKey
	base.HunterToken = d.Recon.HunterToken
	base.HunterAPIKey = d.Recon.HunterAPIKey
	base.ReconProxy = d.Recon.Proxy
	base.ReconLimit = d.Recon.Limit
	base.ScanConfig.Verify = d.Scan.Verify
	base.ScanConfig.VerifyTimeout = d.Scan.VerifyTimeout
	base.IOAURL = ResolveString(d.IOA.URL, base.IOAURL)
	base.IOAToken = ResolveString(d.IOA.Token, base.IOAToken)
	base.IOANodeName = ResolveString(base.IOANodeName, d.IOA.NodeName)
	base.Space = ResolveString(d.IOA.Space, base.Space)
	base.Tools = append([]string(nil), d.Agent.Tools...)
	if d.Agent.Timeout > 0 {
		base.Timeout = d.Agent.Timeout
	}
	base.SaveSession = d.Agent.SaveSession
	if d.Search.TavilyKeys != "" {
		DefaultTavilyKeys = ResolveString(DefaultTavilyKeys, d.Search.TavilyKeys)
	}
	return base
}

// MergeRemoteOption merges remote config into local option. Local (non-empty)
// fields take priority.
func MergeRemoteOption(local *Option, remote *Option) {
	mergeOption(local, remote)
}
