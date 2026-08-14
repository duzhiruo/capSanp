package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"capsnap/internal/agent"
	"capsnap/internal/embedding"
	"capsnap/internal/storage"
	"capsnap/internal/store"
	"capsnap/internal/vectorstore"
)

type Server struct {
	store       store.Repository
	storage     *storage.Local
	maxAttempts int
	embedder    embedding.Provider
	vectors     vectorstore.VectorStore
	mux         *http.ServeMux
}

func New(s store.Repository, st *storage.Local, maxAttempts int, embedder embedding.Provider, vectors vectorstore.VectorStore) *Server {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	server := &Server{
		store:       s,
		storage:     st,
		maxAttempts: maxAttempts,
		embedder:    embedder,
		vectors:     vectors,
		mux:         http.NewServeMux(),
	}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.withRequestID(s.withAccessLog(s.withCORS(s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /v1/devices", s.handleCreateDevice)
	s.mux.HandleFunc("POST /v1/screenshots", s.handleUploadScreenshot)
	s.mux.HandleFunc("GET /v1/screenshots", s.handleListScreenshots)
	s.mux.HandleFunc("GET /v1/screenshots/{id}", s.handleGetScreenshot)
	s.mux.HandleFunc("GET /v1/screenshots/{id}/image", s.handleImage)
	s.mux.HandleFunc("POST /v1/screenshots/{id}/retry-agent", s.handleRetryAgent)
	s.mux.HandleFunc("POST /v1/agent-runs", s.handleCreateAgentRun)
	s.mux.HandleFunc("GET /v1/agent-runs/{id}", s.handleGetAgentRun)
	s.mux.HandleFunc("GET /v1/search", s.handleSearch)
	s.mux.HandleFunc("GET /v1/stats", s.handleStats)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile("web/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		deviceID = agent.NewID("dev")
	}
	if err := s.store.EnsureDevice(r.Context(), deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_id": deviceID})
}

func (s *Server) handleUploadScreenshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	deviceID := firstNonEmpty(r.FormValue("device_id"), "demo-device")
	if err := s.store.EnsureDevice(r.Context(), deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("请使用 file 字段上传截图"))
		return
	}
	defer file.Close()
	screenshotID := agent.NewID("shot")
	path, err := s.storage.Save(file, header, screenshotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	ocrHint := strings.TrimSpace(r.FormValue("ocr_text"))
	shot := store.Screenshot{
		ID:               screenshotID,
		DeviceID:         deviceID,
		OriginalFilename: header.Filename,
		StoragePath:      path,
		Status:           "uploaded",
		OCRText:          ocrHint,
	}
	if err := s.store.CreateScreenshot(r.Context(), shot); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	runID := agent.NewID("run")
	reqID := GetRequestID(r.Context())
	input := map[string]any{"screenshot_id": screenshotID, "device_id": deviceID, "ocr_hint": ocrHint}
	if err := s.store.CreateAgentRun(r.Context(), runID, deviceID, screenshotID, "screenshot_organize", reqID, input); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.EnqueueTask(r.Context(), runID, s.maxAttempts); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"screenshot_id": screenshotID,
		"run_id":        runID,
		"request_id":    reqID,
		"status":        "queued",
	})
}

func (s *Server) handleListScreenshots(w http.ResponseWriter, r *http.Request) {
	deviceID := firstNonEmpty(r.URL.Query().Get("device_id"), "demo-device")
	limit := parseLimit(r.URL.Query().Get("limit"), 20)
	results, err := s.store.ListScreenshots(r.Context(), deviceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func (s *Server) handleGetScreenshot(w http.ResponseWriter, r *http.Request) {
	shot, err := s.store.GetScreenshot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, shot)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	shot, err := s.store.GetScreenshot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if _, err := os.Stat(shot.StoragePath); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	http.ServeFile(w, r, shot.StoragePath)
}

func (s *Server) handleGetAgentRun(w http.ResponseWriter, r *http.Request) {
	detail, err := s.store.GetAgentRunDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCreateAgentRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID     string `json:"device_id"`
		ScreenshotID string `json:"screenshot_id"`
		OCRText      string `json:"ocr_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.ScreenshotID == "" {
		writeError(w, http.StatusBadRequest, errors.New("screenshot_id 不能为空"))
		return
	}
	body.DeviceID = firstNonEmpty(body.DeviceID, "demo-device")

	runID := agent.NewID("run")
	reqID := GetRequestID(r.Context())
	input := map[string]any{"screenshot_id": body.ScreenshotID, "device_id": body.DeviceID, "ocr_hint": body.OCRText}
	if err := s.store.CreateAgentRun(r.Context(), runID, body.DeviceID, body.ScreenshotID, "screenshot_organize", reqID, input); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.EnqueueTask(r.Context(), runID, s.maxAttempts); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id":        runID,
		"screenshot_id": body.ScreenshotID,
		"request_id":    reqID,
		"status":        "queued",
	})
}

func (s *Server) handleRetryAgent(w http.ResponseWriter, r *http.Request) {
	shot, err := s.store.GetScreenshot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	runID := agent.NewID("run")
	reqID := GetRequestID(r.Context())
	input := map[string]any{"screenshot_id": shot.ID, "device_id": shot.DeviceID, "ocr_hint": shot.OCRText}
	if err := s.store.CreateAgentRun(r.Context(), runID, shot.DeviceID, shot.ID, "screenshot_organize", reqID, input); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.EnqueueTask(r.Context(), runID, s.maxAttempts); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id":        runID,
		"screenshot_id": shot.ID,
		"request_id":    reqID,
		"status":        "queued",
	})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseLimit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 100 {
		return fallback
	}
	return value
}
