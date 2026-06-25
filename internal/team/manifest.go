package team

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/cosmtrek/jeju/internal/config"

	"gopkg.in/yaml.v3"
)

const (
	KindAgentTeam      = "AgentTeam"
	TopologyLeadWorker = "lead_worker"
)

var validTeamNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

type AgentTeamManifest struct {
	APIVersion   string             `yaml:"apiVersion"`
	Kind         string             `yaml:"kind"`
	Metadata     config.Metadata    `yaml:"metadata"`
	Lead         LeadSpec           `yaml:"lead"`
	Workers      map[string]Worker  `yaml:"workers"`
	Runtime      RuntimeConfig      `yaml:"runtime,omitempty"`
	Verification VerificationConfig `yaml:"verification,omitempty"`
	Output       OutputConfig       `yaml:"output,omitempty"`

	path    string
	baseDir string
}

type LeadSpec struct {
	Agent          string `yaml:"agent"`
	SynthesisAgent string `yaml:"synthesisAgent,omitempty"`
	Description    string `yaml:"description,omitempty"`
}

type Worker struct {
	Agent       string `yaml:"agent"`
	Description string `yaml:"description,omitempty"`
	MaxTasks    int    `yaml:"maxTasks,omitempty"`
}

type RuntimeConfig struct {
	Topology                  string `yaml:"topology,omitempty"`
	MaxRounds                 int    `yaml:"maxRounds,omitempty"`
	MaxTasks                  int    `yaml:"maxTasks,omitempty"`
	MaxParallel               int    `yaml:"maxParallel,omitempty"`
	MaxRetriesPerTask         int    `yaml:"maxRetriesPerTask"`
	MaxConsecutiveEmptyRounds int    `yaml:"maxConsecutiveEmptyRounds"`

	maxRetriesPerTaskSet         bool
	maxConsecutiveEmptyRoundsSet bool
}

type VerificationConfig struct {
	RequireStructuredTaskOutput bool     `yaml:"requireStructuredTaskOutput,omitempty"`
	RequiredTaskFields          []string `yaml:"requiredTaskFields,omitempty"`
	RequireVerifier             bool     `yaml:"requireVerifier,omitempty"`
}

type OutputConfig struct {
	Dir string `yaml:"dir,omitempty"`
}

func (r *RuntimeConfig) UnmarshalYAML(value *yaml.Node) error {
	type plain RuntimeConfig
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*r = RuntimeConfig(decoded)
	if value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		switch value.Content[i].Value {
		case "maxRetriesPerTask":
			r.maxRetriesPerTaskSet = true
		case "maxConsecutiveEmptyRounds":
			r.maxConsecutiveEmptyRoundsSet = true
		}
	}
	return nil
}

func LoadFile(path string) (*AgentTeamManifest, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var manifest AgentTeamManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	manifest.path = abs
	manifest.baseDir = filepath.Dir(abs)
	manifest.applyDefaults()
	manifest.resolveRelativePaths()
	if err := manifest.validate(); err != nil {
		return nil, nil, err
	}
	snapshot, err := yaml.Marshal(&manifest)
	if err != nil {
		return nil, nil, err
	}
	return &manifest, snapshot, nil
}

func (m *AgentTeamManifest) applyDefaults() {
	if m.Runtime.Topology == "" {
		m.Runtime.Topology = TopologyLeadWorker
	}
	if m.Runtime.MaxRounds == 0 {
		m.Runtime.MaxRounds = 3
	}
	if m.Runtime.MaxTasks == 0 {
		m.Runtime.MaxTasks = 10
	}
	if m.Runtime.MaxParallel == 0 {
		m.Runtime.MaxParallel = 3
	}
	if !m.Runtime.maxRetriesPerTaskSet && m.Runtime.MaxRetriesPerTask == 0 {
		m.Runtime.MaxRetriesPerTask = 1
	}
	if !m.Runtime.maxConsecutiveEmptyRoundsSet && m.Runtime.MaxConsecutiveEmptyRounds == 0 {
		m.Runtime.MaxConsecutiveEmptyRounds = 1
	}
	if m.Output.Dir == "" && m.Metadata.Name != "" {
		m.Output.Dir = filepath.Join(".jeju-dev", "team", m.Metadata.Name)
	}
}

