package transcode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
)

// WriteMasterPlaylist creates master.m3u8 referencing all renditions.
func WriteMasterPlaylist(outputDir string, renditions []model.Rendition) error {
	var sb strings.Builder

	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n\n")

	for _, r := range renditions {
		bandwidth := parseBitrateKbps(r.VideoBitrate) + parseBitrateKbps(r.AudioBitrate)
		sb.WriteString(fmt.Sprintf(
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=\"avc1.640028,mp4a.40.2\"\n",
			bandwidth, r.Width, r.Height,
		))
		sb.WriteString(fmt.Sprintf("%s/stream.m3u8\n\n", r.Name))
	}

	path := filepath.Join(outputDir, "master.m3u8")
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func parseBitrateKbps(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, "k")
	var v int
	fmt.Sscanf(s, "%d", &v)
	return v * 1000
}
