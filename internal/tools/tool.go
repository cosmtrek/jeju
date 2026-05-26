package tools

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Name() string
	Spec() Spec
	Run(ctx context.Context, input json.RawMessage) (Result, error)
}

type Spec struct {
	Name            string
	Description     string
	InputSchema     any
	Args            []string
	Permission      string
	Risks           []string
	TimeoutSec      int
	SideEffect      bool
	SandboxRequired bool
}

type Result struct {
	Output    string         `json:"output"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Artifact struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}
