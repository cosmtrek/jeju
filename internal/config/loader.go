package config

import (
	"os"
	"path/filepath"

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
	resolveRelativePaths(&manifest, filepath.Dir(path))
	snapshot, err := yaml.Marshal(&manifest)
	if err != nil {
		return nil, nil, err
	}
	return &manifest, snapshot, nil
}

func resolveRelativePaths(m *AgentManifest, baseDir string) {
	if baseDir == "" {
		baseDir = "."
	}
	m.Instructions.System = resolvePath(baseDir, m.Instructions.System)
	m.Workspace.Path = resolvePath(baseDir, m.Workspace.Path)
	for i, dir := range m.Skills.Dirs {
		m.Skills.Dirs[i] = resolvePath(baseDir, dir)
	}
	for i := range m.Tools {
		if schemaPath, ok := m.Tools[i].Input.Schema.(string); ok {
			m.Tools[i].Input.Schema = resolvePath(baseDir, schemaPath)
		}
		if m.Tools[i].Command.Run != "" && looksLikePath(m.Tools[i].Command.Run) {
			m.Tools[i].Command.Run = resolvePath(baseDir, m.Tools[i].Command.Run)
		}
		m.Tools[i].Agent.Manifest = resolvePath(baseDir, m.Tools[i].Agent.Manifest)
		resolveCommandArgs(baseDir, m.Tools[i].Command.Args)
	}
	for i := range m.Evaluate.Evaluators {
		m.Evaluate.Evaluators[i].Prompt = resolvePath(baseDir, m.Evaluate.Evaluators[i].Prompt)
		if m.Evaluate.Evaluators[i].Command.Run != "" && looksLikePath(m.Evaluate.Evaluators[i].Command.Run) {
			m.Evaluate.Evaluators[i].Command.Run = resolvePath(baseDir, m.Evaluate.Evaluators[i].Command.Run)
		}
		resolveCommandArgs(baseDir, m.Evaluate.Evaluators[i].Command.Args)
	}
}

func resolveCommandArgs(baseDir string, args []string) {
	for i, arg := range args {
		if looksLikePath(arg) {
			args[i] = resolvePath(baseDir, arg)
		}
	}
}

func resolvePath(baseDir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	resolved := filepath.Clean(filepath.Join(baseDir, path))
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	return abs
}

func looksLikePath(value string) bool {
	return filepath.IsAbs(value) || filepath.Dir(value) != "."
}
