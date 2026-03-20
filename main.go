package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// DownloadRequest represents the JSON payload from the browser extension.
type DownloadRequest struct {
	URL     string            `json:"url"`
	Cookies string            `json:"cookies"`
	Headers map[string]string `json:"headers"`
}

func main() {
	// Ensure downloads directory exists
	if err := os.MkdirAll("downloads", 0755); err != nil {
		log.Fatalf("Failed to create downloads directory: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /download", handleDownload)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long timeout for large downloads
	}

	log.Println("Video downloader server starting on http://localhost:8080")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// corsMiddleware adds CORS headers so the browser extension can reach us.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleDownload receives a download request and dispatches to the right downloader.
func handleDownload(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, `{"error":"url is required"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[JOB RECEIVED] URL: %s", req.URL)

	// Generate a unique output filename based on timestamp
	timestamp := time.Now().Format("20060102_150405_000")

	var err error
	var outPath string

	switch detectType(req.URL) {
	case videoTypeHLS:
		log.Println("[TYPE] HLS stream detected")
		outPath = fmt.Sprintf("downloads/%s.mp4", timestamp)
		err = downloadHLS(req, outPath)
	default:
		log.Println("[TYPE] Direct video download")
		outPath = fmt.Sprintf("downloads/%s.mp4", timestamp)
		err = downloadDirect(req, outPath)
	}

	if err != nil {
		log.Printf("[ERROR] Download failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	log.Printf("[DONE] Saved to %s", outPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"file":   outPath,
	})
}
