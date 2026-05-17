# ffmpeg-pipeline

A learning-oriented video encoding pipeline: upload a source file, transcode to an HLS adaptive bitrate ladder with FFmpeg, score each rendition with VMAF, and play back in the browser.

Built to explore how streaming platforms encode and deliver video (ABR, HLS, perceptual quality metrics).

## Requirements

- Go 1.22+
- [FFmpeg](https://ffmpeg.org/) with `libvmaf` (for quality scoring)
- Modern browser with MSE (or Safari for native HLS)

## Quick start

```bash
make run
# open http://localhost:8000
```

Or build a single binary (UI embedded):

```bash
make build
./bin/ffmpeg-pipeline
```

## Configuration

| Variable       | Default      | Description              |
|----------------|--------------|--------------------------|
| `ADDR`         | `:8000`      | HTTP listen address      |
| `UPLOADS_DIR`  | `./uploads`  | Source uploads           |
| `OUTPUT_DIR`   | `./output`   | Encoded HLS + job metadata |

## Project layout

```
cmd/server/          Entry point, embeds web UI
internal/
  api/               HTTP handlers and routing
  config/            Environment-based configuration
  events/            SSE broker for live progress
  jobs/              Job store (memory + disk persistence)
  model/             Shared types
  pipeline/          Orchestrates transcode → VMAF
  probe/             ffprobe wrapper
  transcode/         HLS ladder encoding
  vmaf/              Per-rendition quality scoring
web/                 Static UI (HTML, CSS, JS) — embedded at build time
```

## API

| Method   | Path                 | Description                    |
|----------|----------------------|--------------------------------|
| `POST`   | `/api/upload`        | Upload video, start encode job |
| `GET`    | `/api/jobs`          | List all jobs                  |
| `GET`    | `/api/jobs/{id}`     | Job metadata + VMAF scores     |
| `DELETE` | `/api/jobs/{id}`     | Delete job and files           |
| `GET`    | `/api/events/{id}`   | SSE progress stream            |
| `GET`    | `/stream/{id}/...`   | HLS segments and playlists     |

## License

MIT
