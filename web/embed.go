package web

import "embed"

// FS contains the UI (HTML, CSS, JS) embedded into the server binary.
//
//go:embed index.html player.html static
var FS embed.FS
