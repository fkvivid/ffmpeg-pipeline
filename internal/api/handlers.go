package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
	"github.com/fkvivid/ffmpeg-pipeline/internal/probe"
	"github.com/fkvivid/ffmpeg-pipeline/internal/transcode"
)

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.store.List())
}

func (s *Server) handleJobByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if id == "" {
		http.Error(w, "missing job ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		j, ok := s.store.Get(id)
		if !ok {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(j)
	case http.MethodDelete:
		if err := s.store.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<30)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "missing 'video' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), header.Filename)
	savePath := filepath.Join(s.cfg.UploadsDir, filename)

	dst, err := os.Create(savePath)
	if err != nil {
		http.Error(w, "could not create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	size, err := io.Copy(dst, file)
	if err != nil {
		http.Error(w, "could not save file", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Saved: %s\n", savePath)

	info, err := probe.Video(savePath)
	if err != nil {
		http.Error(w, "could not probe video", http.StatusInternalServerError)
		return
	}
	info.Filename = filename
	info.Size = size

	renditions := transcode.PickRenditions(info.Height)
	renditionNames := make([]string, len(renditions))
	for i, r := range renditions {
		renditionNames[i] = r.Name
	}
	fmt.Printf("📊 Probed: %dx%d, %.1fs, encoding: %v\n", info.Width, info.Height, info.Duration, renditionNames)

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	jobDir := filepath.Join(s.cfg.OutputDir, jobID)
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		http.Error(w, "could not create job directory", http.StatusInternalServerError)
		return
	}

	s.store.Save(&model.Job{
		ID:         jobID,
		InputPath:  savePath,
		OutputDir:  jobDir,
		Filename:   header.Filename,
		Status:     "processing",
		Renditions: renditionNames,
		CreatedAt:  time.Now(),
	})

	go s.runner.Run(jobID, savePath, jobDir, renditions, info.Duration)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model.UploadResponse{
		JobID:      jobID,
		Info:       info,
		Renditions: renditionNames,
	})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/api/events/")
	if jobID == "" {
		http.Error(w, "missing job ID", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := s.broker.Subscribe(jobID)
	defer s.broker.Unsubscribe(jobID, ch)

	fmt.Fprintf(w, "data: {\"event\":\"connected\"}\n\n")
	flusher.Flush()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
