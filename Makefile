.PHONY: run build clean

BINARY := bin/ffmpeg-pipeline

run:
	go run ./cmd/server

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/server

clean:
	rm -rf bin
