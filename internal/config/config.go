package config

import "os"

// Config holds runtime paths and server settings.
type Config struct {
	Addr       string
	UploadsDir string
	OutputDir  string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Addr:       env("ADDR", ":8000"),
		UploadsDir: env("UPLOADS_DIR", "./uploads"),
		OutputDir:  env("OUTPUT_DIR", "./output"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
