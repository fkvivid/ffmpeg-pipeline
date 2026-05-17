package transcode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fkvivid/ffmpeg-pipeline/internal/events"
	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
	"golang.org/x/sync/errgroup"
)

// All runs parallel ffmpeg transcodes and writes the master playlist.
func All(ctx context.Context, jobID, inputPath, outputDir string, renditions []model.Rendition, duration float64, broker *events.Broker) error {
	g, ctx := errgroup.WithContext(ctx)
	progressCh := make(chan model.Progress, 50)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for p := range progressCh {
			p.JobID = jobID
			b, _ := json.Marshal(map[string]any{
				"event":     "progress",
				"job_id":    p.JobID,
				"rendition": p.Rendition,
				"percent":   p.Percent,
				"fps":       p.FPS,
				"speed":     p.Speed,
				"done":      p.Done,
			})
			broker.Publish(jobID, b)
		}
	}()

	for _, r := range renditions {
		r := r
		g.Go(func() error {
			return one(ctx, inputPath, outputDir, r, duration, progressCh)
		})
	}

	err := g.Wait()
	close(progressCh)
	<-done
	if err != nil {
		return err
	}

	if err := WriteMasterPlaylist(outputDir, renditions); err != nil {
		return fmt.Errorf("write master.m3u8: %w", err)
	}
	return nil
}

func one(ctx context.Context, inputPath, outputDir string, r model.Rendition, duration float64, progressCh chan<- model.Progress) error {
	renditionDir := filepath.Join(outputDir, r.Name)
	if err := os.MkdirAll(renditionDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", r.Name, err)
	}

	segmentPattern := filepath.Join(renditionDir, "seg%05d.ts")
	playlistPath := filepath.Join(renditionDir, "stream.m3u8")
	scaleFilter := fmt.Sprintf(
		"scale=w=%d:h=%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
		r.Width, r.Height, r.Width, r.Height,
	)

	args := []string{
		"-y",
		"-i", inputPath,
		"-vf", scaleFilter,
		"-c:v", "libx264",
		"-preset", "fast",
		"-profile:v", "high",
		"-level", "4.1",
		"-b:v", r.VideoBitrate,
		"-maxrate", r.MaxRate,
		"-bufsize", r.BufSize,
		"-g", "48",
		"-keyint_min", "48",
		"-sc_threshold", "0",
		"-c:a", "aac",
		"-b:a", r.AudioBitrate,
		"-ac", "2",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if p, ok := ParseProgress(line, r.Name, duration); ok {
				select {
				case progressCh <- p:
				default:
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg %s: %w", r.Name, err)
	}

	progressCh <- model.Progress{Rendition: r.Name, Percent: 100, Done: true}
	return nil
}
