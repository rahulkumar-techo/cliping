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

// CleanAllTemp scans storagePath and removes all temporary/intermediate files across all jobs.
// It also completely purges failed or cancelled job folders, returning bytes freed and count of cleaned jobs.
func (c *Cleaner) CleanAllTemp() (int64, int, error) {
	entries, err := os.ReadDir(c.storagePath)
	if err != nil {
		return 0, 0, err
	}

	var totalFreed int64
	cleanedJobs := 0

	intermediates := []string{
		"source.mp4",
		"proxy.mp4",
		"audio.wav",
		"temp_audio.wav",
		"frames",
		"source.f399.mp4",
		"source.f140-2.m4a",
		"test_nokey_proxy.mp4",
		"speed_test.mp4",
		"proxy_fast.mp4",
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jobID := entry.Name()
		jobDir := filepath.Join(c.storagePath, jobID)

		job, _ := c.db.GetJob(jobID)
		if job != nil && (job.Status == "FAILED" || job.Status == "CANCELLED") {
			var dirSize int64
			_ = filepath.Walk(jobDir, func(_ string, info os.FileInfo, err error) error {
				if err == nil && info != nil && !info.IsDir() {
					dirSize += info.Size()
				}
				return nil
			})
			if err := os.RemoveAll(jobDir); err == nil {
				totalFreed += dirSize
				cleanedJobs++
			}
			continue
		}

		jobCleaned := false
		for _, item := range intermediates {
			p := filepath.Join(jobDir, item)
			if info, err := os.Stat(p); err == nil {
				sz := info.Size()
				if err := os.RemoveAll(p); err == nil {
					totalFreed += sz
					jobCleaned = true
				}
			}
		}
		if jobCleaned {
			cleanedJobs++
		}
	}

	log.Printf("[CLEANUP] Manual cleanup finished: %d jobs cleaned, %d MB reclaimed", cleanedJobs, totalFreed/(1024*1024))
	return totalFreed, cleanedJobs, nil
}
