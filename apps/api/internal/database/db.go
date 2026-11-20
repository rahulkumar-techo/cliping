package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps sql.DB with synchronized helpers.
type DB struct {
	*sql.DB
	mu sync.Mutex
}

// Job represents a video processing job.
type Job struct {
	ID             string     `json:"id"`
	Fingerprint    string     `json:"fingerprint"`
	YouTubeURL     string     `json:"youtube_url"`
	ClipDuration   int        `json:"clip_duration"`
	ClipCount      int        `json:"clip_count"`
	AspectRatio    string     `json:"aspect_ratio"`
	SmartTracking  bool       `json:"smart_tracking"`
	AutoCaptions   bool       `json:"auto_captions"`
	AudioSync      bool       `json:"audio_sync"`
	AIRanking      bool       `json:"ai_ranking"`
	AutoPublish    string     `json:"auto_publish"`
	Status         string     `json:"status"`
	Progress       int        `json:"progress"`
	CurrentStage   string     `json:"current_stage"`
	ErrorCode      *string    `json:"error_code"`
	ErrorMessage   *string    `json:"error_message"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

// Clip represents a generated video clip.
type Clip struct {
	ID            string    `json:"id"`
	JobID         string    `json:"job_id"`
	ClipIndex     int       `json:"clip_index"`
	StartTime     float64   `json:"start_time"`
	EndTime       float64   `json:"end_time"`
	Duration      float64   `json:"duration"`
	LocalScore    float64   `json:"local_score"`
	GeminiScore   float64   `json:"gemini_score"`
	FinalScore    float64   `json:"final_score"`
	Reason        string    `json:"reason"`
	FilePath      string    `json:"file_path"`
	ThumbnailPath string    `json:"thumbnail_path"`
	UploadStatus  string    `json:"upload_status"`
	CreatedAt     time.Time `json:"created_at"`
}

// Upload represents a published clip record.
type Upload struct {
	ID         string    `json:"id"`
	ClipID     string    `json:"clip_id"`
	Platform   string    `json:"platform"`
	Status     string    `json:"status"`
	ExternalID *string   `json:"external_id"`
	Error      *string   `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
}

