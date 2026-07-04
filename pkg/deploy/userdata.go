package deploy

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// UserDataParams drives the cloud-init bootstrap script generated for each node.
type UserDataParams struct {
	PublicURL string // hub address reachable from the new VM, e.g. http://1.2.3.4:3000
	// IOAURL is the agent's IOA endpoint. The access token is embedded as URL
	// userinfo (http://{token}@host:port/ioa) — that is how the IOA *client*
	// authenticates; --ioa-token is server-side only.
	IOAURL   string
	Space    string // IOA space the node joins for swarm cooperation
	NodeName string // unique node name within the space
	// ProgressURL is the hub endpoint the bootstrap pings with its phase/bytes so
	// the UI can show real progress before the agent registers. Token + node are
	// pre-baked; the script appends &phase=&bytes=&total=. Empty disables it.
	ProgressURL string
	// Overrides are extra `aiscan agent` long flags (without the leading --),
	// e.g. {"provider": "openai"}. They win over hub-distributed config because
	// local flags take precedence in MergeRemoteOption.
	Overrides map[string]string
}

// shellQuote single-quotes a value for safe embedding in a bash command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// agentArgs builds the ordered `aiscan agent ...` flag list.
func (p UserDataParams) agentArgs() string {
	var args []string
	add := func(flag, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		args = append(args, "--"+flag+" "+shellQuote(val))
	}
	add("web-url", p.PublicURL)
	add("ioa-url", p.IOAURL)
	add("space", p.Space)
	add("ioa-node-name", p.NodeName)

	for _, k := range slices.Sorted(maps.Keys(p.Overrides)) {
		add(k, p.Overrides[k])
	}
	return strings.Join(args, " ")
}

// GenerateUserData renders the cloud-init bash script that downloads the aiscan
// binary from the hub, installs a systemd service, and starts the agent so it
// auto-registers with the hub and joins the IOA space.
func GenerateUserData(p UserDataParams) string {
	binURL := strings.TrimRight(p.PublicURL, "/") + "/api/agent/binary?os=linux&arch=${ARCH}"
	args := p.agentArgs()

	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

# Resolve CPU architecture for the binary download.
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) ARCH=amd64 ;;
esac

mkdir -p /opt/aiscan
BIN=/usr/local/bin/aiscan
BINURL=%q
PROGRESS=%q

# report PHASE [BYTES] [TOTAL] -> best-effort progress ping to the hub. Never
# fails or blocks the bootstrap (short timeout, errors swallowed); no-op when the
# hub set no public URL.
report() {
  [ -n "$PROGRESS" ] || return 0
  curl -fsS -m 5 "$PROGRESS&phase=$1&bytes=${2:-0}&total=${3:-0}" -o /dev/null 2>/dev/null || true
}

report booting

# Learn the binary size (Content-Length) up front so the hub can show a percent.
TOTAL=$(curl -fsSIL -m 15 "$BINURL" 2>/dev/null | awk 'BEGIN{IGNORECASE=1}/^content-length:/{gsub(/\r/,"");print $2}' | tail -1 || true)
[ -n "${TOTAL:-}" ] || TOTAL=0
report downloading 0 "$TOTAL"

# Sample the growing file every 5s while the download runs, in the background.
( while :; do report downloading "$(wc -c < "$BIN" 2>/dev/null || echo 0)" "$TOTAL"; sleep 5; done ) &
SAMP=$!

# Download the aiscan binary from the hub (retry until reachable).
for i in $(seq 1 30); do
  if curl -fsSL "$BINURL" -o "$BIN"; then
    break
  fi
  echo "aiscan: download attempt $i failed, retrying..." >&2
  sleep 10
done
kill "$SAMP" 2>/dev/null || true
chmod +x "$BIN"
report installing "$(wc -c < "$BIN" 2>/dev/null || echo 0)" "$TOTAL"

# Install a systemd unit that keeps the agent connected to the hub.
cat >/etc/systemd/system/aiscan-agent.service <<'UNIT'
[Unit]
Description=AIScan Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/aiscan
ExecStart=/usr/local/bin/aiscan agent %s
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
report starting
systemctl enable --now aiscan-agent
`, binURL, p.ProgressURL, args)
}
