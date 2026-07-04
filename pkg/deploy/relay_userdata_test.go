package deploy

import (
	"strings"
	"testing"
)

// TestGenerateRelayUserData_ConfigBeforeKey guards the ordering invariant: the
// sshd GatewayPorts config + restart must run BEFORE the hub's key is installed.
// The hub can only connect once the key lands, so if the key went first the hub
// could race a default (GatewayPorts=no) sshd and bind its reverse forward to
// loopback — leaving relay:PORT unreachable to cloud nodes.
func TestGenerateRelayUserData_ConfigBeforeKey(t *testing.T) {
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAtest hub@aiscan"
	script := GenerateRelayUserData(key)

	gateway := strings.Index(script, "GatewayPorts yes")
	restart := strings.Index(script, "systemctl restart ssh")
	authKeys := strings.Index(script, "authorized_keys")
	if gateway < 0 || restart < 0 || authKeys < 0 {
		t.Fatalf("script missing expected steps: gateway=%d restart=%d authKeys=%d\n%s", gateway, restart, authKeys, script)
	}
	if !(gateway < restart && restart < authKeys) {
		t.Fatalf("ordering violated: want GatewayPorts(%d) < restart(%d) < authorized_keys(%d)\n%s",
			gateway, restart, authKeys, script)
	}
	if !strings.Contains(script, key) {
		t.Fatalf("hub key not embedded in script")
	}
	// The generated sshd drop-in must not carry leading indentation, which would
	// otherwise leak into the config file (sshd tolerates it, but it's a smell).
	if strings.Contains(script, "\n\tGatewayPorts") || strings.Contains(script, "\n\tPermitRootLogin") {
		t.Fatalf("sshd config lines have stray leading tabs:\n%s", script)
	}
}
