package transcode

import "github.com/fkvivid/ffmpeg-pipeline/internal/model"

// DefaultLadder is the ABR encoding ladder (highest to lowest).
var DefaultLadder = []model.Rendition{
	{Name: "1080p", Width: 1920, Height: 1080, VideoBitrate: "4500k", AudioBitrate: "192k", MaxRate: "4950k", BufSize: "9000k"},
	{Name: "720p", Width: 1280, Height: 720, VideoBitrate: "2500k", AudioBitrate: "128k", MaxRate: "2750k", BufSize: "5000k"},
	{Name: "480p", Width: 854, Height: 480, VideoBitrate: "1000k", AudioBitrate: "128k", MaxRate: "1100k", BufSize: "2000k"},
	{Name: "360p", Width: 640, Height: 360, VideoBitrate: "500k", AudioBitrate: "96k", MaxRate: "550k", BufSize: "1000k"},
}

// PickRenditions selects ladder rungs that fit the source resolution.
func PickRenditions(sourceHeight int) []model.Rendition {
	var picked []model.Rendition
	for _, r := range DefaultLadder {
		if r.Height <= sourceHeight {
			picked = append(picked, r)
		}
	}
	if len(picked) == 0 {
		picked = []model.Rendition{DefaultLadder[len(DefaultLadder)-1]}
	}
	return picked
}
