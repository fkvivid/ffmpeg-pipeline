package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// =========================================
// Types
// =========================================

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

type Rendition struct {
	Name         string
	Width        int
	Height       int
	VideoBitrate string
	AudioBitrate string
	MaxRate      string
	BufSize      string
}

// 🆕 Progress is one progress update from one ffmpeg process
type Progress struct {
	JobID     string  `json:"job_id"`
	Rendition string  `json:"rendition"`
	Percent   float64 `json:"percent"`
	FPS       float64 `json:"fps"`
	Speed     float64 `json:"speed"`
	Done      bool    `json:"done"`
}

var allRenditions = []Rendition{
	{Name: "1080p", Width: 1920, Height: 1080, VideoBitrate: "4500k", AudioBitrate: "192k", MaxRate: "4950k", BufSize: "9000k"},
	{Name: "720p", Width: 1280, Height: 720, VideoBitrate: "2500k", AudioBitrate: "128k", MaxRate: "2750k", BufSize: "5000k"},
	{Name: "480p", Width: 854, Height: 480, VideoBitrate: "1000k", AudioBitrate: "128k", MaxRate: "1100k", BufSize: "2000k"},
	{Name: "360p", Width: 640, Height: 360, VideoBitrate: "500k", AudioBitrate: "96k", MaxRate: "550k", BufSize: "1000k"},
}

// =========================================
// 🆕 SSE Broker — fan-out events to browsers
// =========================================

type Broker struct {
	mu          sync.RWMutex
	subscribers map[string][]chan []byte // jobID -> list of subscriber channels
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[string][]chan []byte)}
}

