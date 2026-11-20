package cleanup

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"clipping-api/internal/database"
)

// Cleaner handles TTL deletion and disk space reclamation.
type Cleaner struct {
	db          *database.DB
	storagePath string
	ttlHours    int
}

// NewCleaner creates a new Cleaner instance.
func NewCleaner(db *database.DB, storagePath string, ttlHours int) *Cleaner {
	return &Cleaner{
		db:          db,
		storagePath: storagePath,
		ttlHours:    ttlHours,
	}
}

// Start runs periodic TTL cleanup in the background.
func (c *Cleaner) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	// Run initial cleanup once at startup
	c.RunTTL()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.RunTTL()
		}
	}
}

// RunTTL scans for jobs older than ttlHours and purges their disk files and DB records.
func (c *Cleaner) RunTTL() {
	jobs, err := c.db.GetOldJobs(c.ttlHours)
	if err != nil {
		log.Printf("[CLEANUP] Failed to query old jobs: %v", err)
		return
	}

	for _, j := range jobs {
		jobDir := filepath.Join(c.storagePath, j.ID)
		if err := os.RemoveAll(jobDir); err != nil && !os.IsNotExist(err) {
			log.Printf("[CLEANUP] Failed to remove job dir %s: %v", jobDir, err)
		} else {
			log.Printf("[CLEANUP] Purged expired job directory for %s", j.ID)
		}
		// Also clean DB record
		_ = c.db.DeleteJob(j.ID)
	}
}

// CleanJobDir deletes an entire job's directory.
func (c *Cleaner) CleanJobDir(jobID string) error {
	dir := filepath.Join(c.storagePath, jobID)
	return os.RemoveAll(dir)
}

// CleanIntermediates deletes large temporary files (source video, proxy, audio, extracted frames)
// while retaining final clips.
func (c *Cleaner) CleanIntermediates(jobID string) error {
	jobDir := filepath.Join(c.storagePath, jobID)
	intermediates := []string{
		"source.mp4",
		"proxy.mp4",
		"audio.wav",
		"frames",
		"temp_audio.wav",
	}

	for _, item := range intermediates {
		path := filepath.Join(jobDir, item)
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			log.Printf("[CLEANUP] Warning: could not remove intermediate %s: %v", path, err)
		}
	}
	log.Printf("[CLEANUP] Cleaned intermediate files for job %s", jobID)
	return nil
}
