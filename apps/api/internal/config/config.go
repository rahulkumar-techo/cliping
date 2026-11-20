package config

import (
	"os"
	"path/filepath"
	"strconv"
)

// Config holds all server and worker configuration.
type Config struct {
	RepoRoot                  string
	AppEnv                    string
	AppPort                   string
	AppBaseURL                string
	DatabasePath              string
	StoragePath               string
	MaxConcurrentJobs         int
	MaxSourceVideoMB          int64
	MaxSourceDurationMinutes  int
	MaxTempStorageGB          int64
	MinFreeDiskGB             int64
	WhisperModel              string
	YoloModel                 string
	GeminiAPIKey              string
	GeminiModel               string
	YouTubeClientID           string
	YouTubeClientSecret       string
	YouTubeRedirectURI        string
	InstagramAppID            string
	InstagramAppSecret        string
	JobTTLHours               int
	PythonPath                string
	WorkerScript              string
}

// findRepoRoot locates the base repository directory (containing apps/worker).
func findRepoRoot() string {
	candidates := []string{
		".",
		"..",
		"../..",
	}
	for _, c := range candidates {
		target := filepath.Join(c, "apps", "worker", "src", "main.py")
		if _, err := os.Stat(target); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	abs, _ := filepath.Abs(".")
	return abs
}

// Load loads configuration from environment variables with sensible defaults.
func Load() *Config {
	root := findRepoRoot()

	storage := getEnv("STORAGE_PATH", filepath.Join(root, "storage", "jobs"))
	if !filepath.IsAbs(storage) {
		storage = filepath.Join(root, storage)
	}

	dbPath := getEnv("DATABASE_PATH", filepath.Join(root, "data", "app.db"))
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(root, dbPath)
	}

	workerScript := getEnv("WORKER_SCRIPT", filepath.Join(root, "apps", "worker", "src", "main.py"))
	if !filepath.IsAbs(workerScript) {
		workerScript = filepath.Join(root, workerScript)
	}

	defaultPython := "python"
	venvWin := filepath.Join(root, "apps", "worker", "venv", "Scripts", "python.exe")
	venvUnix := filepath.Join(root, "apps", "worker", "venv", "bin", "python")
	if _, err := os.Stat(venvWin); err == nil {
		defaultPython = venvWin
	} else if _, err := os.Stat(venvUnix); err == nil {
		defaultPython = venvUnix
	}
	pythonPath := getEnv("PYTHON_PATH", defaultPython)

	port := getEnv("APP_PORT", "8080")
	defaultBaseURL := "http://localhost:" + port
	baseURL := getEnv("APP_BASE_URL", defaultBaseURL)
	defaultRedirect := baseURL + "/oauth2callback"
	redirectURI := getEnv("YOUTUBE_REDIRECT_URI", defaultRedirect)

	return &Config{
		RepoRoot:                 root,
		AppEnv:                   getEnv("APP_ENV", "development"),
		AppPort:                  port,
		AppBaseURL:               baseURL,
		DatabasePath:             dbPath,
		StoragePath:              storage,
		MaxConcurrentJobs:        getEnvInt("MAX_CONCURRENT_JOBS", 1),
		MaxSourceVideoMB:         getEnvInt64("MAX_SOURCE_VIDEO_MB", 2048),
		MaxSourceDurationMinutes: getEnvInt("MAX_SOURCE_DURATION_MINUTES", 120),
		MaxTempStorageGB:         getEnvInt64("MAX_TEMP_STORAGE_GB", 20),
		MinFreeDiskGB:            getEnvInt64("MIN_FREE_DISK_GB", 8),
		WhisperModel:             getEnv("WHISPER_MODEL", "base"),
		YoloModel:                getEnv("YOLO_MODEL", "yolov8n.pt"),
		GeminiAPIKey:             getEnv("GEMINI_API_KEY", ""),
		GeminiModel:              getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		YouTubeClientID:          getEnv("YOUTUBE_CLIENT_ID", ""),
		YouTubeClientSecret:      getEnv("YOUTUBE_CLIENT_SECRET", ""),
		YouTubeRedirectURI:       redirectURI,
		InstagramAppID:           getEnv("INSTAGRAM_APP_ID", ""),
		InstagramAppSecret:       getEnv("INSTAGRAM_APP_SECRET", ""),
		JobTTLHours:              getEnvInt("JOB_TTL_HOURS", 12),
		PythonPath:               pythonPath,
		WorkerScript:             workerScript,
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvInt64(key string, defaultVal int64) int64 {
	if val, ok := os.LookupEnv(key); ok {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return defaultVal
}
