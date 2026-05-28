package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func Load(path string) (*Skill, error) {
	data, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	manifest, body, err := parseSkillMarkdown(data)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", path, err)
	}
	manifest.Path = path
	if err := validateManifest(manifest, filepath.Base(path)); err != nil {
		return nil, fmt.Errorf("skill %q: %w", path, err)
	}
	return &Skill{Manifest: manifest, Instructions: body}, nil
}

func LoadInstructions(skill *Skill) error {
	skill.Active = true
	return nil
}

func parseSkillMarkdown(data []byte) (Manifest, string, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return Manifest{}, text, nil
	}
	rest := text[len("---\n"):]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return Manifest{}, "", fmt.Errorf("frontmatter is not closed")
	}
	rawFrontmatter := rest[:idx]
	body := strings.TrimLeft(rest[idx+len("\n---"):], "\r\n")

	var fm struct {
		Name          string            `yaml:"name"`
		Description   string            `yaml:"description"`
		License       string            `yaml:"license,omitempty"`
		Compatibility string            `yaml:"compatibility,omitempty"`
		Metadata      map[string]string `yaml:"metadata,omitempty"`
		AllowedTools  string            `yaml:"allowed-tools,omitempty"`
	}
	decoder := yaml.NewDecoder(bytes.NewBufferString(rawFrontmatter))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fm); err != nil {
		return Manifest{}, "", err
	}
	return Manifest{
		Metadata: Metadata{
			Name:          fm.Name,
			Description:   fm.Description,
			License:       fm.License,
			Compatibility: fm.Compatibility,
			Metadata:      fm.Metadata,
			AllowedTools:  fm.AllowedTools,
		},
		Disclosure: Disclosure{},
	}, body, nil
}

func validateManifest(manifest Manifest, dirName string) error {
	name := manifest.Metadata.Name
	if name == "" {
		return fmt.Errorf("frontmatter name is required")
	}
	if len(name) > 64 || !skillNameRe.MatchString(name) {
		return fmt.Errorf("frontmatter name %q must be 1-64 lowercase letters, numbers, and hyphens", name)
	}
	if name != dirName {
		return fmt.Errorf("frontmatter name %q must match parent directory %q", name, dirName)
	}
	description := strings.TrimSpace(manifest.Metadata.Description)
	if description == "" || len(description) > 1024 {
		return fmt.Errorf("frontmatter description must be 1-1024 characters")
	}
	if manifest.Metadata.Compatibility != "" && len(manifest.Metadata.Compatibility) > 500 {
		return fmt.Errorf("frontmatter compatibility must be at most 500 characters")
	}
	return nil
}
