package deploy

import (
	"fmt"
	"strings"
)

// GenerateRelayUserData renders the cloud-init bash script for an SSH reverse
// tunnel relay. Unlike an agent node it runs no aiscan binary; it only has to be
// SSH-reachable by the hub with the hub's generated key and permit remote port
// forwarding bound to all interfaces (GatewayPorts) so cloud nodes can reach the
// forwarded hub port. authorizedKey is one OpenSSH "ssh-ed25519 AAAA... " line.
//
// Ordering matters: sshd is configured and restarted BEFORE the hub's key is
// installed. The hub can only authenticate once the key lands, so doing the
// GatewayPorts config + restart first guarantees the hub's first (and every)
// session hits an sshd that already binds reverse forwards to all interfaces.
// If the key went in first, the hub could connect to the still-default sshd
// (GatewayPorts=no), whose `ssh -R 0.0.0.0:PORT` silently binds to loopback —
// leaving relay:PORT unreachable to the cloud nodes even though the tunnel looks
// "connected" from the hub's side.
func GenerateRelayUserData(authorizedKey string) string {
	key := strings.TrimSpace(authorizedKey)
	return fmt.Sprintf(`#!/bin/bash
set -eu

# Permit key-based root login and remote forwarding bound to all interfaces, so
# the hub's reverse tunnel (ssh -R 0.0.0.0:PORT) is reachable by the cloud nodes.
install -d -m 755 /etc/ssh/sshd_config.d
cat >/etc/ssh/sshd_config.d/99-aiscan-relay.conf <<'EOF'
PermitRootLogin prohibit-password
PubkeyAuthentication yes
AllowTcpForwarding yes
GatewayPorts yes
ClientAliveInterval 30
ClientAliveCountMax 3
EOF

# Older images may not Include sshd_config.d; append there too (idempotent).
if ! grep -q 'sshd_config.d/\*.conf' /etc/ssh/sshd_config 2>/dev/null; then
  { echo ''
    echo '# aiscan relay'
    echo 'PermitRootLogin prohibit-password'
    echo 'PubkeyAuthentication yes'
    echo 'AllowTcpForwarding yes'
    echo 'GatewayPorts yes'
  } >>/etc/ssh/sshd_config
fi

# Apply the sshd config now, so the reverse forward the hub opens next binds to
# all interfaces rather than loopback.
systemctl restart ssh 2>/dev/null || systemctl restart sshd 2>/dev/null || service ssh restart || service sshd restart || true

# Only now install the hub's public key. This is the gate that lets the hub in;
# by the time it can connect, GatewayPorts=yes above is already in effect.
install -d -m 700 /root/.ssh
grep -qxF %q /root/.ssh/authorized_keys 2>/dev/null || echo %q >>/root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
`, key, key)
}
