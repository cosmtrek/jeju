package runs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Store struct {
	BasePath string
}

func NewStore(basePath string) *Store {
	if basePath == "" {
		basePath = "./runs"
	}
	return &Store{BasePath: basePath}
}

func (s *Store) CreateRun(agentName string, input string) (*RunDir, error) {
	if err := os.MkdirAll(s.BasePath, 0o755); err != nil {
		return nil, err
	}
	runID := time.Now().Format("20060102-150405") + "-" + sanitize(agentName)
	path := filepath.Join(s.BasePath, runID)
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		runID = fmt.Sprintf("%s-%02d", time.Now().Format("20060102-150405")+"-"+sanitize(agentName), i)
		path = filepath.Join(s.BasePath, runID)
	}
	artifacts := filepath.Join(path, ArtifactsDir)
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return nil, err
	}
	return &RunDir{RunID: runID, Path: path, ArtifactsDir: artifacts}, nil
}

func (s *Store) WriteMetadata(runID string, metadata Metadata) error {
	return writeJSON(filepath.Join(s.BasePath, runID, MetadataFile), metadata)
}

func (s *Store) ReadMetadata(runID string) (Metadata, error) {
	var metadata Metadata
	data, err := os.ReadFile(filepath.Join(s.BasePath, runID, MetadataFile))
	if err != nil {
		return metadata, err
	}
	err = json.Unmarshal(data, &metadata)
	return metadata, err
}

func (s *Store) WriteConfigSnapshot(runID string, data []byte) error {
	return os.WriteFile(filepath.Join(s.BasePath, runID, ConfigSnapshotFile), data, 0o644)
}

func (s *Store) WriteArtifact(runID string, name string, data []byte) (string, error) {
	cleaned := filepath.Clean(name)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid artifact name %q", name)
	}
	path := filepath.Join(s.BasePath, runID, ArtifactsDir, cleaned)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(ArtifactsDir, cleaned)), nil
}

func (s *Store) WriteFinal(runID string, content string) error {
	return os.WriteFile(filepath.Join(s.BasePath, runID, FinalFile), []byte(content), 0o644)
}

func (s *Store) WriteEvaluation(runID string, data []byte) error {
	return os.WriteFile(filepath.Join(s.BasePath, runID, EvaluationFile), data, 0o644)
}

func (s *Store) ListRuns() ([]Metadata, error) {
	entries, err := os.ReadDir(s.BasePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := []Metadata{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := s.ReadMetadata(entry.Name())
		if err != nil {
			continue
		}
		items = append(items, meta)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	return items, nil
}

func (s *Store) LoadRun(runID string) (*RunDir, error) {
	path := filepath.Join(s.BasePath, runID)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("run %q is not a directory", runID)
	}
	return &RunDir{RunID: runID, Path: path, ArtifactsDir: filepath.Join(path, ArtifactsDir)}, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

var nonRunChar = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitize(name string) string {
	name = strings.Trim(nonRunChar.ReplaceAllString(name, "-"), "-")
	if name == "" {
		return "agent"
	}
	return strings.ToLower(name)
}
