package database

import (
	"path/filepath"
	"testing"
)

func TestDBCRUDAndFingerprint(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}
	defer db.Close()

	job := &Job{
		ID:            "TEST01",
		Fingerprint:   "abc123hash",
		YouTubeURL:    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		ClipDuration:  30,
		ClipCount:     3,
		AspectRatio:   "9:16",
		SmartTracking: true,
		AutoCaptions:  true,
		AudioSync:     true,
		AIRanking:     true,
		Status:        "QUEUED",
		Progress:      0,
		CurrentStage:  "Validating URL",
	}

	if err := db.CreateJob(job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	fetched, err := db.GetJob("TEST01")
	if err != nil || fetched == nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if fetched.YouTubeURL != job.YouTubeURL {
		t.Errorf("Expected URL %s, got %s", job.YouTubeURL, fetched.YouTubeURL)
	}

	// Test UpdateJobStatus
	if err := db.UpdateJobStatus("TEST01", "COMPLETED", "Completed", 100, nil, nil); err != nil {
		t.Fatalf("UpdateJobStatus failed: %v", err)
	}

	// Test FindRecentByFingerprint
	dup, err := db.FindRecentByFingerprint("abc123hash", 24)
	if err != nil {
		t.Fatalf("FindRecentByFingerprint failed: %v", err)
	}
	if dup == nil || dup.ID != "TEST01" {
		t.Errorf("Expected to find recent completed job TEST01")
	}

	// Test Clips
	clip := &Clip{
		ID:           "CLIP01",
		JobID:        "TEST01",
		ClipIndex:    1,
		StartTime:    10.0,
		EndTime:      40.0,
		Duration:     30.0,
		LocalScore:   85.0,
		GeminiScore:  90.0,
		FinalScore:   87.25,
		Reason:       "Great hook and high energy",
		FilePath:     filepath.Join(tmpDir, "clip_01.mp4"),
		UploadStatus: "pending",
	}

	if err := db.SaveClip(clip); err != nil {
		t.Fatalf("SaveClip failed: %v", err)
	}

	clips, err := db.GetClipsByJobID("TEST01")
	if err != nil || len(clips) != 1 {
		t.Fatalf("GetClipsByJobID failed or wrong count: %v, count: %d", err, len(clips))
	}
	if clips[0].FinalScore != 87.25 {
		t.Errorf("Expected final score 87.25, got %f", clips[0].FinalScore)
	}

	// Test ResetInterruptedJobs
	job2 := &Job{
		ID:           "TEST02",
		Fingerprint:  "hash2",
		YouTubeURL:   "https://www.youtube.com/watch?v=xyz",
		ClipDuration: 15,
		ClipCount:    1,
		Status:       "TRANSCRIBING",
	}
	_ = db.CreateJob(job2)
	_ = db.ResetInterruptedJobs()

	fetched2, _ := db.GetJob("TEST02")
	if fetched2.Status != "FAILED" {
		t.Errorf("Expected interrupted job to be FAILED, got %s", fetched2.Status)
	}
}
