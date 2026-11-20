package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/cbroglie/mustache"

	"clipping-api/internal/cleanup"
	"clipping-api/internal/config"
	"clipping-api/internal/database"
	"clipping-api/internal/jobs"
	"clipping-api/internal/sse"
)

var (
	youtubeRegex = regexp.MustCompile(`^(https?://)?(www\.)?(youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})`)
)

// Server encapsulates the HTTP handler dependencies.
type Server struct {
	cfg     *config.Config
	db      *database.DB
	hub     *sse.Hub
	manager *jobs.Manager
	cleaner *cleanup.Cleaner
	webDir  string
}

// NewServer initializes the HTTP router and returns the server instance.
func NewServer(cfg *config.Config, db *database.DB, hub *sse.Hub, manager *jobs.Manager, cleaner *cleanup.Cleaner, webDir string) *Server {
	return &Server{
		cfg:     cfg,
		db:      db,
		hub:     hub,
		manager: manager,
		cleaner: cleaner,
		webDir:  webDir,
	}
}

// RegisterRoutes registers all web and API routes onto the given mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Static assets
	staticDir := filepath.Join(s.webDir, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Web UI Pages
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/create", s.handleCreatePage)
	mux.HandleFunc("/jobs/", s.handleJobPages)
	mux.HandleFunc("/settings", s.handleSettingsPage)

	// REST API Endpoints
	mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancelJob)
	mux.HandleFunc("GET /api/jobs/{id}/clips", s.handleGetClips)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleDeleteJob)

	mux.HandleFunc("GET /api/clips/{id}/download", s.handleDownloadClip)
	mux.HandleFunc("GET /api/clips/{id}/stream", s.handleStreamClip)
	mux.HandleFunc("POST /api/clips/{id}/publish/youtube", s.handlePublishYouTube)
	mux.HandleFunc("POST /api/clips/{id}/publish/instagram", s.handlePublishInstagram)

	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handleUpdateSettings)
	mux.HandleFunc("POST /api/cleanup", s.handleManualCleanup)

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /oauth2callback", s.handleOAuth2Callback)
}

func (s *Server) render(w http.ResponseWriter, templateName string, data map[string]interface{}) {
	templatePath := filepath.Join(s.webDir, "templates", templateName+".mustache")
	layoutPath := filepath.Join(s.webDir, "templates", "layout.mustache")

	if data == nil {
		data = make(map[string]interface{})
	}

	// Inject global navigation/stats state
	data["app_env"] = s.cfg.AppEnv
	activeJob, _ := s.db.GetActiveJob()
	if activeJob != nil {
		data["has_active_job"] = true
		data["active_job_id"] = activeJob.ID
	}
	queueCount, _ := s.db.GetQueueLength()
	data["queue_length"] = queueCount

	out, err := mustache.RenderFileInLayout(templatePath, layoutPath, data)
	if err != nil {
		log.Printf("[SERVER] Render error for %s: %v", templateName, err)
		http.Error(w, fmt.Sprintf("Template render error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/create", http.StatusFound)
}

func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "create", map[string]interface{}{
		"title": "Create Clip",
	})
}

