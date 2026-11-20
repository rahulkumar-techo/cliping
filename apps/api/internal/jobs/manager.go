package jobs

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"clipping-api/internal/cleanup"
	"clipping-api/internal/config"
	"clipping-api/internal/database"
	"clipping-api/internal/sse"
)

// WorkerEvent is emitted by the Python worker over stdout.
type WorkerEvent struct {
	Event    string         `json:"event"`
	Stage    string         `json:"stage,omitempty"`
	Progress int            `json:"progress,omitempty"`
	Percent  int            `json:"percent,omitempty"`
	Detail   string         `json:"detail,omitempty"`
	Level    string         `json:"level,omitempty"`
	Message  string         `json:"message,omitempty"`
	Code     string         `json:"code,omitempty"`
	Clip     *database.Clip `json:"clip,omitempty"`
}

// Manager orchestrates job queueing, resource safety checks, and single-worker execution.
type Manager struct {
	cfg         *config.Config
	db          *database.DB
	hub         *sse.Hub
	cleaner     *cleanup.Cleaner
	mu          sync.Mutex
	activeJobID string
	activeCmd   *exec.Cmd
	cancelFunc  context.CancelFunc
}

// NewManager creates a new Job Manager.
func NewManager(cfg *config.Config, db *database.DB, hub *sse.Hub, cleaner *cleanup.Cleaner) *Manager {
	return &Manager{
		cfg:     cfg,
		db:      db,
		hub:     hub,
		cleaner: cleaner,
	}
}

// ComputeFingerprint computes a SHA-256 fingerprint for duplicate job detection.
func ComputeFingerprint(youtubeURL string, duration, count int, smartTrack, captions, audioSync, aiRank bool) string {
	raw := fmt.Sprintf("%s|%d|%d|%t|%t|%t|%t",
		youtubeURL, duration, count, smartTrack, captions, audioSync, aiRank)
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

// StartQueueWorker runs the continuous loop that processes queued jobs one by one.
func (m *Manager) StartQueueWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			hasActive := m.activeJobID != ""
			m.mu.Unlock()

			if hasActive {
				continue
			}

			// Poll for next queued job
			job, err := m.db.GetNextQueuedJob()
			if err != nil {
				log.Printf("[MANAGER] Error querying queued job: %v", err)
				continue
			}
			if job == nil {
				continue
			}

			// Process job
			m.processJob(job)
		}
	}
}