// Subscribe returns a channel that will receive all events for this jobID
func (b *Broker) Subscribe(jobID string) chan []byte {
	ch := make(chan []byte, 32) // buffered so slow consumers don't block
	b.mu.Lock()
	b.subscribers[jobID] = append(b.subscribers[jobID], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel and closes it
func (b *Broker) Unsubscribe(jobID string, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[jobID]
	for i, s := range subs {
		if s == ch {
			b.subscribers[jobID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends data to every subscriber of jobID
func (b *Broker) Publish(jobID string, data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers[jobID] {
		select {
		case ch <- data: // send if there's room
		default: // drop if subscriber is slow — keeps publisher fast
		}
	}
}

// =========================================
// Global broker instance
// =========================================

var broker = NewBroker()

// =========================================
// HTTP handlers
// =========================================

func main() {
	os.MkdirAll("./uploads", 0755)
	os.MkdirAll("./output", 0755)

	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/api/upload", handleUpload)
	http.HandleFunc("/api/events/", handleSSE) // trailing slash on purpose

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

	fmt.Printf("✅ Saved: %s\n", savePath)

	info, err := probeVideo(savePath)
	if err != nil {
		http.Error(w, "could not probe video", http.StatusInternalServerError)
		return
	}
	info.Filename = filename
	info.Size = size

	renditions := pickRenditions(info.Height)
	renditionNames := make([]string, len(renditions))
	for i, r := range renditions {
		renditionNames[i] = r.Name
	}
	fmt.Printf("📊 Probed: %dx%d, %.1fs, encoding: %v\n", info.Width, info.Height, info.Duration, renditionNames)

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	jobDir := filepath.Join("./output", jobID)

	// 🆕 Pass duration so we can compute percent from time= position
	go func() {
		start := time.Now()
		fmt.Printf("🎬 [%s] Starting parallel transcode of %d renditions...\n", jobID, len(renditions))

		if err := transcodeAll(jobID, savePath, jobDir, renditions, info.Duration); err != nil {
			fmt.Printf("❌ [%s] Failed: %v\n", jobID, err)
			publishEvent(jobID, map[string]any{"event": "error", "error": err.Error()})
			return
		}

		elapsed := time.Since(start)
		fmt.Printf("✅ [%s] All done in %s\n", jobID, elapsed.Round(time.Second))
		publishEvent(jobID, map[string]any{"event": "done"})
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UploadResponse{JobID: jobID, Info: info, Renditions: renditionNames})
}

// 🆕 handleSSE streams live progress events to the browser
func handleSSE(w http.ResponseWriter, r *http.Request) {
	// URL is /api/events/{jobID} — extract jobID
	jobID := strings.TrimPrefix(r.URL.Path, "/api/events/")
	if jobID == "" {
		http.Error(w, "missing job ID", http.StatusBadRequest)
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disables proxy buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to events for this job
	ch := broker.Subscribe(jobID)
	defer broker.Unsubscribe(jobID, ch)

	// Send initial ping so the browser knows the connection is alive
	fmt.Fprintf(w, "data: {\"event\":\"connected\"}\n\n")
	flusher.Flush()

	// Pump events from the broker to the browser
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush() // critical — pushes the bytes immediately
		case <-r.Context().Done():
			return // browser disconnected
		}
	}
}

// publishEvent is a helper that JSON-encodes and sends an event
func publishEvent(jobID string, data map[string]any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	broker.Publish(jobID, b)
}

// =========================================
// Transcoding
// =========================================

func pickRenditions(sourceHeight int) []Rendition {
	var picked []Rendition
	for _, r := range allRenditions {
		if r.Height <= sourceHeight {
			picked = append(picked, r)
		}
	}
	if len(picked) == 0 {
		picked = []Rendition{allRenditions[len(allRenditions)-1]}
	}
	return picked
}

func transcodeAll(jobID, inputPath, outputDir string, renditions []Rendition, duration float64) error {
	g, ctx := errgroup.WithContext(context.Background())

	// 🆕 Channel for progress events from all 4 ffmpegs
	progressCh := make(chan Progress, 50)

	// 🆕 Forwarder goroutine — reads from progressCh, publishes to SSE broker
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
			return transcodeOne(ctx, inputPath, outputDir, r, duration, progressCh)
		})
	}

	err := g.Wait()
	close(progressCh) // tell forwarder to stop
	<-done            // wait for forwarder to drain
	return err
}

func transcodeOne(ctx context.Context, inputPath, outputDir string, r Rendition, duration float64, progressCh chan<- Progress) error {
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

	// 🆕 Capture stderr so we can parse progress
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	// 🆕 Read stderr line by line, parse progress, send to channel
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if p, ok := parseProgress(line, r.Name, duration); ok {
				select {
				case progressCh <- p:
				default:
					// channel full — drop this update, the next one will catch up
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg %s: %w", r.Name, err)
	}

	// Send final "done" event
	progressCh <- Progress{Rendition: r.Name, Percent: 100, Done: true}
	return nil
}

// =========================================
// 🆕 ffmpeg stderr parsing
// =========================================

var (
	reTime  = regexp.MustCompile(`time=(\d+):(\d+):(\d+\.\d+)`)
	reFPS   = regexp.MustCompile(`fps=\s*([\d.]+)`)
	reSpeed = regexp.MustCompile(`speed=\s*([\d.]+)x`)
)

// parseProgress turns one ffmpeg stderr line into a Progress event
func parseProgress(line, renditionName string, duration float64) (Progress, bool) {
	if !strings.Contains(line, "time=") {
		return Progress{}, false
	}

	p := Progress{Rendition: renditionName}

	// Extract time position
	if m := reTime.FindStringSubmatch(line); m != nil {
		h, _ := strconv.ParseFloat(m[1], 64)
		min, _ := strconv.ParseFloat(m[2], 64)
		sec, _ := strconv.ParseFloat(m[3], 64)
		current := h*3600 + min*60 + sec
		if duration > 0 {
			p.Percent = (current / duration) * 100
			if p.Percent > 99 {
				p.Percent = 99 // reserve 100 for the done signal
			}
		}
	}

	if m := reFPS.FindStringSubmatch(line); m != nil {
		p.FPS, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reSpeed.FindStringSubmatch(line); m != nil {
		p.Speed, _ = strconv.ParseFloat(m[1], 64)
	}

	return p, true
}

// =========================================
// Probe (unchanged)
// =========================================

func probeVideo(filePath string) (*VideoInfo, error) {
	args := []string{"-v", "quiet", "-print_format", "json", "-show_streams", "-show_format", filePath}
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