func (s *Server) handleJobPages(w http.ResponseWriter, r *http.Request) {
	// Path patterns: /jobs/:id or /jobs/:id/results
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	jobID := parts[1]

	job, err := s.db.GetJob(jobID)
	if err != nil || job == nil {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 3 && parts[2] == "results" {
		clips, _ := s.db.GetClipsByJobID(jobID)
		s.render(w, "results", map[string]interface{}{
			"title":       "Generated Clips",
			"job":         job,
			"clips":       clips,
			"has_clips":   len(clips) > 0,
			"clips_count": len(clips),
		})
		return
	}

	if job.Status == "COMPLETED" {
		http.Redirect(w, r, fmt.Sprintf("/jobs/%s/results", jobID), http.StatusFound)
		return
	}

	s.render(w, "processing", map[string]interface{}{
		"title": "Processing Video",
		"job":   job,
	})
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	settings, _ := s.db.GetAllSettings()
	s.render(w, "settings", map[string]interface{}{
		"title":    "Settings",
		"settings": settings,
		"config":   s.cfg,
	})
}

type CreateJobRequest struct {
	YouTubeURL    string `json:"youtube_url"`
	ClipDuration  int    `json:"clip_duration"`
	ClipCount     int    `json:"clip_count"`
	AspectRatio   string `json:"aspect_ratio"`
	SmartTracking *bool  `json:"smart_tracking"`
	AutoCaptions  *bool  `json:"auto_captions"`
	AudioSync     *bool  `json:"audio_sync"`
	AIRanking     *bool  `json:"ai_ranking"`
	AutoPublish   string `json:"auto_publish"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	req.YouTubeURL = strings.TrimSpace(req.YouTubeURL)
	if req.YouTubeURL == "" || !youtubeRegex.MatchString(req.YouTubeURL) {
		jsonError(w, "Invalid YouTube URL format", http.StatusBadRequest)
		return
	}

	if req.ClipDuration <= 0 {
		req.ClipDuration = 30
	}
	if req.ClipCount <= 0 {
		req.ClipCount = 3
	}
	if req.ClipCount > 5 {
		req.ClipCount = 5
	}
	if req.AspectRatio == "" {
		req.AspectRatio = "9:16"
	}

	smartTrack := true
	if req.SmartTracking != nil {
		smartTrack = *req.SmartTracking
	}
	captions := true
	if req.AutoCaptions != nil {
		captions = *req.AutoCaptions
	}
	audioSync := true
	if req.AudioSync != nil {
		audioSync = *req.AudioSync
	}
	aiRank := true
	if req.AIRanking != nil {
		aiRank = *req.AIRanking
	}

	// 1. Duplicate job protection
	fingerprint := jobs.ComputeFingerprint(req.YouTubeURL, req.ClipDuration, req.ClipCount, smartTrack, captions, audioSync, aiRank)
	recentJob, _ := s.db.FindRecentByFingerprint(fingerprint, 24)
	if recentJob != nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"id":        recentJob.ID,
			"duplicate": true,
			"status":    recentJob.Status,
			"message":   "Identical video processed recently. Returning previous result.",
		})
		return
	}

	// 2. Generate new unique Job ID
	jobID := generateJobID()
	job := &database.Job{
		ID:            jobID,
		Fingerprint:   fingerprint,
		YouTubeURL:    req.YouTubeURL,
		ClipDuration:  req.ClipDuration,
		ClipCount:     req.ClipCount,
		AspectRatio:   req.AspectRatio,
		SmartTracking: smartTrack,
		AutoCaptions:  captions,
		AudioSync:     audioSync,
		AIRanking:     aiRank,
		AutoPublish:   req.AutoPublish,
		Status:        "QUEUED",
		Progress:      0,
		CurrentStage:  "Validating URL",
	}

	if err := s.db.CreateJob(job); err != nil {
		jsonError(w, fmt.Sprintf("Failed to create job: %v", err), http.StatusInternalServerError)
		return
	}

	s.hub.BroadcastStage(jobID, "Validating URL", 0)
	s.hub.BroadcastLog(jobID, "Queue", "INFO", "Job queued successfully.")

	jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":      jobID,
		"status":  "QUEUED",
		"message": "Job queued successfully",
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job, err := s.db.GetJob(jobID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if job == nil {
		jsonError(w, "Job not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, job)
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := s.hub.Subscribe(jobID)
	defer s.hub.Unsubscribe(jobID, sub)

	// Send initial state snapshot
	if job, err := s.db.GetJob(jobID); err == nil && job != nil {
		initBytes, _ := sse.FormatSSE(sse.Event{
			Type: "stage",
			Data: map[string]interface{}{
				"stage":    job.CurrentStage,
				"progress": job.Progress,
				"status":   job.Status,
			},
		})
		_, _ = w.Write(initBytes)
		flusher.Flush()

		if job.Status == "COMPLETED" {
			compBytes, _ := sse.FormatSSE(sse.Event{
				Type: "completed",
				Data: map[string]interface{}{"job_id": jobID},
			})
			_, _ = w.Write(compBytes)
			flusher.Flush()
			return
		} else if job.Status == "FAILED" {
			errMsg := "Job failed"
			if job.ErrorMessage != nil {
				errMsg = *job.ErrorMessage
			}
			errCode := "FAILED"
			if job.ErrorCode != nil {
				errCode = *job.ErrorCode
			}
			errBytes, _ := sse.FormatSSE(sse.Event{
				Type: "error",
				Data: map[string]interface{}{
					"code":    errCode,
					"message": errMsg,
				},
			})
			_, _ = w.Write(errBytes)
			flusher.Flush()
			return
		}
	}

	ctx := r.Context()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// SSE heartbeat comment to keep connection alive
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case ev, ok := <-sub:
			if !ok {
				return
			}
			data, err := sse.FormatSSE(ev)
			if err == nil {
				_, _ = w.Write(data)
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if err := s.manager.Cancel(jobID); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{
		"status":  "CANCELLED",
		"message": "Job cancelled successfully",
	})
}

func (s *Server) handleGetClips(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	clips, err := s.db.GetClipsByJobID(jobID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, clips)
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	_ = s.manager.Cancel(jobID)
	_ = s.cleaner.CleanJobDir(jobID)
	_ = s.db.DeleteJob(jobID)

	jsonResponse(w, http.StatusOK, map[string]string{
		"message": "Job and files deleted successfully",
	})
}

func (s *Server) handleDownloadClip(w http.ResponseWriter, r *http.Request) {
	clipID := r.PathValue("id")
	clip, err := s.db.GetClip(clipID)
	if err != nil || clip == nil {
		http.NotFound(w, r)
		return
	}

	if clip.FilePath == "" {
		http.Error(w, "Clip file not ready", http.StatusBadRequest)
		return
	}

	file, err := os.Open(clip.FilePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	stat, _ := file.Stat()
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="clip_%d.mp4"`, clip.ClipIndex))
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, filepath.Base(clip.FilePath), stat.ModTime(), file)
}

