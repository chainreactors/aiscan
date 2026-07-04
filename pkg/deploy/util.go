package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// NewID returns a random hex id with the given prefix (e.g. "cloud-", "dep-",
// "relay-"). Falls back to a timestamp if the RNG is unavailable.
func NewID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(b[:])
}

// ProviderKind canonicalizes and validates a cloud provider name.
func ProviderKind(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "aliyun", "alibaba", "ali":
		return "aliyun", nil
	case "tencent", "qcloud", "tencentcloud":
		return "tencent", nil
	default:
		return "", fmt.Errorf("unsupported provider %q (want aliyun|tencent)", name)
	}
}

// FirstNonEmpty returns the first argument that is non-empty after trimming.
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// NodeIOAURL builds a node's IOA endpoint URL with the access token baked in as
// userinfo (never serialized to JSON logs).
func NodeIOAURL(publicURL, token string) (string, error) {
	u, err := url.Parse(strings.TrimRight(publicURL, "/"))
	if err != nil {
		return "", err
	}
	if token != "" {
		u.User = url.User(token)
	}
	u.Path = "/ioa"
	return u.String(), nil
}

// ProgressURL builds the node's bootstrap-progress ping endpoint with the IOA
// token and node name pre-baked; the cloud-init report() appends the
// phase/bytes/total query params. Returns "" when no public URL is set, which
// disables reporting in the script.
func ProgressURL(publicURL, token, nodeName string) string {
	base := strings.TrimRight(publicURL, "/")
	if base == "" {
		return ""
	}
	return base + "/api/agent/progress?token=" + url.QueryEscape(token) + "&node=" + url.QueryEscape(nodeName)
}
