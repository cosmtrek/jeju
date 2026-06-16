package runs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cosmtrek/jeju/internal/trajectory"
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
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return &RunDir{RunID: runID, Path: path}, nil
}

func (s *Store) ReadMetadata(runID string) (Metadata, error) {
	var metadata Metadata
	record, err := s.ReadRunRecord(runID)
	if err != nil {
		return metadata, err
	}
	metadata.RunID = record.RunID
	metadata.Agent = record.Agent
	metadata.PackageID = record.Package.ID
	metadata.PackageVersion = record.Package.Version
	metadata.PackageDigest = record.Package.Digest
	metadata.PackageSource = record.Package.Source
	metadata.PackageStorePath = record.Package.StorePath
	metadata.PackageAgentManifest = record.Package.AgentManifest
	metadata.Status = record.Status
	metadata.Integrity = record.Integrity
	metadata.StartedAt = record.StartedAt
	metadata.EndedAt = record.EndedAt
	metadata.Input = record.Input
	return metadata, nil
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
	return &RunDir{RunID: runID, Path: path}, nil
}

func (s *Store) ReadRunRecord(runID string) (trajectory.RunRecord, error) {
	events, err := trajectory.ReadFile(filepath.Join(s.BasePath, runID, TrajectoryFile))
	if err != nil {
		return trajectory.RunRecord{}, err
	}
	return trajectory.Project(events), nil
}

var nonRunChar = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitize(name string) string {
	name = strings.Trim(nonRunChar.ReplaceAllString(name, "-"), "-")
	if name == "" {
		return "agent"
	}
	return strings.ToLower(name)
}
