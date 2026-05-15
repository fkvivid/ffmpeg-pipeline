package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	os.MkdirAll("./uploads", 0755)
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/api/upload", handleUpload)
	fmt.Println("Server running at http://localhost:8000")
	http.ListenAndServe(":8000", nil)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only!!!", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "file too large or bad form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "missing video field", http.StatusBadRequest)
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

	fmt.Printf("Saved: %s (%d bytes)\n", savePath, size)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"filename": "%s", "size": "%d}`, filename, size)

}
