package agentpkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cosmtrek/jeju/internal/config"
	"gopkg.in/yaml.v3"
)

type InitOptions struct {
	Agent       string
	ID          string
	Version     string
	Name        string
	Description string
}

func Init(root string, opts InitOptions) (*Package, error) {
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return nil, err
	}
	if opts.Agent == "" {
		opts.Agent = "agent.yaml"
	}
	agentRel, err := normalizeAgentManifestPath(absRoot, opts.Agent)
	if err != nil {
		return nil, err
	}
	if opts.ID == "" {
		return nil, fmt.Errorf("--id is required")
	}
	if opts.Version == "" {
		return nil, fmt.Errorf("--version is required")
	}
	agentPath := filepath.Join(absRoot, filepath.FromSlash(agentRel))
	agentManifest, _, err := config.LoadFile(agentPath)
	if err != nil {
		return nil, err
	}
	if opts.Name == "" {
		opts.Name = agentManifest.Metadata.Name
	}
	if opts.Description == "" {
		opts.Description = agentManifest.Metadata.Description
	}
	manifest := Manifest{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata: Metadata{
			ID:          opts.ID,
			Name:        opts.Name,
			Version:     opts.Version,
			Description: opts.Description,
		},
		Agent: AgentConfig{Manifest: agentRel},
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(absRoot, ManifestFile)
	if _, err := os.Stat(manifestPath); err == nil {
		return nil, fmt.Errorf("%s already exists", manifestPath)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return nil, err
	}
	return Validate(absRoot, ValidateOptions{})
}

func normalizeAgentManifestPath(root, agent string) (string, error) {
	if filepath.IsAbs(agent) {
		if err := ensureInsideRoot(root, agent); err != nil {
			return "", fmt.Errorf("--agent must be inside package root: %w", err)
		}
		rel, err := filepath.Rel(root, agent)
		if err != nil {
			return "", err
		}
		return filepath.ToSlash(rel), nil
	}
	resolved, err := resolvePackageRelativePath(root, agent)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