// New initializes the SQLite database and executes migrations.
func New(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	db.SetMaxOpenConns(1) // Avoid database locked issues on low-resource machines

	d := &DB{DB: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return d, nil
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		fingerprint TEXT,
		youtube_url TEXT NOT NULL,
		clip_duration INTEGER NOT NULL,
		clip_count INTEGER NOT NULL,
		aspect_ratio TEXT NOT NULL DEFAULT '9:16',
		smart_tracking INTEGER NOT NULL DEFAULT 1,
		auto_captions INTEGER NOT NULL DEFAULT 1,
		audio_sync INTEGER NOT NULL DEFAULT 1,
		ai_ranking INTEGER NOT NULL DEFAULT 1,
		auto_publish TEXT DEFAULT '',
		status TEXT NOT NULL DEFAULT 'QUEUED',
		progress INTEGER NOT NULL DEFAULT 0,
		current_stage TEXT NOT NULL DEFAULT 'Validating URL',
		error_code TEXT,
		error_message TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		completed_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	CREATE INDEX IF NOT EXISTS idx_jobs_fingerprint ON jobs(fingerprint);

	CREATE TABLE IF NOT EXISTS clips (
		id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
		clip_index INTEGER NOT NULL DEFAULT 1,
		start_time REAL NOT NULL,
		end_time REAL NOT NULL,
		duration REAL NOT NULL,
		local_score REAL NOT NULL DEFAULT 0,
		gemini_score REAL NOT NULL DEFAULT 0,
		final_score REAL NOT NULL DEFAULT 0,
		reason TEXT NOT NULL DEFAULT '',
		file_path TEXT NOT NULL DEFAULT '',
		thumbnail_path TEXT NOT NULL DEFAULT '',
		upload_status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_clips_job_id ON clips(job_id);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS uploads (
		id TEXT PRIMARY KEY,
		clip_id TEXT NOT NULL REFERENCES clips(id) ON DELETE CASCADE,
		platform TEXT NOT NULL,
		status TEXT NOT NULL,
		external_id TEXT,
		error TEXT,
		created_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_uploads_clip_id ON uploads(clip_id);
	`
	_, err := d.Exec(schema)
	return err
}

// ResetInterruptedJobs marks any previously RUNNING or intermediate jobs as FAILED on startup.
func (d *DB) ResetInterruptedJobs() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.Exec(`
		UPDATE jobs
		SET status = 'FAILED',
		    error_code = 'SERVER_RESTARTED',
		    error_message = 'Job was interrupted by server restart',
		    updated_at = ?
		WHERE status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'QUEUED')
	`, time.Now())
	return err
}

// CreateJob inserts a new job into the database.
func (d *DB) CreateJob(j *Job) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	j.CreatedAt = now
	j.UpdatedAt = now
	if j.Status == "" {
		j.Status = "QUEUED"
	}

	_, err := d.Exec(`
		INSERT INTO jobs (
			id, fingerprint, youtube_url, clip_duration, clip_count, aspect_ratio,
			smart_tracking, auto_captions, audio_sync, ai_ranking, auto_publish,
			status, progress, current_stage, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		j.ID, j.Fingerprint, j.YouTubeURL, j.ClipDuration, j.ClipCount, j.AspectRatio,
		boolToInt(j.SmartTracking), boolToInt(j.AutoCaptions), boolToInt(j.AudioSync),
		boolToInt(j.AIRanking), j.AutoPublish, j.Status, j.Progress, j.CurrentStage,
		j.CreatedAt, j.UpdatedAt,
	)
	return err
}

// FindRecentByFingerprint looks for an already completed job with the exact same fingerprint.
func (d *DB) FindRecentByFingerprint(fingerprint string, maxAgeHours int) (*Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAgeHours) * time.Hour)
	row := d.QueryRow(`
		SELECT id, fingerprint, youtube_url, clip_duration, clip_count, aspect_ratio,
		       smart_tracking, auto_captions, audio_sync, ai_ranking, auto_publish,
		       status, progress, current_stage, error_code, error_message,
		       created_at, updated_at, completed_at
		FROM jobs
		WHERE fingerprint = ? AND status = 'COMPLETED' AND completed_at >= ?
		ORDER BY completed_at DESC LIMIT 1
	`, fingerprint, cutoff)

	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// GetJob retrieves a job by ID.
func (d *DB) GetJob(id string) (*Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	row := d.QueryRow(`
		SELECT id, fingerprint, youtube_url, clip_duration, clip_count, aspect_ratio,
		       smart_tracking, auto_captions, audio_sync, ai_ranking, auto_publish,
		       status, progress, current_stage, error_code, error_message,
		       created_at, updated_at, completed_at
		FROM jobs WHERE id = ?
	`, id)

	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// GetNextQueuedJob returns the next job with status 'QUEUED' ordered by created_at.
func (d *DB) GetNextQueuedJob() (*Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	row := d.QueryRow(`
		SELECT id, fingerprint, youtube_url, clip_duration, clip_count, aspect_ratio,
		       smart_tracking, auto_captions, audio_sync, ai_ranking, auto_publish,
		       status, progress, current_stage, error_code, error_message,
		       created_at, updated_at, completed_at
		FROM jobs WHERE status = 'QUEUED' ORDER BY created_at ASC LIMIT 1
	`)

	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// UpdateJobStatus updates the status, stage, and progress of a job.
func (d *DB) UpdateJobStatus(id, status, stage string, progress int, errCode, errMsg *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	var completedAt *time.Time
	if status == "COMPLETED" || status == "FAILED" || status == "CANCELLED" {
		completedAt = &now
	}

	_, err := d.Exec(`
		UPDATE jobs
		SET status = ?, current_stage = ?, progress = ?,
		    error_code = ?, error_message = ?, updated_at = ?,
		    completed_at = COALESCE(?, completed_at)
		WHERE id = ?
	`, status, stage, progress, errCode, errMsg, now, completedAt, id)
	return err
}

// GetActiveJob returns the currently processing job, if any.
func (d *DB) GetActiveJob() (*Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	row := d.QueryRow(`
		SELECT id, fingerprint, youtube_url, clip_duration, clip_count, aspect_ratio,
		       smart_tracking, auto_captions, audio_sync, ai_ranking, auto_publish,
		       status, progress, current_stage, error_code, error_message,
		       created_at, updated_at, completed_at
		FROM jobs
		WHERE status NOT IN ('QUEUED', 'COMPLETED', 'FAILED', 'CANCELLED')
		ORDER BY updated_at DESC LIMIT 1
	`)

	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// GetQueueLength returns the count of queued jobs.
func (d *DB) GetQueueLength() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var count int
	err := d.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status = 'QUEUED'`).Scan(&count)
	return count, err
}

// SaveClip inserts or updates a clip record.
func (d *DB) SaveClip(c *Clip) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	c.CreatedAt = time.Now()
	_, err := d.Exec(`
		INSERT INTO clips (
			id, job_id, clip_index, start_time, end_time, duration,
			local_score, gemini_score, final_score, reason,
			file_path, thumbnail_path, upload_status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			file_path = excluded.file_path,
			thumbnail_path = excluded.thumbnail_path,
			upload_status = excluded.upload_status
	`,
		c.ID, c.JobID, c.ClipIndex, c.StartTime, c.EndTime, c.Duration,
		c.LocalScore, c.GeminiScore, c.FinalScore, c.Reason,
		c.FilePath, c.ThumbnailPath, c.UploadStatus, c.CreatedAt,
	)
	return err
}

// GetClipsByJobID returns all clips for a job.
func (d *DB) GetClipsByJobID(jobID string) ([]Clip, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.Query(`
		SELECT id, job_id, clip_index, start_time, end_time, duration,
		       local_score, gemini_score, final_score, reason,
		       file_path, thumbnail_path, upload_status, created_at
		FROM clips WHERE job_id = ? ORDER BY clip_index ASC
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clips []Clip
	for rows.Next() {
		var c Clip
		if err := rows.Scan(
			&c.ID, &c.JobID, &c.ClipIndex, &c.StartTime, &c.EndTime, &c.Duration,
			&c.LocalScore, &c.GeminiScore, &c.FinalScore, &c.Reason,
			&c.FilePath, &c.ThumbnailPath, &c.UploadStatus, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		clips = append(clips, c)
	}
	return clips, nil
}

// GetClip retrieves a single clip by ID.
func (d *DB) GetClip(id string) (*Clip, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var c Clip
	err := d.QueryRow(`
		SELECT id, job_id, clip_index, start_time, end_time, duration,
		       local_score, gemini_score, final_score, reason,
		       file_path, thumbnail_path, upload_status, created_at
		FROM clips WHERE id = ?
	`, id).Scan(
		&c.ID, &c.JobID, &c.ClipIndex, &c.StartTime, &c.EndTime, &c.Duration,
		&c.LocalScore, &c.GeminiScore, &c.FinalScore, &c.Reason,
		&c.FilePath, &c.ThumbnailPath, &c.UploadStatus, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateClipUploadStatus updates the upload status of a clip.
func (d *DB) UpdateClipUploadStatus(clipID, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.Exec(`UPDATE clips SET upload_status = ? WHERE id = ?`, status, clipID)
	return err
}

// RecordUpload inserts a record in uploads table.
func (d *DB) RecordUpload(u *Upload) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	u.CreatedAt = time.Now()
	_, err := d.Exec(`
		INSERT INTO uploads (id, clip_id, platform, status, external_id, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, u.ID, u.ClipID, u.Platform, u.Status, u.ExternalID, u.Error, u.CreatedAt)
	return err
}

// DeleteJob removes a job and cascades to its clips.
func (d *DB) DeleteJob(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}

// GetOldJobs returns completed or failed jobs older than maxAgeHours for TTL cleanup.
func (d *DB) GetOldJobs(maxAgeHours int) ([]Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAgeHours) * time.Hour)
	rows, err := d.Query(`
		SELECT id, fingerprint, youtube_url, clip_duration, clip_count, aspect_ratio,
		       smart_tracking, auto_captions, audio_sync, ai_ranking, auto_publish,
		       status, progress, current_stage, error_code, error_message,
		       created_at, updated_at, completed_at
		FROM jobs
		WHERE (status IN ('COMPLETED', 'FAILED', 'CANCELLED')) AND updated_at < ?
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, nil
}

// GetSetting retrieves a setting value by key.
func (d *DB) GetSetting(key string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var val string
	err := d.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// SetSetting sets a key-value setting.
func (d *DB) SetSetting(key, val string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, val)
	return err
}

// GetAllSettings returns all settings as a map.
func (d *DB) GetAllSettings() (map[string]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		res[k] = v
	}
	return res, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanJob(s rowScanner) (*Job, error) {
	var j Job
	var smartTracking, autoCaptions, audioSync, aiRanking int
	err := s.Scan(
		&j.ID, &j.Fingerprint, &j.YouTubeURL, &j.ClipDuration, &j.ClipCount, &j.AspectRatio,
		&smartTracking, &autoCaptions, &audioSync, &aiRanking, &j.AutoPublish,
		&j.Status, &j.Progress, &j.CurrentStage, &j.ErrorCode, &j.ErrorMessage,
		&j.CreatedAt, &j.UpdatedAt, &j.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	j.SmartTracking = smartTracking == 1
	j.AutoCaptions = autoCaptions == 1
	j.AudioSync = audioSync == 1
	j.AIRanking = aiRanking == 1
	return &j, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
