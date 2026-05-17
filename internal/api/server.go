package api

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/fkvivid/ffmpeg-pipeline/internal/config"
	"github.com/fkvivid/ffmpeg-pipeline/internal/events"
	"github.com/fkvivid/ffmpeg-pipeline/internal/jobs"
	"github.com/fkvivid/ffmpeg-pipeline/internal/pipeline"
)

// Server is the HTTP API and static file handler.
type Server struct {
	cfg    config.Config
	store  *jobs.Store
	broker *events.Broker
	runner *pipeline.Runner
	web    fs.FS
}

// NewServer wires dependencies and routes.
func NewServer(cfg config.Config, store *jobs.Store, broker *events.Broker, web fs.FS) *Server {
	return &Server{
		cfg:    cfg,
		store:  store,
		broker: broker,
		runner: &pipeline.Runner{Store: store, Broker: broker},
		web:    web,
	}
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/stream/", http.StripPrefix("/stream/", http.FileServer(http.Dir(s.store.OutputDirPath()))))
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/events/", s.handleSSE)
	mux.HandleFunc("/api/jobs", s.handleListJobs)
	mux.HandleFunc("/api/jobs/", s.handleJobByID)

	// UI: explicit routes only — a catch-all FileServer on "/" causes redirect loops
	// (301 to "./") when the embedded FS root is treated as a directory.
	assets := http.FileServer(http.FS(s.web))
	mux.HandleFunc("GET /{$}", s.serveIndex)
	mux.HandleFunc("GET /index.html", s.serveIndex)
	mux.Handle("GET /player.html", assets)
	mux.Handle("/static/", assets)

	return mux
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := s.cfg.Addr
	if len(addr) > 0 && addr[0] == ':' {
		fmt.Printf("🎬 Server running at http://localhost%s\n", addr)
	} else {
		fmt.Printf("🎬 Server running at http://%s\n", addr)
	}
	return http.ListenAndServe(addr, s.Handler())
}
