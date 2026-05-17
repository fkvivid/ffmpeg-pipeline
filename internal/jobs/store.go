package jobs

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fkvivid/ffmpeg-pipeline/internal/model"
)

const metaFile = "job.json"

// Store is the in-memory job registry backed by disk under outputDir.
type Store struct {
	mu          sync.RWMutex
	jobs        map[string]*model.Job
	uploadsDir  string
	outputDir   string
}

// NewStore creates a job store rooted at the given directories.
func NewStore(uploadsDir, outputDir string) *Store {
	return &Store{
		jobs:       make(map[string]*model.Job),
		uploadsDir: uploadsDir,
		outputDir:  outputDir,
	}
}

// Save inserts or updates a job and persists metadata.
func (s *Store) Save(j *model.Job) {
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.mu.Unlock()
	if err := s.persist(j); err != nil {
		fmt.Printf("⚠️  could not persist job %s: %v\n", j.ID, err)
	}
}

// Get returns a job by ID.
func (s *Store) Get(id string) (*model.Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// List returns all jobs sorted newest first.
func (s *Store) List() []*model.Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// UpdateStatus sets job status and persists.
func (s *Store) UpdateStatus(id, status string) {
	var j *model.Job
	s.mu.Lock()
	if jj, ok := s.jobs[id]; ok {
		jj.Status = status
		j = jj
	}
	s.mu.Unlock()
	if j != nil {
		s.persist(j)
	}
}

// SaveVMAF attaches VMAF scores and persists.
func (s *Store) SaveVMAF(id string, scores []model.VMAFScore) {
	var j *model.Job
	s.mu.Lock()
	if jj, ok := s.jobs[id]; ok {
		jj.VMAFScores = scores
		j = jj
	}
	s.mu.Unlock()
	if j != nil {
		s.persist(j)
	}
}

// Delete removes a job from memory and deletes its files.
func (s *Store) Delete(id string) error {
	j, ok := s.Get(id)
	if !ok {
		return fmt.Errorf("job not found")
	}

	if j.OutputDir != "" {
		if err := os.RemoveAll(j.OutputDir); err != nil {
			return fmt.Errorf("remove output: %w", err)
		}
	}
	if j.InputPath != "" {
		_ = os.Remove(j.InputPath)
	}

	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
	return nil
}

// LoadFromDisk rebuilds the registry from outputDir subdirectories.
func (s *Store) LoadFromDisk() {
	entries, err := os.ReadDir(s.outputDir)
	if err != nil {
		fmt.Printf("⚠️  could not read output dir: %v\n", err)
		return
	}

	loaded := 0
	for _, ent := range entries {
		if !ent.IsDir() || !strings.HasPrefix(ent.Name(), "job_") {
			continue
		}
		jobDir := filepath.Join(s.outputDir, ent.Name())
		job, err := s.loadFromDir(jobDir, ent.Name())
		if err != nil {
			fmt.Printf("⚠️  skip %s: %v\n", ent.Name(), err)
			continue
		}
		s.mu.Lock()
		s.jobs[job.ID] = job
		s.mu.Unlock()
		loaded++
	}
	if loaded > 0 {
		fmt.Printf("📂 Restored %d job(s) from disk\n", loaded)
	}
}

func (s *Store) persist(j *model.Job) error {
	if j.OutputDir == "" {
		return nil
	}
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(j.OutputDir, metaFile), data, 0644)
}

func (s *Store) loadFromDir(jobDir, jobID string) (*model.Job, error) {
	metaPath := filepath.Join(jobDir, metaFile)
	if data, err := os.ReadFile(metaPath); err == nil {
		var j model.Job
		if err := json.Unmarshal(data, &j); err != nil {
			return nil, fmt.Errorf("parse %s: %w", metaFile, err)
		}
		if j.ID == "" {
			j.ID = jobID
		}
		if j.OutputDir == "" {
			j.OutputDir = jobDir
		}
		return &j, nil
	}

	j, err := s.reconstruct(jobDir, jobID)
	if err != nil {
		return nil, err
	}
	_ = s.persist(j)
	return j, nil
}

func (s *Store) reconstruct(jobDir, jobID string) (*model.Job, error) {
	info, err := os.Stat(jobDir)
	if err != nil {
		return nil, err
	}

	renditions, err := scanRenditions(jobDir)
	if err != nil {
		return nil, err
	}

	status := "processing"
	if _, err := os.Stat(filepath.Join(jobDir, "master.m3u8")); err == nil {
		status = "done"
	} else if len(renditions) > 0 {
		status = "failed"
	}

	inputPath, filename := s.matchUpload(jobID)
	if filename == "" {
		filename = jobID
	}

	return &model.Job{
		ID:         jobID,
		InputPath:  inputPath,
		OutputDir:  jobDir,
		Filename:   filename,
		Status:     status,
		Renditions: renditions,
		VMAFScores: loadVMAFFromDisk(jobDir, renditions),
		CreatedAt:  info.ModTime(),
	}, nil
}

func scanRenditions(jobDir string) ([]string, error) {
	entries, err := os.ReadDir(jobDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(jobDir, ent.Name(), "stream.m3u8")); err == nil {
			names = append(names, ent.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return parseHeight(names[i]) > parseHeight(names[j])
	})
	return names, nil
}

func parseHeight(name string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(name, "p"))
	return n
}

func loadVMAFFromDisk(jobDir string, renditions []string) []model.VMAFScore {
	var scores []model.VMAFScore
	for _, name := range renditions {
		data, err := os.ReadFile(filepath.Join(jobDir, name, "vmaf.json"))
		if err != nil {
			continue
		}
		var report model.VMAFReport
		if err := json.Unmarshal(data, &report); err != nil {
			continue
		}
		scores = append(scores, model.VMAFScore{
			Rendition: name,
			Mean:      report.PooledMetrics.VMAF.Mean,
			Min:       report.PooledMetrics.VMAF.Min,
			Max:       report.PooledMetrics.VMAF.Max,
		})
	}
	return scores
}

func (s *Store) matchUpload(jobID string) (inputPath, filename string) {
	tsStr := strings.TrimPrefix(jobID, "job_")
	jobTS, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", ""
	}

	entries, err := os.ReadDir(s.uploadsDir)
	if err != nil {
		return "", ""
	}

	var bestPath, bestName string
	var bestDiff int64 = math.MaxInt64

	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		parts := strings.SplitN(ent.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		uploadTS, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		diff := jobTS - uploadTS
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			bestPath = filepath.Join(s.uploadsDir, ent.Name())
			bestName = parts[1]
		}
	}

	if bestDiff > int64(10*time.Second) {
		return "", ""
	}
	return bestPath, bestName
}

// OutputDir returns the configured output root (for HTTP static serving).
func (s *Store) OutputDirPath() string {
	return s.outputDir
}
