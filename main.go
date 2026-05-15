package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
)

type VideoInfo struct {
	Filename string  `json:"filename"`
	Size     int64   `json:"size"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration"`
	FPS      float64 `json:"fps"`
	Codec    string  `json:"codec"`
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

type UploadResponse struct {
	JobID      string     `json:"job_id"`
	Info       *VideoInfo `json:"info"`
	Renditions []string   `json:"renditions"`
}

// 🆕 Rendition defines one HLS output quality
type Rendition struct {
	Name         string // "720p"
	Width        int    // 1280
	Height       int    // 720
	VideoBitrate string // "2500k"
	AudioBitrate string // "128k"
	MaxRate      string // "2750k"
	BufSize      string // "5000k"
}

// 🆕 The 4 standard profiles
var allRenditions = []Rendition{
	{
		Name: "1080p", Width: 1920, Height: 1080,
		VideoBitrate: "4500k", AudioBitrate: "192k",
		MaxRate: "4950k", BufSize: "9000k",
	},
	{
		Name: "720p", Width: 1280, Height: 720,
		VideoBitrate: "2500k", AudioBitrate: "128k",
		MaxRate: "2750k", BufSize: "5000k",
	},
	{
		Name: "480p", Width: 854, Height: 480,
		VideoBitrate: "1000k", AudioBitrate: "128k",
		MaxRate: "1100k", BufSize: "2000k",
	},
	{
		Name: "360p", Width: 640, Height: 360,
		VideoBitrate: "500k", AudioBitrate: "96k",
		MaxRate: "550k", BufSize: "1000k",
	},
}

func main() {
	os.MkdirAll("./uploads", 0755)
	os.MkdirAll("./output", 0755)

	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/api/upload", handleUpload)

	fmt.Println("🎬 Server running at http://localhost:8000")
	http.ListenAndServe(":8000", nil)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
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
	savePath := filepath.Join("./uploads", filename)

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

	fmt.Printf("✅ Saved: %s (%d bytes)\n", savePath, size)

	info, err := probeVideo(savePath)
	if err != nil {
		http.Error(w, "could not probe video", http.StatusInternalServerError)
		return
	}
	info.Filename = filename
	info.Size = size

	fmt.Printf("📊 Probed: %dx%d, %.1fs\n", info.Width, info.Height, info.Duration)

	// 🆕 Pick which renditions to encode (skip upscaling)
	renditions := pickRenditions(info.Height)
	renditionNames := make([]string, len(renditions))
	for i, r := range renditions {
		renditionNames[i] = r.Name
	}
	fmt.Printf("🎯 Encoding renditions: %v\n", renditionNames)

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	jobDir := filepath.Join("./output", jobID)

	// 🆕 Run all renditions in parallel using errgroup
	go func() {
		start := time.Now()
		fmt.Printf("🎬 [%s] Starting parallel transcode of %d renditions...\n", jobID, len(renditions))

		if err := transcodeAll(savePath, jobDir, renditions); err != nil {
			fmt.Printf("❌ [%s] Transcode failed: %v\n", jobID, err)
			return
		}

		elapsed := time.Since(start)
		fmt.Printf("✅ [%s] All renditions done in %s\n", jobID, elapsed.Round(time.Second))
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UploadResponse{
		JobID:      jobID,
		Info:       info,
		Renditions: renditionNames,
	})
}

// 🆕 pickRenditions returns only renditions that don't upscale the source.
// If your input is 720p, we won't waste time generating a fake "1080p" version.
func pickRenditions(sourceHeight int) []Rendition {
	var picked []Rendition
	for _, r := range allRenditions {
		if r.Height <= sourceHeight {
			picked = append(picked, r)
		}
	}
	// Always include at least the lowest rendition
	if len(picked) == 0 {
		picked = []Rendition{allRenditions[len(allRenditions)-1]}
	}
	return picked
}

// 🆕 transcodeAll runs every rendition in parallel and waits for them all.
// If any one fails, the context is cancelled and the others stop.
func transcodeAll(inputPath, outputDir string, renditions []Rendition) error {
	// errgroup gives us "wait for all + cancel on first error" semantics
	g, ctx := errgroup.WithContext(context.Background())

	for _, r := range renditions {
		r := r // 👈 IMPORTANT: capture loop variable for the goroutine
		g.Go(func() error {
			return transcodeOne(ctx, inputPath, outputDir, r)
		})
	}

	return g.Wait() // blocks until all goroutines return; returns first error if any
}

// 🆕 transcodeOne encodes a single rendition. Identical to Phase 4 but parameterized.
func transcodeOne(ctx context.Context, inputPath, outputDir string, r Rendition) error {
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

	// 🆕 exec.CommandContext — kills ffmpeg if the context is cancelled
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	// Discard stderr because 4 ffmpegs writing to your terminal is chaos
	// (we'll capture and parse it properly in Phase 6)
	cmd.Stderr = nil

	fmt.Printf("  ▶️  [%s] starting\n", r.Name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s ffmpeg: %w", r.Name, err)
	}
	fmt.Printf("  ✅ [%s] done\n", r.Name)
	return nil
}

func probeVideo(filePath string) (*VideoInfo, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		filePath,
	}

	out, err := exec.Command("ffprobe", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ffprobe JSON parse: %w", err)
	}

	info := &VideoInfo{}

	if raw.Format.Duration != "" {
		info.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	}

	for _, s := range raw.Streams {
		if s.CodecType != "video" {
			continue
		}
		info.Width = s.Width
		info.Height = s.Height
		info.Codec = s.CodecName
		info.FPS = parseFrameRate(s.RFrameRate)
		break
	}

	if info.Width == 0 {
		return nil, fmt.Errorf("no video stream found")
	}

	return info, nil
}

func parseFrameRate(s string) float64 {
	var num, den float64
	if _, err := fmt.Sscanf(s, "%f/%f", &num, &den); err != nil || den == 0 {
		return 0
	}
	return num / den
}
