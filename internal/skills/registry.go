package skills

import (
	"fmt"
	"os"
	"path/filepath"

	"jeju/internal/config"
)

type Registry struct {
	items map[string]*Skill
	order []string
}

func NewRegistry() *Registry {
	return &Registry{items: map[string]*Skill{}}
}

func LoadRegistry(cfg config.SkillsConfig, availableTools map[string]bool) (*Registry, error) {
	_ = availableTools
	registry := NewRegistry()
	for _, dir := range cfg.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skill, err := Load(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			registry.Add(skill)
		}
	}
	active := map[string]bool{}
	for _, name := range cfg.Active {
		active[name] = true
	}
	for name := range active {
		skill, ok := registry.Get(name)
		if !ok {
			return nil, fmt.Errorf("active skill %q is not loaded", name)
		}
		if err := LoadInstructions(skill); err != nil {
			return nil, fmt.Errorf("load skill %q instructions: %w", name, err)
		}
	}
	return registry, nil
}

func (r *Registry) Add(skill *Skill) {
	name := skill.Manifest.Metadata.Name
	if _, exists := r.items[name]; !exists {
		r.order = append(r.order, name)
	}
	r.items[name] = skill
}

func (r *Registry) Get(name string) (*Skill, bool) {
	skill, ok := r.items[name]
	return skill, ok
}

func (r *Registry) All() []*Skill {
	result := make([]*Skill, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.items[name])
	}
	return result
}

func (r *Registry) Active() []*Skill {
	result := []*Skill{}
	for _, skill := range r.All() {
		if skill.Active {
			result = append(result, skill)
		}
	}
	return result
}