// processJob executes a single job through the Python AI/video worker.
func (m *Manager) processJob(job *database.Job) {
	// 1. Check free disk space before starting
	freeGB, err := getFreeDiskSpaceGB(m.cfg.StoragePath)
	if err != nil {
		log.Printf("[MANAGER] Warning: unable to check free disk space: %v", err)
	} else if freeGB < float64(m.cfg.MinFreeDiskGB) {
		errMsg := fmt.Sprintf("Insufficient disk space: %.1f GB available, minimum %.1f GB required", freeGB, float64(m.cfg.MinFreeDiskGB))
		errCode := "INSUFFICIENT_DISK"
		_ = m.db.UpdateJobStatus(job.ID, "FAILED", "Checking disk space", 0, &errCode, &errMsg)
		m.hub.BroadcastError(job.ID, errCode, errMsg)
		return
	}

	// 2. Prepare job directory
	jobDir := filepath.Join(m.cfg.StoragePath, job.ID)
	if err := os.MkdirAll(filepath.Join(jobDir, "final"), 0755); err != nil {
		errMsg := fmt.Sprintf("Failed to create job directory: %v", err)
		errCode := "FS_ERROR"
		_ = m.db.UpdateJobStatus(job.ID, "FAILED", "Initializing directory", 0, &errCode, &errMsg)
		m.hub.BroadcastError(job.ID, errCode, errMsg)
		return
	}

	jobCtx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	m.activeJobID = job.ID
	m.cancelFunc = cancel
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.activeJobID = ""
		m.activeCmd = nil
		m.cancelFunc = nil
		m.mu.Unlock()
	}()

	_ = m.db.UpdateJobStatus(job.ID, "DOWNLOADING", "Downloading", 5, nil, nil)
	m.hub.BroadcastStage(job.ID, "Downloading", 5)
	m.hub.BroadcastLog(job.ID, "Downloading", "INFO", fmt.Sprintf("Starting job %s for %s", job.ID, job.YouTubeURL))

	// 3. Spawn Python worker process
	cmd := exec.CommandContext(jobCtx, m.cfg.PythonPath, m.cfg.WorkerScript, "--job-id", job.ID, "--db-path", m.cfg.DatabasePath, "--storage-path", m.cfg.StoragePath)
	cmd.Dir = filepath.Dir(m.cfg.WorkerScript)

	// Inject environment configurations to worker
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GEMINI_API_KEY=%s", m.cfg.GeminiAPIKey),
		fmt.Sprintf("GEMINI_MODEL=%s", m.cfg.GeminiModel),
		fmt.Sprintf("WHISPER_MODEL=%s", m.cfg.WhisperModel),
		fmt.Sprintf("YOLO_MODEL=%s", m.cfg.YoloModel),
		fmt.Sprintf("MAX_CONCURRENT_JOBS=%d", m.cfg.MaxConcurrentJobs),
		fmt.Sprintf("YOUTUBE_CLIENT_ID=%s", m.cfg.YouTubeClientID),
		fmt.Sprintf("YOUTUBE_CLIENT_SECRET=%s", m.cfg.YouTubeClientSecret),
		fmt.Sprintf("INSTAGRAM_APP_ID=%s", m.cfg.InstagramAppID),
		fmt.Sprintf("INSTAGRAM_APP_SECRET=%s", m.cfg.InstagramAppSecret),
	)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		m.handleJobError(job.ID, "PROCESS_PIPE_ERROR", err.Error())
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		m.handleJobError(job.ID, "PROCESS_PIPE_ERROR", err.Error())
		return
	}

	m.mu.Lock()
	m.activeCmd = cmd
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
		m.handleJobError(job.ID, "WORKER_START_FAILED", err.Error())
		return
	}

	// Stream stderr for debugging/logging
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			text := scanner.Text()
			log.Printf("[WORKER-STDERR] [%s] %s", job.ID, text)
			m.hub.BroadcastLog(job.ID, "Worker", "DEBUG", text)
		}
	}()

	// Read structured stdout lines
	reader := bufio.NewReader(stdoutPipe)
	failed := false
	var failCode, failMsg string

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var ev WorkerEvent
			if jsonErr := json.Unmarshal(line, &ev); jsonErr == nil {
				m.handleWorkerEvent(job.ID, &ev)
				if ev.Event == "error" {
					failed = true
					failCode = ev.Code
					failMsg = ev.Message
				}
			} else {
				// Raw log line fallback
				msg := string(line)
				m.hub.BroadcastLog(job.ID, "Processing", "INFO", msg)
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[MANAGER] Stdout read error: %v", err)
			}
			break
		}
	}

	waitErr := cmd.Wait()

	// Check if cancelled
	latestJob, _ := m.db.GetJob(job.ID)
	if latestJob != nil && latestJob.Status == "CANCELLED" {
		log.Printf("[MANAGER] Job %s was cancelled by user.", job.ID)
		_ = m.cleaner.CleanJobDir(job.ID)
		return
	}

	if failed {
		m.handleJobError(job.ID, failCode, failMsg)
		return
	}

	if waitErr != nil {
		m.handleJobError(job.ID, "WORKER_EXIT_ERROR", waitErr.Error())
		return
	}

	// Mark completed
	_ = m.db.UpdateJobStatus(job.ID, "COMPLETED", "Completed", 100, nil, nil)
	m.hub.BroadcastStage(job.ID, "Completed", 100)
	m.hub.BroadcastCompleted(job.ID)
	log.Printf("[MANAGER] Job %s completed successfully.", job.ID)
}