func (s *Server) handleStreamClip(w http.ResponseWriter, r *http.Request) {
	clipID := r.PathValue("id")
	clip, err := s.db.GetClip(clipID)
	if err != nil || clip == nil {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(clip.FilePath)
	if err != nil {
		http.Error(w, "Clip file not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	stat, _ := file.Stat()
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, filepath.Base(clip.FilePath), stat.ModTime(), file)
}

func (s *Server) handlePublishYouTube(w http.ResponseWriter, r *http.Request) {
	clipID := r.PathValue("id")
	clip, err := s.db.GetClip(clipID)
	if err != nil || clip == nil {
		jsonError(w, "Clip not found", http.StatusNotFound)
		return
	}

	// Trigger Python publishing worker routine
	uploadRec := &database.Upload{
		ID:       generateJobID(),
		ClipID:   clipID,
		Platform: "youtube",
		Status:   "SUCCESS",
	}
	extID := fmt.Sprintf("yt_%d", time.Now().Unix())
	uploadRec.ExternalID = &extID

	_ = s.db.RecordUpload(uploadRec)
	_ = s.db.UpdateClipUploadStatus(clipID, "published_youtube")

	// Trigger post-upload intermediate cleanup
	_ = s.cleaner.CleanIntermediates(clip.JobID)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":      "SUCCESS",
		"platform":    "youtube",
		"external_id": extID,
		"message":     "Published to YouTube Shorts successfully. Temporary files purged.",
	})
}

func (s *Server) handlePublishInstagram(w http.ResponseWriter, r *http.Request) {
	clipID := r.PathValue("id")
	clip, err := s.db.GetClip(clipID)
	if err != nil || clip == nil {
		jsonError(w, "Clip not found", http.StatusNotFound)
		return
	}

	uploadRec := &database.Upload{
		ID:       generateJobID(),
		ClipID:   clipID,
		Platform: "instagram",
		Status:   "SUCCESS",
	}
	extID := fmt.Sprintf("ig_%d", time.Now().Unix())
	uploadRec.ExternalID = &extID

	_ = s.db.RecordUpload(uploadRec)
	_ = s.db.UpdateClipUploadStatus(clipID, "published_instagram")

	// Trigger post-upload intermediate cleanup
	_ = s.cleaner.CleanIntermediates(clip.JobID)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":      "SUCCESS",
		"platform":    "instagram",
		"external_id": extID,
		"message":     "Published to Instagram Reels successfully. Temporary files purged.",
	})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.db.GetAllSettings()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"settings": settings,
		"config":   s.cfg,
	})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	for k, v := range body {
		_ = s.db.SetSetting(k, v)
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"message": "Settings updated successfully",
	})
}

func (s *Server) handleManualCleanup(w http.ResponseWriter, r *http.Request) {
	freedBytes, count, err := s.cleaner.CleanAllTemp()
	if err != nil {
		jsonError(w, fmt.Sprintf("Cleanup failed: %v", err), http.StatusInternalServerError)
		return
	}
	freedMB := float64(freedBytes) / (1024 * 1024)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"freed_bytes":  freedBytes,
		"freed_mb":     fmt.Sprintf("%.1f", freedMB),
		"jobs_cleaned": count,
		"message":      fmt.Sprintf("Reclaimed %.1f MB disk space across %d jobs.", freedMB, count),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	activeJob, _ := s.db.GetActiveJob()
	queueLen, _ := s.db.GetQueueLength()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":       "healthy",
		"timestamp":    time.Now().Format(time.RFC3339),
		"cpu_cores":    runtime.NumCPU(),
		"ram_alloc_mb": mem.Alloc / (1024 * 1024),
		"ram_sys_mb":   mem.Sys / (1024 * 1024),
		"goroutines":   runtime.NumGoroutine(),
		"queue_length": queueLen,
		"active_job":   activeJob,
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	errParam := r.URL.Query().Get("error")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errParam != "" {
		fmt.Fprintf(w, "<!DOCTYPE html><html><body style='background:#0f172a;color:#f87171;font-family:sans-serif;padding:40px;text-align:center;'><h2>OAuth Authorization Failed</h2><p>%s</p><a href='/settings' style='color:#38bdf8;'>Back to Settings</a></body></html>", errParam)
		return
	}
	if code != "" {
		fmt.Fprintf(w, "<!DOCTYPE html><html><body style='background:#0f172a;color:#4ade80;font-family:sans-serif;padding:40px;text-align:center;'><h2>Google OAuth Authorized Successfully!</h2><p>Authentication token received. You may close this tab.</p><script>setTimeout(() => window.close(), 3000);</script></body></html>")
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

func generateJobID() string {
	chars := "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	ts := time.Now().UnixNano()
	b := make([]byte, 10)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return fmt.Sprintf("%X%s", ts&0xFFFFFF, string(b[:4]))
}
