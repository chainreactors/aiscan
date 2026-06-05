// proxy command registers unconditionally

package proxy

import (
	"net/url"
	"strings"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/proxyclient"

	// Register extra proxy protocols so proxyclient.NewClient can handle them.
	_ "github.com/chainreactors/proxyclient/extra/anytls"
	_ "github.com/chainreactors/proxyclient/extra/clash"
	_ "github.com/chainreactors/proxyclient/extra/hysteria2"
	_ "github.com/chainreactors/proxyclient/extra/trojan"
	_ "github.com/chainreactors/proxyclient/extra/vmess"
)

func init() {
	command.RegisterFactory(command.Factory{
		Group: "proxy",
		Build: func(deps *command.Deps, reg *command.CommandRegistry) {
			state := NewState(deps.ScannerProxy)
			cmd := New(state)
			cmd.SetOnProxyChange(func(newProxy string) {
				// 1. update BashTool scanner proxy env (for shell commands)
				if bt, ok := reg.GetTool("bash"); ok {
					if bash, ok := bt.(*command.BashTool); ok {
						bash.SetScannerProxy(newProxy)
					}
				}
				// 2. update commands that keep proxy state in-process.
				for _, pc := range reg.All() {
					if updater, ok := pc.(interface{ SetProxy(string) }); ok {
						updater.SetProxy(newProxy)
					}
				}
			})
			cmd.SetCommandExecutor(passthroughExecutor(reg))
			reg.Register(cmd, "proxy")

			// If --proxy / config proxy is a clash:// URL, auto-activate
			if strings.HasPrefix(strings.ToUpper(deps.ScannerProxy), "CLASH://") {
				u, err := url.Parse(deps.ScannerProxy)
				if err == nil {
					dial, dialErr := proxyclient.NewClient(u)
					if dialErr == nil {
						state.SetAutoDial(deps.ScannerProxy, dial)
					}
				}
			}
		},
	})
}
