package web

import (
	"encoding/json"
	"net/http"
	"sort"
)

// Capability describes one independently mountable Web feature. The manifest
// is intentionally JSON: browsers can discover the product surface without
// importing a product's protobuf package or knowing its profile name.
type Capability struct {
	ID         string   `json:"id"`
	Title      string   `json:"title,omitempty"`
	Version    string   `json:"version,omitempty"`
	APIRoutes  []string `json:"api_routes,omitempty"`
	AOPTypes   []string `json:"aop_types,omitempty"`
	UIContribs []string `json:"ui_contributions,omitempty"`
}

// CapabilityManifest is the descriptive form consumed by profile adapters.
// Capability is kept as the short name for route/config call sites.
type CapabilityManifest = Capability

// Profile describes an external node distribution that can be connected to
// this Hub. It is deliberately smaller than a capability: the profile is an
// installable product, while capabilities are the features it contributes
// after it connects.
type Profile struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

type Manifest struct {
	Product      string       `json:"product"`
	Version      string       `json:"version,omitempty"`
	Capabilities []Capability `json:"capabilities"`
	Profiles     []Profile    `json:"profiles,omitempty"`
}

func (m Manifest) Normalize() Manifest {
	m.Capabilities = append([]Capability(nil), m.Capabilities...)
	m.Profiles = append([]Profile(nil), m.Profiles...)
	sort.SliceStable(m.Capabilities, func(i, j int) bool { return m.Capabilities[i].ID < m.Capabilities[j].ID })
	sort.SliceStable(m.Profiles, func(i, j int) bool { return m.Profiles[i].ID < m.Profiles[j].ID })
	return m
}

func ManifestRoute(manifest Manifest) Route {
	manifest = manifest.Normalize()
	return Route{Pattern: "GET /api/manifest", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(manifest)
	})}
}
