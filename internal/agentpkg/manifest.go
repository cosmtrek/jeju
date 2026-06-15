package agentpkg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	APIVersion   = "jeju/v1alpha1"
	Kind         = "AgentPackage"
	ManifestFile = "jeju.package.yaml"
)

var (
	packageIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`)
	semverRe    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

type Manifest struct {
	APIVersion    string              `yaml:"apiVersion"`
	Kind          string              `yaml:"kind"`
	Metadata      Metadata            `yaml:"metadata"`
	Compatibility CompatibilityConfig `yaml:"compatibility,omitempty"`
	Agent         AgentConfig         `yaml:"agent"`
}

type Metadata struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name,omitempty"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	License     string            `yaml:"license,omitempty"`
	Homepage    string            `yaml:"homepage,omitempty"`
	Repository  string            `yaml:"repository,omitempty"`
}

type CompatibilityConfig struct {
	Jeju string `yaml:"jeju,omitempty"`
}

type AgentConfig struct {
	Manifest string `yaml:"manifest"`
}

type Package struct {
	Root              string
	ManifestPath      string
	AgentManifestPath string
	Manifest          Manifest
	AgentManifest     config.AgentManifest
	Warnings          []string
}

type ValidateOptions struct {
	JejuVersion string
}

func Load(path string) (*Package, error) {
	manifestPath, root, err := resolveManifestPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	return &Package{
		Root:         root,
		ManifestPath: manifestPath,
		Manifest:     manifest,
	}, nil
}

func Validate(path string, opts ValidateOptions) (*Package, error) {
	pkg, err := Load(path)
	if err != nil {
		return nil, err
	}
	if err := validatePackageManifest(pkg); err != nil {
		return nil, err
	}
	agentPath, err := resolvePackageRelativePath(pkg.Root, pkg.Manifest.Agent.Manifest)
	if err != nil {
		return nil, fmt.Errorf("agent.manifest: %w", err)
	}
	pkg.AgentManifestPath = agentPath
	if err := ensureRegularFile(agentPath); err != nil {
		return nil, fmt.Errorf("agent.manifest %q: %w", pkg.Manifest.Agent.Manifest, err)
	}
	agentManifest, _, err := config.LoadFile(agentPath)
	if err != nil {
		return nil, err
	}
	pkg.AgentManifest = *agentManifest
	if err := validateCompatibility(pkg.Manifest.Compatibility.Jeju, opts.JejuVersion); err != nil {
		return nil, err
	}
	if err := ensureDeclaredPathsInsideRoot(pkg.Root, agentManifest); err != nil {
		return nil, err
	}
	if err := config.Validate(agentManifest); err != nil {
		return nil, err
	}
	if _, err := compiler.Compile(agentPath); err != nil {
		return nil, err
	}
	if err := scanDeveloperHome(pkg.Root); err != nil {
		return nil, err
	}
	pkg.Warnings = validationWarnings(pkg, opts)
	return pkg, nil
}

func resolveManifestPath(path string) (manifestPath string, root string, err error) {
	if path == "" {
		path = "."
	}
	clean := filepath.Clean(path)
	info, statErr := os.Stat(clean)
	if statErr != nil {
		return "", "", statErr
	}
	if info.IsDir() {
		root, err = filepath.Abs(clean)
		if err != nil {
			return "", "", err
		}
		return filepath.Join(root, ManifestFile), root, nil
	}
	if filepath.Base(clean) != ManifestFile {
		return "", "", fmt.Errorf("%s is not a package root or %s file", path, ManifestFile)
	}
	manifestPath, err = filepath.Abs(clean)
	if err != nil {
		return "", "", err
	}
	return manifestPath, filepath.Dir(manifestPath), nil
}

func validatePackageManifest(pkg *Package) error {
	m := pkg.Manifest
	if m.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", m.APIVersion)
	}
	if m.Kind != Kind {
		return fmt.Errorf("unsupported kind %q", m.Kind)
	}
	if !packageIDRe.MatchString(m.Metadata.ID) {
		return fmt.Errorf("metadata.id must match namespace/name using lowercase letters, numbers, '.', '_', or '-'")
	}
	if !semverRe.MatchString(m.Metadata.Version) {
		return fmt.Errorf("metadata.version must be semantic version, got %q", m.Metadata.Version)
	}
	if strings.TrimSpace(m.Metadata.Description) == "" {
		return fmt.Errorf("metadata.description is required")
	}
	if strings.TrimSpace(m.Agent.Manifest) == "" {
		return fmt.Errorf("agent.manifest is required")
	}
	if filepath.IsAbs(m.Agent.Manifest) {
		return fmt.Errorf("agent.manifest must be relative")
	}
	if _, err := resolvePackageRelativePath(pkg.Root, m.Agent.Manifest); err != nil {
		return fmt.Errorf("agent.manifest: %w", err)
	}
	return nil
}

func validationWarnings(pkg *Package, opts ValidateOptions) []string {
	var warnings []string
	if _, err := os.Stat(filepath.Join(pkg.Root, "README.md")); os.IsNotExist(err) {
		warnings = append(warnings, "README.md is recommended")
	}
	if strings.TrimSpace(pkg.Manifest.Compatibility.Jeju) == "" {
		warnings = append(warnings, "compatibility.jeju is recommended")
	} else if opts.JejuVersion == "" || opts.JejuVersion == "dev" {
		warnings = append(warnings, "compatibility.jeju is declared but cannot be checked against a dev build")
	}
	return warnings
}

func resolvePackageRelativePath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative")
	}
	clean := filepath.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("path escapes package root")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(absRoot, clean)
	if err := ensureInsideRoot(absRoot, path); err != nil {
		return "", err
	}
	return path, nil
}

func ensureDeclaredPathsInsideRoot(root string, manifest *config.AgentManifest) error {
	paths := []namedPath{
		{name: "instructions.system", path: manifest.Instructions.System},
		{name: "workspace.path", path: manifest.Workspace.Path},
	}
	for i, dir := range manifest.Skills.Dirs {
		paths = append(paths, namedPath{name: fmt.Sprintf("skills.dirs[%d]", i), path: dir})
	}
	for i, tool := range manifest.Tools {
		if schema, ok := tool.Input.Schema.(string); ok {
			paths = append(paths, namedPath{name: fmt.Sprintf("tools[%d].input.schema", i), path: schema})
		}
		if tool.Command.Run != "" && filepath.IsAbs(tool.Command.Run) {
			paths = append(paths, namedPath{name: fmt.Sprintf("tools[%d].command.run", i), path: tool.Command.Run})
		}
		for j, arg := range tool.Command.Args {
			if filepath.IsAbs(arg) {
				paths = append(paths, namedPath{name: fmt.Sprintf("tools[%d].command.args[%d]", i, j), path: arg})
			}
		}
	}
	for i, evaluator := range manifest.Evaluate.Evaluators {
		if evaluator.Prompt != "" {
			paths = append(paths, namedPath{name: fmt.Sprintf("evaluate.evaluators[%d].prompt", i), path: evaluator.Prompt})
		}
		if evaluator.Command.Run != "" && filepath.IsAbs(evaluator.Command.Run) {
			paths = append(paths, namedPath{name: fmt.Sprintf("evaluate.evaluators[%d].command.run", i), path: evaluator.Command.Run})
		}
		for j, arg := range evaluator.Command.Args {
			if filepath.IsAbs(arg) {
				paths = append(paths, namedPath{name: fmt.Sprintf("evaluate.evaluators[%d].command.args[%d]", i, j), path: arg})
			}
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	for _, item := range paths {
		if strings.TrimSpace(item.path) == "" {
			continue
		}
		if err := ensureInsideRoot(absRoot, item.path); err != nil {
			return fmt.Errorf("%s %q: %w", item.name, item.path, err)
		}
	}
	return nil
}

type namedPath struct {
	name string
	path string
}

func ensureInsideRoot(root, path string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("path escapes package root")
	}
	return nil
}

func ensureRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlinks are not allowed")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	return nil
}

func scanDeveloperHome(root string) error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	files, err := collectPackageFiles(root)
	if err != nil {
		return err
	}
	needle := []byte(home)
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Size() > 2*1024*1024 {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, needle) {
			return fmt.Errorf("package file %s contains developer-home absolute path %q", rel, home)
		}
	}
	return nil
}
