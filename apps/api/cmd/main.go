package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"clipping-api/internal/cleanup"
	"clipping-api/internal/config"
	"clipping-api/internal/database"
	"clipping-api/internal/handlers"
	"clipping-api/internal/jobs"
	"clipping-api/internal/sse"
)

func main() {
	log.Println("[API] Starting Personal AI Video Clipping API Server...")

	cfg := config.Load()

	// Locate web directory (templates and static)
	webDir := filepath.Join(cfg.RepoRoot, "apps", "web")
	absWebDir, _ := filepath.Abs(webDir)
	log.Printf("[API] Web directory: %s", absWebDir)
	log.Printf("[API] Worker script: %s", cfg.WorkerScript)

	// Initialize SQLite Database
	db, err := database.New(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("[API] Database initialization failed: %v", err)
	}
	defer db.Close()
	log.Printf("[API] Database initialized at: %s", cfg.DatabasePath)

	// Clean up interrupted jobs from previous unclean shutdown
	if err := db.ResetInterruptedJobs(); err != nil {
		log.Printf("[API] Warning: Reset interrupted jobs error: %v", err)
	}

	// SSE Hub
	hub := sse.NewHub()

	// Cleanup service
	cleaner := cleanup.NewCleaner(db, cfg.StoragePath, cfg.JobTTLHours)
	cleanerCtx, cancelCleaner := context.WithCancel(context.Background())
	defer cancelCleaner()
	go cleaner.Start(cleanerCtx)

	// Job Orchestrator Manager
	manager := jobs.NewManager(cfg, db, hub, cleaner)
	managerCtx, cancelManager := context.WithCancel(context.Background())
	defer cancelManager()
	go manager.StartQueueWorker(managerCtx)

	// Server and Routes
	srv := handlers.NewServer(cfg, db, hub, manager, cleaner, absWebDir)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	addr := fmt.Sprintf("0.0.0.0:%s", cfg.AppPort)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // Disable timeout for SSE streaming
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown channel
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("")
		fmt.Println("==================================================================")
		fmt.Printf("🚀 ClipCraft AI Server is LIVE and READY!\n")
		fmt.Printf("👉 Open in your browser: http://localhost:%s\n", cfg.AppPort)
		fmt.Println("👉 Note: Keep this terminal window open while using the app.")
		fmt.Println("👉 Press Ctrl+C in this terminal whenever you want to stop.")
		fmt.Println("==================================================================")
		fmt.Println("")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[API] HTTP server fatal error: %v", err)
		}
	}()

	<-shutdownCh
	log.Println("[API] Shutting down gracefully...")

	cancelCleaner()
	cancelManager()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("[API] Server forced to shutdown: %v", err)
	}

	log.Println("[API] Server exited cleanly.")
}
