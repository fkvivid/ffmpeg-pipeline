package model

import "time"

// VideoInfo describes a source file after ffprobe.
type VideoInfo struct {
	Filename string  `json:"filename"`
	Size     int64   `json:"size"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration"`
	FPS      float64 `json:"fps"`
	Codec    string  `json:"codec"`
}

// UploadResponse is returned after a successful upload.
type UploadResponse struct {
	JobID      string     `json:"job_id"`
	Info       *VideoInfo `json:"info"`
	Renditions []string   `json:"renditions"`
}

// Rendition is one rung on the encoding ladder.
type Rendition struct {
	Name         string
	Width        int
	Height       int
	VideoBitrate string
	AudioBitrate string
	MaxRate      string
	BufSize      string
}

// Progress is one ffmpeg stderr progress update.
type Progress struct {
	JobID     string  `json:"job_id"`
	Rendition string  `json:"rendition"`
	Percent   float64 `json:"percent"`
	FPS       float64 `json:"fps"`
	Speed     float64 `json:"speed"`
	Done      bool    `json:"done"`
}

// Job tracks one encode pipeline run.
type Job struct {
	ID         string      `json:"id"`
	InputPath  string      `json:"input_path"`
	OutputDir  string      `json:"output_dir"`
	Filename   string      `json:"filename"`
	Status     string      `json:"status"`
	Renditions []string    `json:"renditions"`
	VMAFScores []VMAFScore `json:"vmaf_scores,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}

// VMAFScore holds perceptual quality for one rendition.
type VMAFScore struct {
	Rendition string  `json:"rendition"`
	Mean      float64 `json:"mean"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
}

// VMAFReport matches libvmaf JSON log output.
type VMAFReport struct {
	PooledMetrics struct {
		VMAF struct {
			Mean float64 `json:"mean"`
			Min  float64 `json:"min"`
			Max  float64 `json:"max"`
		} `json:"vmaf"`
	} `json:"pooled_metrics"`
}

// FFprobeOutput is the subset of ffprobe JSON we parse.
type FFprobeOutput struct {
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