func (m *AgentTeamManifest) resolveRelativePaths() {
	m.Lead.Agent = resolveManifestPath(m.baseDir, m.Lead.Agent)
	for name, worker := range m.Workers {
		worker.Agent = resolveManifestPath(m.baseDir, worker.Agent)
		m.Workers[name] = worker
	}
	m.Output.Dir = resolveManifestPath(m.baseDir, m.Output.Dir)
}

func (m *AgentTeamManifest) validate() error {
	if m.APIVersion != "jeju/v1alpha1" {
		return fmt.Errorf("unsupported apiVersion %q", m.APIVersion)
	}
	if m.Kind != KindAgentTeam {
		return fmt.Errorf("unsupported kind %q", m.Kind)
	}
	if !validTeamNameRe.MatchString(m.Metadata.Name) {
		return fmt.Errorf("metadata.name must match %s", validTeamNameRe.String())
	}
	if m.Lead.Agent == "" {
		return fmt.Errorf("lead.agent is required")
	}
	if err := ensureFile(m.Lead.Agent); err != nil {
		return fmt.Errorf("lead.agent %q: %w", m.Lead.Agent, err)
	}
	if m.Lead.SynthesisAgent != "" {
		return fmt.Errorf("lead.synthesisAgent is no longer supported; declare a normal worker and finish with finish.task_id, or let the lead return finish.content")
	}
	if len(m.Workers) == 0 {
		return fmt.Errorf("workers is required")
	}
	for name, worker := range m.Workers {
		if !validTeamNameRe.MatchString(name) {
			return fmt.Errorf("worker name %q must match %s", name, validTeamNameRe.String())
		}
		if worker.Agent == "" {
			return fmt.Errorf("workers.%s.agent is required", name)
		}
		if err := ensureFile(worker.Agent); err != nil {
			return fmt.Errorf("workers.%s.agent %q: %w", name, worker.Agent, err)
		}
		if worker.MaxTasks < 0 {
			return fmt.Errorf("workers.%s.maxTasks must be non-negative", name)
		}
	}
	if m.Verification.RequireVerifier {
		if _, ok := m.Workers[VerifierWorkerName]; !ok {
			return fmt.Errorf("verification.requireVerifier requires a worker named %q", VerifierWorkerName)
		}
	}
	if m.Runtime.Topology != TopologyLeadWorker {
		return fmt.Errorf("runtime.topology %q is not supported", m.Runtime.Topology)
	}
	if m.Runtime.MaxRounds <= 0 {
		return fmt.Errorf("runtime.maxRounds must be positive")
	}
	if m.Runtime.MaxTasks <= 0 {
		return fmt.Errorf("runtime.maxTasks must be positive")
	}
	if m.Runtime.MaxParallel <= 0 {
		return fmt.Errorf("runtime.maxParallel must be positive")
	}
	if m.Runtime.MaxRetriesPerTask < 0 {
		return fmt.Errorf("runtime.maxRetriesPerTask must be non-negative")
	}
	if m.Runtime.MaxConsecutiveEmptyRounds < 0 {
		return fmt.Errorf("runtime.maxConsecutiveEmptyRounds must be non-negative")
	}
	if err := ensureCreatableDir(m.Output.Dir); err != nil {
		return fmt.Errorf("output.dir %q is not creatable: %w", m.Output.Dir, err)
	}
	return nil
}

func resolveManifestPath(baseDir, path string) string {
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

func ensureFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("is a directory")
	}
	return nil
}

func ensureCreatableDir(path string) error {
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("not a directory")
		}
		return nil
	}
	ancestor := filepath.Dir(path)
	for ancestor != "" && ancestor != "." && ancestor != string(filepath.Separator) {
		info, err := os.Stat(ancestor)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("ancestor %q is not a directory", ancestor)
			}
			return nil
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return nil
		}
		ancestor = parent
	}
	return nil
}
