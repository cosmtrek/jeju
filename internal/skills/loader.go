package skills

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Skill, error) {
	data, err := os.ReadFile(filepath.Join(path, "skill.yaml"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	manifest.Path = path
	if manifest.APIVersion != "jeju/v1alpha1" {
		return nil, fmt.Errorf("skill %q unsupported apiVersion %q", path, manifest.APIVersion)
	}
	if manifest.Kind != "Skill" {
		return nil, fmt.Errorf("skill %q unsupported kind %q", path, manifest.Kind)
	}
	if manifest.Metadata.Name == "" {
		return nil, fmt.Errorf("skill %q metadata.name is required", path)
	}
	return &Skill{Manifest: manifest}, nil
}

func LoadInstructions(skill *Skill) error {
	file := skill.Manifest.Instructions.File
	if file == "" {
		return nil
	}
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(skill.Manifest.Path, file)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	skill.Instructions = string(data)
	skill.Active = true
	return nil
}
