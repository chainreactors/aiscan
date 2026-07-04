package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/chainreactors/aiscan/pkg/deploy/manager"
	"github.com/chainreactors/aiscan/pkg/probe"
	"github.com/chainreactors/aiscan/pkg/webproto"
)

type Handler struct {
	handler http.Handler
}

func NewHandler(service *Service, agents *AgentPool, deployMgr *manager.DeployManager, adminToken string, ioaHandler http.Handler, static http.Handler) *Handler {
	mux := http.NewServeMux()

	h := &handlerImpl{service: service, agents: agents}

	mux.HandleFunc("POST /api/scans", h.createScan)
	mux.HandleFunc("GET /api/scans", h.listScans)
	mux.HandleFunc("GET /api/scans/{id}", h.getScan)
	mux.HandleFunc("DELETE /api/scans/{id}", h.deleteScan)
	mux.HandleFunc("GET /api/scans/{id}/events", h.scanEvents)
	mux.HandleFunc("GET /api/scans/{id}/report", h.scanReport)
	mux.HandleFunc("GET /api/status", h.serviceStatus)
	mux.HandleFunc("GET /api/config", h.getConfig)
	mux.HandleFunc("PUT /api/config", h.saveConfig)
	mux.HandleFunc("GET /api/config/distribute", h.getDistributeConfig)
	mux.HandleFunc("POST /api/config/llm/test", h.testLLM)
	mux.HandleFunc("POST /api/config/{section}/test", h.testConn)
	mux.HandleFunc("GET /api/agents", h.listAgents)

	// Asset pool routes
	mux.HandleFunc("GET /api/assets", h.listAssets)
	mux.HandleFunc("POST /api/assets", h.addAssets)
	mux.HandleFunc("DELETE /api/assets/{id}", h.deleteAsset)

	// Project routes (asset-pool scope)
	mux.HandleFunc("GET /api/projects", h.listProjects)
	mux.HandleFunc("POST /api/projects", h.createProject)
	mux.HandleFunc("DELETE /api/projects/{id}", h.deleteProject)

	// Chat session routes
	mux.HandleFunc("POST /api/chat/sessions", h.createSession)
	mux.HandleFunc("GET /api/chat/sessions", h.listSessions)
	mux.HandleFunc("GET /api/chat/sessions/{id}", h.getSession)
	mux.HandleFunc("DELETE /api/chat/sessions/{id}", h.deleteSession)
	mux.HandleFunc("POST /api/chat/sessions/{id}/messages", h.sendMessage)
	mux.HandleFunc("POST /api/chat/sessions/{id}/cancel", h.cancelSession)
	mux.HandleFunc("POST /api/chat/sessions/{id}/upload", h.uploadFile)
	mux.HandleFunc("GET /api/chat/sessions/{id}/messages", h.listMessages)
	mux.HandleFunc("GET /api/chat/sessions/{id}/events", h.sessionEvents)

	if agents != nil {
		mux.HandleFunc("/api/agents/{id}/terminal/ws", func(w http.ResponseWriter, r *http.Request) {
			agents.HandleTerminalWS(r.PathValue("id"), w, r)
		})
		mux.HandleFunc("/api/agent/ws", agents.HandleWS)
	}

	if ioaHandler != nil {
		mux.Handle("/ioa/", http.StripPrefix("/ioa", ioaHandler))
	}

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	if deployMgr != nil {
		registerDeployRoutes(mux, deployMgr)
	}

	if static != nil {
		mux.Handle("/", static)
	}

	// Gate management-style endpoints behind the admin token (no-op if empty).
	return &Handler{handler: deployAuthMiddleware(adminToken, mux)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.handler.ServeHTTP(w, r)
}

type handlerImpl struct {
	service *Service
	agents  *AgentPool
}

func (h *handlerImpl) serviceStatus(w http.ResponseWriter, r *http.Request) {
	status := h.service.Status()
	if h.agents != nil {
		status.Agents = h.agents.Count()
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handlerImpl) listAgents(w http.ResponseWriter, r *http.Request) {
	if h.agents == nil {
		writeJSON(w, http.StatusOK, []AgentInfo{})
		return
	}
	writeJSON(w, http.StatusOK, h.agents.List())
}

func (h *handlerImpl) getConfig(w http.ResponseWriter, r *http.Request) {
	cs, err := h.service.GetConfigStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (h *handlerImpl) saveConfig(w http.ResponseWriter, r *http.Request) {
	var req webproto.DistributeConfig
	if !decodeBody(w, r, &req) {
		return
	}
	cs, err := h.service.SaveConfig(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (h *handlerImpl) getDistributeConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.service.GetDistributeConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *handlerImpl) testLLM(w http.ResponseWriter, r *http.Request) {
	var req probe.LLMTestRequest
	if !decodeBody(w, r, &req) {
		return
	}
	result, err := h.service.TestLLM(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handlerImpl) testConn(w http.ResponseWriter, r *http.Request) {
	var cfg webproto.DistributeConfig
	if !decodeOptionalBody(w, r, &cfg) {
		return
	}
	result, err := h.service.TestConn(r.Context(), r.PathValue("section"), cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handlerImpl) createScan(w http.ResponseWriter, r *http.Request) {
	var req ScanRequest
	if !decodeBody(w, r, &req) {
		return
	}
	verify, sniper, deep := req.AnalysisOptions()
	job, err := h.service.SubmitScan(r.Context(), req.Target, req.Mode, verify, sniper, deep, req.Project)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (h *handlerImpl) listScans(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.service.ListScans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, jobs)
}

func (h *handlerImpl) getScan(w http.ResponseWriter, r *http.Request) {
	job, err := h.service.GetScan(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "scan not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *handlerImpl) deleteScan(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteScan(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *handlerImpl) listAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := h.service.ListAssets(r.Context(), r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, assets)
}

func (h *handlerImpl) addAssets(w http.ResponseWriter, r *http.Request) {
	var req AssetRequest
	if !decodeBody(w, r, &req) {
		return
	}
	targets := req.Targets
	if req.Target != "" {
		targets = append(targets, req.Target)
	}
	if len(targets) == 0 {
		writeError(w, http.StatusBadRequest, "no targets provided")
		return
	}
	added, err := h.service.AddAssets(r.Context(), req.Project, targets, req.Source, req.Label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, added)
}

func (h *handlerImpl) deleteAsset(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteAsset(r.Context(), r.URL.Query().Get("project"), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *handlerImpl) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.service.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, projects)
}

func (h *handlerImpl) createProject(w http.ResponseWriter, r *http.Request) {
	var req ProjectRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" && strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, "project name required")
		return
	}
	p, err := h.service.CreateProject(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *handlerImpl) deleteProject(w http.ResponseWriter, r *http.Request) {
	// Refused-default and not-found are caller errors (400); the message is
	// surfaced verbatim in the UI, so keep it human-readable.
	if err := h.service.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *handlerImpl) scanEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.service.GetScan(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "scan not found")
		return
	}
	// A fast scan can reach a terminal state before the browser's EventSource
	// finishes subscribing; the hub keeps no backlog, so re-derive the terminal
	// event from the stored job for a late joiner (runs after Subscribe, and the
	// job is persisted terminal before its terminal event is broadcast).
	catchUp := func() []HubEvent {
		job, err := h.service.GetScan(r.Context(), id)
		if err != nil {
			return nil
		}
		switch job.Status {
		case StatusCompleted:
			return []HubEvent{{
				Type: "complete",
				Data: mustJSON(map[string]any{"scan_id": id, "status": "completed", "result": job.Result}),
			}}
		case StatusFailed, StatusCanceled:
			msg := job.Error
			if msg == "" {
				msg = "scan " + string(job.Status)
			}
			return []HubEvent{{
				Type: "error",
				Data: mustJSON(map[string]string{"scan_id": id, "error": msg}),
			}}
		}
		return nil
	}
	ServeSSE(w, r, h.service.Hub(), id, catchUp, "complete", "error")
}

func (h *handlerImpl) scanReport(w http.ResponseWriter, r *http.Request) {
	report, err := h.service.GetReport(r.Context(), r.PathValue("id"), r.URL.Query().Get("lang"))
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
	_, _ = io.WriteString(w, report) //nolint:gosec // Content-Type is text/markdown, not HTML
}

// --- Chat session handlers ---

func (h *handlerImpl) createSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if r.ContentLength > 0 {
		_ = decodeJSON(r.Body, &req)
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	session, err := h.service.CreateSession(r.Context(), req.AgentID, req.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (h *handlerImpl) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.service.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, sessions)
}

func (h *handlerImpl) getSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.service.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *handlerImpl) deleteSession(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *handlerImpl) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req SendMessageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	msg, err := h.service.HandleUserMessage(r.Context(), r.PathValue("id"), req.Content, ChatOptions{
		Persist:       req.Persist,
		MaxTurns:      req.PersistMaxTurns,
		EvalCriteria:  req.EvalCriteria,
		EvalMaxRounds: req.EvalMaxRounds,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (h *handlerImpl) cancelSession(w http.ResponseWriter, r *http.Request) {
	if err := h.service.CancelSession(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

const maxUploadSize = 50 << 20 // 50 MB

func (h *handlerImpl) uploadFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	result, err := h.service.HandleFileUpload(r.Context(), r.PathValue("id"), header.Filename, data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handlerImpl) listMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.service.GetMessages(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, http.StatusOK, msgs)
}

func (h *handlerImpl) sessionEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.service.GetSession(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	ServeSSE(w, r, h.service.Hub(), sessionTopic(id), nil, "_never")
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

// decodeBody decodes the JSON request body into v, writing a 400 on failure.
// Returns false when the caller should return early.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := decodeJSON(r.Body, v); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

// decodeOptionalBody decodes the request body only when one is present. An absent
// body is fine (returns true); a present-but-invalid body writes a 400 and
// returns false so the caller returns early.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	return decodeBody(w, r, v)
}

// writeList writes list as JSON, coalescing a nil slice to an empty JSON array.
func writeList[T any](w http.ResponseWriter, status int, list []T) {
	if list == nil {
		list = []T{}
	}
	writeJSON(w, status, list)
}
