package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// -- EMBEDDED STATIC FILES -- //

//go:embed static/index.html
//go:embed static/actualizaciones.html
//go:embed static/images/*
var staticFiles embed.FS

// -- UPDATE STATUS STATE -- //

type UpdateStatus struct {
	UpdateAvailable int `json:"update_available"`
	UpdateRequested int `json:"update_requested"`
}

var (
	jsonFilePath = "C:\\nebula-on-premise-windows\\update_status.json"
	updateStatus UpdateStatus
	updateMutex  sync.Mutex
)

// readUpdateStatus loads or defaults the update status from a JSON file.
func readUpdateStatus(logger *log.Logger) {
	updateMutex.Lock()
	defer updateMutex.Unlock()

	file, err := os.ReadFile(jsonFilePath)
	if err != nil {
		logger.Printf("⚠️ Could not read update status file, using default (0). Error %s:", err)
		return
	}

	err = json.Unmarshal(file, &updateStatus)
	if err != nil {
		logger.Printf("⚠️ Could not parse update status JSON, using default (0). Error:%s", err)
	}
}

// setUpdateRequestedStatus updates the requested status in-memory and on disk.
func setUpdateRequestedStatus(value int) error {
	updateMutex.Lock()
	defer updateMutex.Unlock()

	updateStatus.UpdateRequested = value
	file, err := json.MarshalIndent(updateStatus, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(jsonFilePath, file, 0644)
}

// -- HTTP HANDLERS -- //

func checkUpdateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updateStatus)
}

func runUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("⚙️ Running update process...")
	if err := setUpdateRequestedStatus(1); err != nil {
		fmt.Printf("⚠️ Error setting update requested status: %s", err)
		http.Error(w, "Failed to set update requested status", http.StatusInternalServerError)
		return
	}

	// Here you might signal a process manager or otherwise trigger an update...
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Update requested\n"))
}

// corsMiddleware enables Cross-Origin Resource Sharing.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// periodicUpdateCheck runs in a goroutine, periodically re-reads the update file.
func periodicUpdateCheck(ctx context.Context, logger *log.Logger) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			readUpdateStatus(logger)
			if updateStatus.UpdateAvailable == 1 {
				logger.Printf("🔄 Update available! Notifying frontend.")
				// Possibly push a websocket event or set a specific status, etc.
			}
		case <-ctx.Done():
			logger.Printf("🛑 Stopping periodic update check...")
			return
		}
	}
}

// Server wraps an http.Server and additional fields to manage the lifecycle.
type Server struct {
	httpServer *http.Server
	cancel     context.CancelFunc
}

// NewServer creates and configures the HTTP server, starts background tasks.
func NewServer(cfg *Config, logger *log.Logger) (*Server, error) {
	if cfg.HTTPAddr == "" {
		return nil, errors.New("invalid config: HTTPAddr is required")
	}

	// Set up mux and static files
	mux := http.NewServeMux()

	// Serve embedded static files under /static
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("failed to sub static files: %w", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Example route serving an embedded file
	mux.HandleFunc("/nebula", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "Index file not found", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Update-related routes
	mux.HandleFunc("/check-update", checkUpdateHandler)
	mux.HandleFunc("/run-update", runUpdateHandler)

	// Wrap mux with CORS
	handler := corsMiddleware(mux)

	// Create a context we can cancel to stop our background goroutines
	ctx, cancel := context.WithCancel(context.Background())
	go periodicUpdateCheck(ctx, logger)

	// Prepare the standard library http.Server
	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}

	return &Server{
		httpServer: server,
		cancel:     cancel,
	}, nil
}

// Run starts the server (blocking call).
func (s *Server) Run(logger *log.Logger) error {
	logger.Printf("🚀 Server started on %s", s.httpServer.Addr)
	// Start serving; this blocks until the server fails or is shut down
	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		// This usually means we've called Shutdown
		return nil
	}
	return err
}

// Shutdown gracefully stops the server, cancels background work.
func (s *Server) Shutdown(logger *log.Logger) {
	logger.Printf("🛑 Shutting down server...")
	// Stop the periodic checker
	s.cancel()

	// Graceful shutdown with a context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		logger.Printf("Error during shutdown: %s", err)
	} else {
		logger.Printf("✅ Server stopped.")
	}
}
