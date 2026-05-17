package transcode

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
)

var (
	reTime  = regexp.MustCompile(`time=(\d+):(\d+):(\d+\.\d+)`)
	reFPS   = regexp.MustCompile(`fps=\s*([\d.]+)`)
	reSpeed = regexp.MustCompile(`speed=\s*([\d.]+)x`)
)

// ParseProgress turns one ffmpeg stderr line into a Progress event.
func ParseProgress(line, renditionName string, duration float64) (model.Progress, bool) {
	if !strings.Contains(line, "time=") {
		return model.Progress{}, false
	}

	p := model.Progress{Rendition: renditionName}

	if m := reTime.FindStringSubmatch(line); m != nil {
		h, _ := strconv.ParseFloat(m[1], 64)
		min, _ := strconv.ParseFloat(m[2], 64)
		sec, _ := strconv.ParseFloat(m[3], 64)
		current := h*3600 + min*60 + sec
		if duration > 0 {
			p.Percent = (current / duration) * 100
			if p.Percent > 99 {
				p.Percent = 99
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
