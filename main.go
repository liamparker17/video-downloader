package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func main() {
	// Downloads go to the user's system Downloads folder (see downloader.go)

	ffmpegOk, ffmpegVer := checkTool("ffmpeg")
	if ffmpegOk {
		log.Printf("[STARTUP] ffmpeg: found (%s)", ffmpegVer)
	} else {
		log.Println("[STARTUP] ffmpeg: NOT FOUND — video conversion will not work")
	}

	var ytdlpVer string
	ytdlpAvailable, ytdlpVer = checkYtdlp()
	if ytdlpAvailable {
		log.Printf("[STARTUP] yt-dlp: found (%s)", ytdlpVer)
	} else {
		log.Println("[STARTUP] yt-dlp: NOT FOUND — YouTube/social media downloads will not work")
	}

	store.StartCleanup()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /download", handleCreateJob)
	mux.HandleFunc("GET /jobs/{id}", handleGetJob)
	mux.HandleFunc("GET /jobs", handleListJobs)
	mux.HandleFunc("DELETE /jobs/{id}", handleDeleteJob)
	mux.HandleFunc("GET /health", handleHealth(ffmpegOk, ffmpegVer, ytdlpVer))

	server := &http.Server{
		Addr:         ":8080",
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Println("Video downloader server starting on http://localhost:8080")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	if req.URL == "" && req.PageURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url or pageUrl is required"})
		return
	}

	job := store.CreateJob(req.URL, req.PageURL, req.Title, req.Quality, req.AudioOnly)
	log.Printf("[JOB CREATED] ID: %s, URL: %s, PageURL: %s", job.ID, req.URL, req.PageURL)

	go runPipeline(job, req)

	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": job.ID})
}

func handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := store.GetJob(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func handleListJobs(w http.ResponseWriter, _ *http.Request) {
	jobs := store.ListJobs()
	if jobs == nil {
		jobs = []*Job{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if ok := store.CancelJob(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleHealth(ffmpegOk bool, ffmpegVer, ytdlpVer string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		health := map[string]any{
			"ffmpeg": map[string]any{
				"available": ffmpegOk,
				"version":   ffmpegVer,
			},
			"ytdlp": map[string]any{
				"available": ytdlpAvailable,
				"version":   ytdlpVer,
			},
		}
		writeJSON(w, http.StatusOK, health)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
