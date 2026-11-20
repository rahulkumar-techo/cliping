package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"clipping-api/internal/cleanup"
	"clipping-api/internal/config"
	"clipping-api/internal/database"
	"clipping-api/internal/jobs"
	"clipping-api/internal/sse"
)

func setupTestServer(t *testing.T) (*Server, *http.ServeMux, string) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	cfg := &config.Config{
		AppEnv:            "testing",
		AppPort:           "8080",
		DatabasePath:      dbPath,
		StoragePath:       filepath.Join(tmpDir, "storage"),
		MaxConcurrentJobs: 1,
		MinFreeDiskGB:     1,
		JobTTLHours:       12,
		GeminiModel:       "gemini-2.5-flash",
	}

	hub := sse.NewHub()
	cleaner := cleanup.NewCleaner(db, cfg.StoragePath, cfg.JobTTLHours)
	mgr := jobs.NewManager(cfg, db, hub, cleaner)

	// Web dir points to repo apps/web
	webDir, _ := filepath.Abs("../../../apps/web")
	srv := NewServer(cfg, db, hub, mgr, cleaner, webDir)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	return srv, mux, tmpDir
}

func TestHealthAndReadyEndpoints(t *testing.T) {
	_, mux, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var health map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &health); err != nil {
		t.Fatalf("Failed to parse health json: %v", err)
	}
	if health["status"] != "healthy" {
		t.Errorf("Expected status healthy, got %v", health["status"])
	}

	readyReq := httptest.NewRequest("GET", "/ready", nil)
	readyRr := httptest.NewRecorder()
	mux.ServeHTTP(readyRr, readyReq)
	if readyRr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", readyRr.Code)
	}
}

func TestCreateJobValidation(t *testing.T) {
	_, mux, _ := setupTestServer(t)

	// Test invalid URL
	payload := []byte(`{"youtube_url": "invalid-url", "clip_duration": 30, "clip_count": 3}`)
	req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid URL, got %d", rr.Code)
	}

	// Test valid YouTube URL
	validPayload := []byte(`{"youtube_url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "clip_duration": 30, "clip_count": 3}`)
	validReq := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(validPayload))
	validReq.Header.Set("Content-Type", "application/json")
	validRr := httptest.NewRecorder()
	mux.ServeHTTP(validRr, validReq)

	if validRr.Code != http.StatusCreated {
		t.Fatalf("Expected 201 for valid job create, got %d: %s", validRr.Code, validRr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(validRr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid response JSON: %v", err)
	}
	jobID, ok := resp["id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("Missing or invalid job ID in response")
	}

	// Test GetJob
	getReq := httptest.NewRequest("GET", "/api/jobs/"+jobID, nil)
	getRr := httptest.NewRecorder()
	mux.ServeHTTP(getRr, getReq)
	if getRr.Code != http.StatusOK {
		t.Errorf("Expected 200 for GetJob, got %d", getRr.Code)
	}
}