func (m *Manager) handleWorkerEvent(jobID string, ev *WorkerEvent) {
	switch ev.Event {
	case "stage":
		status := mapStageToStatus(ev.Stage)
		_ = m.db.UpdateJobStatus(jobID, status, ev.Stage, ev.Progress, nil, nil)
		m.hub.BroadcastStage(jobID, ev.Stage, ev.Progress)
	case "progress":
		_ = m.db.UpdateJobStatus(jobID, "", ev.Stage, ev.Progress, nil, nil)
		m.hub.BroadcastProgress(jobID, ev.Progress, ev.Stage)
	case "step_progress":
		m.hub.BroadcastStepProgress(jobID, ev.Stage, ev.Percent, ev.Detail)
	case "log":
		m.hub.BroadcastLog(jobID, ev.Stage, ev.Level, ev.Message)
	case "clip":
		if ev.Clip != nil {
			_ = m.db.SaveClip(ev.Clip)
		}
	case "completed":
		_ = m.db.UpdateJobStatus(jobID, "COMPLETED", "Completed", 100, nil, nil)
		m.hub.BroadcastStage(jobID, "Completed", 100)
		m.hub.BroadcastCompleted(jobID)
	case "error":
		m.handleJobError(jobID, ev.Code, ev.Message)
	}
}

func (m *Manager) handleJobError(jobID, code, message string) {
	_ = m.db.UpdateJobStatus(jobID, "FAILED", "Failed", 0, &code, &message)
	m.hub.BroadcastError(jobID, code, message)
	log.Printf("[MANAGER] Job %s failed: [%s] %s", jobID, code, message)

	// Automatically clean temporary storage on job failure
	if err := m.cleaner.CleanJobDir(jobID); err != nil {
		log.Printf("[MANAGER] Warning: could not clean storage for failed job %s: %v", jobID, err)
	} else {
		log.Printf("[MANAGER] Successfully purged temporary storage for failed job %s", jobID)
	}
}

// Cancel cancels the currently running job or marks a queued job as cancelled.
func (m *Manager) Cancel(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, err := m.db.GetJob(jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job not found")
	}

	if job.Status == "COMPLETED" || job.Status == "FAILED" || job.Status == "CANCELLED" {
		return fmt.Errorf("job is already in terminal state %s", job.Status)
	}

	_ = m.db.UpdateJobStatus(jobID, "CANCELLED", "Cancelled by user", 0, nil, nil)
	m.hub.BroadcastLog(jobID, "Cancellation", "WARN", "Job cancelled by user")

	if m.activeJobID == jobID && m.cancelFunc != nil {
		m.cancelFunc()
		if m.activeCmd != nil && m.activeCmd.Process != nil {
			_ = m.activeCmd.Process.Kill()
		}
	}

	_ = m.cleaner.CleanJobDir(jobID)
	return nil
}

func mapStageToStatus(stage string) string {
	switch stage {
	case "Validating URL":
		return "QUEUED"
	case "Downloading":
		return "DOWNLOADING"
	case "Extracting audio", "Creating analysis proxy", "Detecting scenes", "Analyzing audio":
		return "ANALYZING"
	case "Transcribing":
		return "TRANSCRIBING"
	case "Generating candidates":
		return "GENERATING_CANDIDATES"
	case "AI ranking":
		return "AI_RANKING"
	case "Detecting subjects", "Tracking subject", "Smart reframing":
		return "TRACKING"
	case "Generating captions", "Rendering":
		return "RENDERING"
	case "Uploading":
		return "UPLOADING"
	case "Cleanup":
		return "CLEANUP"
	case "Completed":
		return "COMPLETED"
	default:
		return "ANALYZING"
	}
}

// getFreeDiskSpaceGB returns available disk space on the drive of the given path.
func getFreeDiskSpaceGB(path string) (float64, error) {
	if runtime.GOOS == "windows" {
		// Windows disk query
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

		var freeBytes, totalBytes, totalFreeBytes int64
		p, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return 50.0, nil
		}

		r, _, err := getDiskFreeSpaceEx.Call(
			uintptr(unsafe.Pointer(p)),
			uintptr(unsafe.Pointer(&freeBytes)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&totalFreeBytes)),
		)
		if r == 0 {
			// Fallback if path doesn't exist yet: check root volume
			root, _ := syscall.UTF16PtrFromString(filepath.VolumeName(path) + "\\")
			r, _, err = getDiskFreeSpaceEx.Call(
				uintptr(unsafe.Pointer(root)),
				uintptr(unsafe.Pointer(&freeBytes)),
				uintptr(unsafe.Pointer(&totalBytes)),
				uintptr(unsafe.Pointer(&totalFreeBytes)),
			)
			if r == 0 {
				return 32.0, nil // Default safe assumption on failure
			}
		}
		return float64(freeBytes) / (1024 * 1024 * 1024), nil
	}

	// Linux / Unix fallback
	return 20.0, nil
}
