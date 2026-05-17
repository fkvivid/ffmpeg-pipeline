package main

import (
	"log"
	"os"

	"github.com/fkvivid/ffmpeg-pipeline/internal/api"
	"github.com/fkvivid/ffmpeg-pipeline/internal/config"
	"github.com/fkvivid/ffmpeg-pipeline/internal/events"
	"github.com/fkvivid/ffmpeg-pipeline/internal/jobs"
	webassets "github.com/fkvivid/ffmpeg-pipeline/web"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.UploadsDir, 0755); err != nil {
		log.Fatalf("create uploads dir: %v", err)
	}
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	store := jobs.NewStore(cfg.UploadsDir, cfg.OutputDir)
	store.LoadFromDisk()

	broker := events.NewBroker()
	srv := api.NewServer(cfg, store, broker, webassets.FS)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
