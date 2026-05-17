package vmaf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fkvivid/ffmpeg-pipeline/internal/events"
	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
)

// All scores every rendition in parallel and publishes SSE events.
func All(ctx context.Context, jobID, referencePath, outputDir string, renditions []model.Rendition, broker *events.Broker) ([]model.VMAFScore, error) {
	type result struct {
		score *model.VMAFScore
		err   error
	}

	resultCh := make(chan result, len(renditions))
	var wg sync.WaitGroup

	for _, r := range renditions {
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()

			publish(broker, jobID, map[string]any{
				"event":     "vmaf_start",
				"rendition": r.Name,
			})

			score, err := runOne(ctx, referencePath, outputDir, r.Name)
			resultCh <- result{score, err}

			if err != nil {
				fmt.Printf("⚠️  [%s] VMAF failed: %v\n", r.Name, err)
				return
			}

			publish(broker, jobID, map[string]any{
				"event":     "vmaf_score",
				"rendition": r.Name,
				"mean":      score.Mean,
				"min":       score.Min,
				"max":       score.Max,
			})
			fmt.Printf("📊 [%s] VMAF mean: %.2f\n", r.Name, score.Mean)
		}()
	}

	wg.Wait()
	close(resultCh)

	var scores []model.VMAFScore
	for res := range resultCh {
		if res.err != nil {
			continue
		}
		scores = append(scores, *res.score)
	}
	return scores, nil
}

func runOne(ctx context.Context, referencePath, outputDir, renditionName string) (*model.VMAFScore, error) {
	renditionPlaylist := filepath.Join(outputDir, renditionName, "stream.m3u8")
	logPath := filepath.Join(outputDir, renditionName, "vmaf.json")

	vmafFilter := fmt.Sprintf(
		"[0:v]format=yuv420p10le,setpts=PTS-STARTPTS,settb=AVTB[dist];"+
			"[1:v]format=yuv420p10le,setpts=PTS-STARTPTS,settb=AVTB[ref];"+
			"[dist][ref]libvmaf=shortest=true:ts_sync_mode=nearest:n_threads=4:log_path=%s:log_fmt=json",
		logPath,
	)

	args := []string{
		"-i", renditionPlaylist,
		"-i", referencePath,
		"-filter_complex", vmafFilter,
		"-an", "-sn", "-dn",
		"-f", "null",
		"-",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg vmaf %s: %w\nstderr:\n%s",
			renditionName, err, stderr.String())
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, fmt.Errorf("read vmaf log: %w", err)
	}

	var report model.VMAFReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse vmaf log: %w", err)
	}

	return &model.VMAFScore{
		Rendition: renditionName,
		Mean:      report.PooledMetrics.VMAF.Mean,
		Min:       report.PooledMetrics.VMAF.Min,
		Max:       report.PooledMetrics.VMAF.Max,
	}, nil
}

func publish(broker *events.Broker, jobID string, data map[string]any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	broker.Publish(jobID, b)
}
