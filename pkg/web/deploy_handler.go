package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/chainreactors/aiscan/pkg/cloud"
	"github.com/chainreactors/aiscan/pkg/deploy/manager"
)

// deployHandler serves the cloud-deploy API and the agent binary endpoint.
type deployHandler struct {
	mgr *manager.DeployManager

	binOnce sync.Once
	binPath string
	binSum  string
	binErr  error
}

func newDeployHandler(mgr *manager.DeployManager) *deployHandler {
	return &deployHandler{mgr: mgr}
}

// --- credentials ---

func (h *deployHandler) listCredentials(w http.ResponseWriter, r *http.Request) {
	views, err := h.mgr.ListCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, views)
}

func (h *deployHandler) saveCredential(w http.ResponseWriter, r *http.Request) {
	var in manager.SaveCredentialInput
	if !decodeBody(w, r, &in) {
		return
	}
	view, err := h.mgr.SaveCredential(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *deployHandler) deleteCredential(w http.ResponseWriter, r *http.Request) {
	if err := h.mgr.DeleteCredential(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *deployHandler) listRegions(w http.ResponseWriter, r *http.Request) {
	var in manager.ListRegionsInput
	if !decodeOptionalBody(w, r, &in) {
		return
	}
	regions, err := h.mgr.ListRegions(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeList(w, http.StatusOK, regions)
}

func (h *deployHandler) listImages(w http.ResponseWriter, r *http.Request) {
	var in manager.ListImagesInput
	if !decodeBody(w, r, &in) {
		return
	}
	imgs, err := h.mgr.ListImages(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeList(w, http.StatusOK, imgs)
}

func (h *deployHandler) listInstanceTypes(w http.ResponseWriter, r *http.Request) {
	var in manager.ListInstanceTypesInput
	if !decodeBody(w, r, &in) {
		return
	}
	types, err := h.mgr.ListInstanceTypes(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeList(w, http.StatusOK, types)
}

// --- public url & providers ---

func (h *deployHandler) getPublicURL(w http.ResponseWriter, r *http.Request) {
	u, err := h.mgr.GetPublicURL(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"public_url": u,
		"providers":  cloud.SupportedProviders(),
	})
}

func (h *deployHandler) setPublicURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublicURL string `json:"public_url"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.mgr.SetPublicURL(r.Context(), req.PublicURL); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_url": req.PublicURL})
}

// --- outbound tunnel ---

func (h *deployHandler) getTunnel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.mgr.TunnelStatus())
}

func (h *deployHandler) startTunnel(w http.ResponseWriter, r *http.Request) {
	var req manager.StartTunnelRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	st, err := h.mgr.StartTunnel(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *deployHandler) stopTunnel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.mgr.StopTunnel(r.Context()))
}

// destroyRelay terminates the auto-provisioned relay VM and clears the tunnel.
func (h *deployHandler) destroyRelay(w http.ResponseWriter, r *http.Request) {
	st, err := h.mgr.DestroyRelay(r.Context())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// --- deployments ---

func (h *deployHandler) createDeploy(w http.ResponseWriter, r *http.Request) {
	var req manager.DeployRequest
	if !decodeBody(w, r, &req) {
		return
	}
	res, err := h.mgr.CreateDeploy(r.Context(), req)
	if err != nil {
		// A partial/failed launch may still return a record alongside the error.
		if res != nil {
			writeJSON(w, http.StatusMultiStatus, map[string]any{"result": res, "error": err.Error()})
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *deployHandler) listDeploys(w http.ResponseWriter, r *http.Request) {
	views, err := h.mgr.ListDeploys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, views)
}

func (h *deployHandler) getDeploy(w http.ResponseWriter, r *http.Request) {
	view, err := h.mgr.GetDeploy(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *deployHandler) deleteDeploy(w http.ResponseWriter, r *http.Request) {
	view, err := h.mgr.Recycle(r.Context(), r.PathValue("id"), nil)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *deployHandler) recycleDeploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceIDs []string `json:"instance_ids"`
	}
	if r.ContentLength > 0 {
		_ = decodeJSON(r.Body, &req)
	}
	view, err := h.mgr.Recycle(r.Context(), r.PathValue("id"), req.InstanceIDs)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *deployHandler) recycleAll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CloudID string `json:"cloud_id"`
		Space   string `json:"space"`
	}
	if r.ContentLength > 0 {
		_ = decodeJSON(r.Body, &req)
	}
	n, err := h.mgr.RecycleAll(r.Context(), req.CloudID, req.Space)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recycled": n})
}

// reconcile terminates tagged orphan nodes the hub owns but no live record tracks
// (records lost across a restart, partial launches, or deletes that never landed).
// Pass ?dry_run=true to preview without terminating.
func (h *deployHandler) reconcile(w http.ResponseWriter, r *http.Request) {
	dryRun := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("dry_run")), "true")
	report, err := h.mgr.Reconcile(r.Context(), dryRun)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// --- agent binary distribution (open: new VMs fetch this without admin token) ---

func (h *deployHandler) resolveBinary() (string, string, error) {
	h.binOnce.Do(func() {
		path := h.mgr.BinaryPath()
		if path == "" {
			exe, err := os.Executable()
			if err != nil {
				h.binErr = err
				return
			}
			path = exe
		}
		f, err := os.Open(path)
		if err != nil {
			h.binErr = err
			return
		}
		defer f.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, f); err != nil {
			h.binErr = err
			return
		}
		h.binPath = path
		h.binSum = hex.EncodeToString(sum.Sum(nil))
	})
	return h.binPath, h.binSum, h.binErr
}

func (h *deployHandler) serveBinary(w http.ResponseWriter, r *http.Request) {
	if h.mgr.BinaryPath() == "" {
		targetOS := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("os")))
		targetArch := normalizeBinaryArch(r.URL.Query().Get("arch"))
		if (targetOS != "" && targetOS != runtime.GOOS) || (targetArch != "" && targetArch != runtime.GOARCH) {
			writeError(w, http.StatusBadRequest, "agent binary unavailable for requested platform "+targetOS+"/"+targetArch+"; hub binary is "+runtime.GOOS+"/"+runtime.GOARCH)
			return
		}
	}
	path, sum, err := h.resolveBinary()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent binary unavailable: "+err.Error())
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent binary unavailable")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Checksum-Sha256", sum)
	w.Header().Set("Content-Disposition", `attachment; filename="aiscan"`)
	http.ServeContent(w, r, "aiscan", fi.ModTime(), f)
}

// recordProgress ingests a node's self-reported bootstrap progress. Like
// serveBinary it is ungated (freshly-booted nodes have no admin token), so it is
// validated against the embedded IOA token every node carries in its config.
func (h *deployHandler) recordProgress(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	bytes, _ := strconv.ParseInt(q.Get("bytes"), 10, 64)
	total, _ := strconv.ParseInt(q.Get("total"), 10, 64)
	if !h.mgr.RecordNodeProgress(q.Get("token"), q.Get("node"), q.Get("phase"), bytes, total) {
		writeError(w, http.StatusUnauthorized, "invalid progress report")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func normalizeBinaryArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(arch))
	}
}

// --- local agents (hub-hosted nodes: one-click launch + stop) ---

func (h *deployHandler) launchLocal(w http.ResponseWriter, r *http.Request) {
	view, err := h.mgr.LaunchLocal(r.Context())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *deployHandler) listLocal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.mgr.ListLocal())
}

func (h *deployHandler) stopLocal(w http.ResponseWriter, r *http.Request) {
	if err := h.mgr.StopLocal(r.PathValue("id")); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// registerDeployRoutes wires the deploy endpoints onto the mux.
func registerDeployRoutes(mux *http.ServeMux, mgr *manager.DeployManager) {
	h := newDeployHandler(mgr)
	mux.HandleFunc("GET /api/cloud/credentials", h.listCredentials)
	mux.HandleFunc("PUT /api/cloud/credentials", h.saveCredential)
	mux.HandleFunc("DELETE /api/cloud/credentials/{id}", h.deleteCredential)
	mux.HandleFunc("POST /api/cloud/regions", h.listRegions)
	mux.HandleFunc("POST /api/cloud/images", h.listImages)
	mux.HandleFunc("POST /api/cloud/instance-types", h.listInstanceTypes)
	mux.HandleFunc("GET /api/cloud/public-url", h.getPublicURL)
	mux.HandleFunc("PUT /api/cloud/public-url", h.setPublicURL)
	mux.HandleFunc("GET /api/cloud/tunnel", h.getTunnel)
	mux.HandleFunc("POST /api/cloud/tunnel", h.startTunnel)
	mux.HandleFunc("DELETE /api/cloud/tunnel", h.stopTunnel)
	mux.HandleFunc("DELETE /api/cloud/tunnel/relay", h.destroyRelay)
	mux.HandleFunc("POST /api/deploy", h.createDeploy)
	mux.HandleFunc("GET /api/deploy", h.listDeploys)
	mux.HandleFunc("GET /api/deploy/{id}", h.getDeploy)
	mux.HandleFunc("DELETE /api/deploy/{id}", h.deleteDeploy)
	mux.HandleFunc("POST /api/deploy/{id}/recycle", h.recycleDeploy)
	mux.HandleFunc("POST /api/deploy/recycle-all", h.recycleAll)
	mux.HandleFunc("POST /api/deploy/reconcile", h.reconcile)
	// Local agents (hub-hosted). The literal "local" segment takes precedence
	// over the {id} wildcards above, and no cloud deploy id is ever "local".
	mux.HandleFunc("POST /api/deploy/local", h.launchLocal)
	mux.HandleFunc("GET /api/deploy/local", h.listLocal)
	mux.HandleFunc("DELETE /api/deploy/local/{id}", h.stopLocal)
	mux.HandleFunc("GET /api/agent/binary", h.serveBinary)
	// Ungated like the binary endpoint (nodes have no admin token); the handler
	// validates the IOA token every node carries.
	mux.HandleFunc("POST /api/agent/progress", h.recordProgress)
}

// deployAuthMiddleware gates management endpoints behind an admin token when one
// is configured. The agent binary endpoint stays open so freshly-booted VMs can
// fetch it. Empty token => no gating (back-compat).
func deployAuthMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		gated := strings.HasPrefix(path, "/api/cloud") || strings.HasPrefix(path, "/api/deploy")
		if gated && r.Method != http.MethodOptions {
			if !checkAdminToken(r, token) {
				writeError(w, http.StatusUnauthorized, "admin token required")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func checkAdminToken(r *http.Request, token string) bool {
	if got := r.Header.Get("X-Admin-Token"); tokenEqual(got, token) {
		return true
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return tokenEqual(strings.TrimPrefix(auth, "Bearer "), token)
	}
	return false
}

// tokenEqual compares an admin token in constant time so a timing side channel
// cannot leak it byte by byte.
func tokenEqual(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
