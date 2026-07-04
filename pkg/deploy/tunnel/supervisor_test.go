package tunnel

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestNewRelayKeyRoundTrips(t *testing.T) {
	priv, authKey, err := NewRelayKey()
	if err != nil {
		t.Fatalf("NewRelayKey: %v", err)
	}
	if authKey == "" || priv == "" {
		t.Fatalf("empty key material")
	}
	signer, err := SignerFromPEM(priv)
	if err != nil {
		t.Fatalf("SignerFromPEM: %v", err)
	}
	// The authorized_keys line must match the signer's public key.
	want := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if authKey != want {
		t.Fatalf("authorized key %q != signer pubkey %q", authKey, want)
	}
}
