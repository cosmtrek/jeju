package skills

type Manifest struct {
	APIVersion   string             `yaml:"apiVersion"`
	Kind         string             `yaml:"kind"`
	Metadata     Metadata           `yaml:"metadata"`
	Disclosure   Disclosure         `yaml:"disclosure"`
	Instructions InstructionsConfig `yaml:"instructions"`
	Examples     []string           `yaml:"examples,omitempty"`
	Evals        []string           `yaml:"evals,omitempty"`
	Path         string             `yaml:"-"`
}

type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type Disclosure struct {
	WhenToUse    []string       `yaml:"when_to_use,omitempty"`
	Capabilities []string       `yaml:"capabilities,omitempty"`
	Inputs       map[string]any `yaml:"inputs,omitempty"`
	Outputs      map[string]any `yaml:"outputs,omitempty"`
	Requires     Requires       `yaml:"requires,omitempty"`
	Risk         map[string]any `yaml:"risk,omitempty"`
}

type Requires struct {
	Tools []string `yaml:"tools,omitempty"`
}

type InstructionsConfig struct {
	File string `yaml:"file"`
}

type Skill struct {
	Manifest     Manifest
	Instructions string
	Active       bool
}
