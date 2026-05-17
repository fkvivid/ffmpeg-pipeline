package probe

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
)

// Video runs ffprobe and returns structured metadata.
func Video(filePath string) (*model.VideoInfo, error) {
	args := []string{"-v", "quiet", "-print_format", "json", "-show_streams", "-show_format", filePath}
	out, err := exec.Command("ffprobe", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var raw model.FFprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ffprobe JSON parse: %w", err)
	}

	info := &model.VideoInfo{}
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
