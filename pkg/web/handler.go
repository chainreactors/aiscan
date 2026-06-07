package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type Handler struct {
	service *Service
	static  http.Handler
}

func NewHandler(service *Service, static http.Handler) *Handler {
	return &Handler{service: service, static: static}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	segments := pathSegments(r.URL.Path)

	if len(segments) == 0 || (len(segments) == 1 && segments[0] == "") {
		if h.static != nil {
			h.static.ServeHTTP(w, r)
		} else {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "aiscan-web"})
		}
		return
	}

	if segments[0] == "api" && len(segments) >= 2 && segments[1] == "scans" {
		h.serveScans(w, r, segments[2:])
		return
	}

	if segments[0] == "api" && len(segments) == 2 && segments[1] == "status" {
		h.serviceStatus(w, r)
		return
	}

	if segments[0] == "api" && len(segments) >= 2 && segments[1] == "config" {
		h.serveConfig(w, r, segments[2:])
		return
	}

	if segments[0] == "api" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if segments[0] == "health" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if h.static != nil {
		h.static.ServeHTTP(w, r)
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (h *Handler) serveScans(w http.ResponseWriter, r *http.Request, segments []string) {
	switch {
	case len(segments) == 0 && r.Method == http.MethodPost:
		h.createScan(w, r)
	case len(segments) == 0 && r.Method == http.MethodGet:
		h.listScans(w, r)
	case len(segments) == 1:
		id := segments[0]
		switch r.Method {
		case http.MethodGet:
			h.getScan(w, r, id)
		case http.MethodDelete:
			h.cancelScan(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(segments) == 2 && segments[1] == "events":
		h.scanEvents(w, r, segments[0])
	case len(segments) == 2 && segments[1] == "report":
		h.scanReport(w, r, segments[0])
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) serviceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, h.service.Status())
}

func (h *Handler) serveConfig(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) != 1 || segments[0] != "llm" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg, err := h.service.GetLLMConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var req LLMConfig
		if err := decodeJSON(r.Body, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cfg, err := h.service.SaveLLMConfig(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) createScan(w http.ResponseWriter, r *http.Request) {
	var req ScanRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	verify, sniper, deep := req.AnalysisOptions()
	job, err := h.service.SubmitScan(r.Context(), req.Target, req.Mode, verify, sniper, deep)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (h *Handler) listScans(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.service.ListScans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if jobs == nil {
		jobs = []*ScanJob{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (h *Handler) getScan(w http.ResponseWriter, r *http.Request, id string) {
	job, err := h.service.GetScan(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "scan not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) cancelScan(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.service.CancelScan(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

func (h *Handler) scanEvents(w http.ResponseWriter, r *http.Request, id string) {
	_, err := h.service.GetScan(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "scan not found")
		return
	}
	ServeSSE(w, r, h.service.Hub(), id)
}

func (h *Handler) scanReport(w http.ResponseWriter, r *http.Request, id string) {
	report, err := h.service.GetReport(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "scan not found")
		return
	}
	if report == "" {
		writeError(w, http.StatusNotFound, "report not ready")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, report) //nolint:gosec // G705: content-type is text/markdown, not HTML
}

func pathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(body io.ReadCloser, v interface{}) error {
	defer body.Close()
	return json.NewDecoder(body).Decode(v)
}
