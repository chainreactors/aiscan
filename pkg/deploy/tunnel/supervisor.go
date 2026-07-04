// Package tunnel implements the hub's outbound SSH reverse tunnel to a relay.
// It lets a hub behind NAT (no public IP) still be reached by cloud nodes: the
// hub dials out to a small relay VM over SSH and binds a remote-forward port on
// the relay's public IP that nodes call back through.
package tunnel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// NewRelayKey generates an ed25519 keypair for hub->relay authentication. It
// returns the private key as PKCS8 PEM (persisted on the hub, never distributed)
// and the public key as an OpenSSH authorized_keys line (injected into the relay
// via cloud-init).
func NewRelayKey() (privPEM, authorizedKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	authorizedKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return privPEM, authorizedKey, nil
}

// SignerFromPEM parses a PKCS8 PEM private key into an ssh.Signer.
func SignerFromPEM(privPEM string) (ssh.Signer, error) {
	return ssh.ParsePrivateKey([]byte(privPEM))
}

// Supervisor supervises a native SSH reverse tunnel to a relay. It dials the
// relay, requests a remote forward on remoteBind, and proxies each accepted
// connection to the hub's local address. If the connection drops while desired
// up, it reconnects with capped backoff.
type Supervisor struct {
	relayAddr  string     // relay sshd address, host:port
	user       string     // relay login user
	signer     ssh.Signer // hub auth key
	remoteBind string     // bind on the relay, e.g. "0.0.0.0:3000"
	localAddr  string     // hub target, e.g. "127.0.0.1:3000"

	mu        sync.Mutex
	running   bool
	connected bool
	lastErr   string
	startedAt time.Time
	hostKey   ssh.PublicKey // pinned on first connect (in-memory TOFU)
	cancel    context.CancelFunc
	done      chan struct{}
}

// New builds a Supervisor, parsing privPEM (PKCS8 PEM) into the hub auth key.
// relayAddr and remoteBind are host:port strings; localAddr is the hub target.
func New(relayAddr, user, privPEM, remoteBind, localAddr string) (*Supervisor, error) {
	signer, err := SignerFromPEM(privPEM)
	if err != nil {
		return nil, err
	}
	return &Supervisor{
		relayAddr:  relayAddr,
		user:       user,
		signer:     signer,
		remoteBind: remoteBind,
		localAddr:  localAddr,
	}, nil
}

// Start launches the supervisor goroutine. Idempotent while already running.
func (t *Supervisor) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.running = true
	t.connected = false
	t.lastErr = ""
	t.startedAt = time.Now()
	done := make(chan struct{})
	t.done = done
	go t.supervise(ctx, done)
}

// Stop cancels the supervisor (dropping the SSH connection) and waits for exit.
func (t *Supervisor) Stop() {
	t.mu.Lock()
	cancel := t.cancel
	done := t.done
	t.running = false
	t.cancel = nil
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Status snapshots the supervisor state.
func (t *Supervisor) Status() (running, connected bool, lastErr string, startedAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running, t.connected, t.lastErr, t.startedAt
}

func (t *Supervisor) supervise(ctx context.Context, done chan struct{}) {
	defer close(done)
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		err := t.runOnce(ctx)
		t.mu.Lock()
		t.connected = false
		if err != nil && ctx.Err() == nil {
			t.lastErr = err.Error()
		}
		t.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		// A connection that stayed up a while resets the backoff so a transient
		// drop reconnects promptly rather than after a grown delay.
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// runOnce establishes one SSH connection + remote forward and blocks until it
// dies or ctx is canceled, sending periodic keepalives.
func (t *Supervisor) runOnce(ctx context.Context) error {
	cfg := &ssh.ClientConfig{
		User:            t.user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(t.signer)},
		HostKeyCallback: t.checkHostKey,
		Timeout:         15 * time.Second,
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", t.relayAddr)
	if err != nil {
		return err
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, t.relayAddr, cfg)
	if err != nil {
		conn.Close()
		return err
	}
	client := ssh.NewClient(sc, chans, reqs)
	defer client.Close()

	ln, err := client.Listen("tcp", t.remoteBind)
	if err != nil {
		return fmt.Errorf("remote forward %s failed (relay GatewayPorts/AllowTcpForwarding?): %w", t.remoteBind, err)
	}
	defer ln.Close()

	t.mu.Lock()
	t.connected = true
	t.lastErr = ""
	t.mu.Unlock()

	go t.acceptLoop(ln)

	closed := make(chan error, 1)
	go func() { closed <- client.Wait() }()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-closed:
			return err
		case <-ticker.C:
			if _, _, err := client.SendRequest("keepalive@aiscan", true, nil); err != nil {
				return err
			}
		}
	}
}

// acceptLoop accepts relay-side connections and proxies each to the local hub.
func (t *Supervisor) acceptLoop(ln net.Listener) {
	for {
		rc, err := ln.Accept()
		if err != nil {
			return
		}
		go t.proxy(rc)
	}
}

// proxy bridges one relay-forwarded connection to the local hub address.
func (t *Supervisor) proxy(rc net.Conn) {
	defer rc.Close()
	lc, err := net.DialTimeout("tcp", t.localAddr, 10*time.Second)
	if err != nil {
		return
	}
	defer lc.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(lc, rc); done <- struct{}{} }()
	go func() { _, _ = io.Copy(rc, lc); done <- struct{}{} }()
	<-done // first half to finish closes both via the defers
}

// checkHostKey pins the relay host key on first connect (trust-on-first-use) and
// rejects a later mismatch. Pinning is in-memory for the supervisor's lifetime;
// the node->relay hop is plain TCP anyway, so this guards the hub->relay leg
// against a mid-session key swap rather than promising cross-restart identity.
func (t *Supervisor) checkHostKey(hostname string, remote net.Addr, key ssh.PublicKey) error {
	t.mu.Lock()
	pinned := t.hostKey
	if pinned == nil {
		t.hostKey = key
	}
	t.mu.Unlock()
	if pinned == nil {
		return nil
	}
	if bytes.Equal(pinned.Marshal(), key.Marshal()) {
		return nil
	}
	return fmt.Errorf("relay host key changed since first connect (possible MITM); destroy and re-provision the relay")
}
