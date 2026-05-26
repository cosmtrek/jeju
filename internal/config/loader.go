package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*AgentManifest, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var manifest AgentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, nil, err
	}
	ApplyDefaults(&manifest)
	ResolveEnv(&manifest)
	snapshot, err := yaml.Marshal(&manifest)
	if err != nil {
		return nil, nil, err
	}
	return &manifest, snapshot, nil
}
