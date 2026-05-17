package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fkvivid/ffmpeg-pipeline/internal/events"
	"github.com/fkvivid/ffmpeg-pipeline/internal/jobs"
	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
	"github.com/fkvivid/ffmpeg-pipeline/internal/transcode"
	"github.com/fkvivid/ffmpeg-pipeline/internal/vmaf"
)

// Runner executes transcode + VMAF for a single job.
type Runner struct {
	Store  *jobs.Store
	Broker *events.Broker
}

// Run processes one job asynchronously (call from a goroutine).
func (r *Runner) Run(jobID, inputPath, outputDir string, renditions []model.Rendition, duration float64) {
	start := time.Now()
	fmt.Printf("🎬 [%s] Starting parallel transcode of %d renditions...\n", jobID, len(renditions))

	if err := transcode.All(context.Background(), jobID, inputPath, outputDir, renditions, duration, r.Broker); err != nil {
		fmt.Printf("❌ [%s] Failed: %v\n", jobID, err)
		r.Store.UpdateStatus(jobID, "failed")
		publish(r.Broker, jobID, map[string]any{"event": "error", "error": err.Error()})
		return
	}

	fmt.Printf("✅ [%s] Transcode done in %s\n", jobID, time.Since(start).Round(time.Second))

	r.Store.UpdateStatus(jobID, "scoring")
	publish(r.Broker, jobID, map[string]any{"event": "status", "status": "scoring"})

	vmafStart := time.Now()
	scores, err := vmaf.All(context.Background(), jobID, inputPath, outputDir, renditions, r.Broker)
	if err != nil {
		fmt.Printf("⚠️  [%s] VMAF scoring had errors: %v\n", jobID, err)
	}

	r.Store.SaveVMAF(jobID, scores)
	fmt.Printf("✅ [%s] VMAF done in %s\n", jobID, time.Since(vmafStart).Round(time.Second))
	fmt.Printf("🎉 [%s] All done in %s\n", jobID, time.Since(start).Round(time.Second))

	r.Store.UpdateStatus(jobID, "done")
	publish(r.Broker, jobID, map[string]any{"event": "done"})
}

func publish(broker *events.Broker, jobID string, data map[string]any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	broker.Publish(jobID, b)
}
