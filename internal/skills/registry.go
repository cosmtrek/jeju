package skills

import (
	"fmt"

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
	registry := NewRegistry()
	for _, path := range cfg.Paths {
		skill, err := Load(path)
		if err != nil {
			return nil, err
		}
		for _, required := range skill.Manifest.Disclosure.Requires.Tools {
			if !availableTools[required] {
				return nil, fmt.Errorf("skill %q requires missing tool %q", skill.Manifest.Metadata.Name, required)
			}
		}
		registry.Add(skill)
	}
	active := map[string]bool{}
	for _, name := range cfg.Activation.Active {
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
