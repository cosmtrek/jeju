package compiler

import (
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/config"
)

func TestCompileToolSpecDefaultsBuiltinDescriptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		uses string
	}{
		{name: "read", uses: "builtin:read"},
		{name: "write", uses: "builtin:write"},
		{name: "edit", uses: "builtin:edit"},
		{name: "search", uses: "builtin:search"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := compileToolSpec(config.ToolConfig{Name: tc.name, Uses: tc.uses})
			if err != nil {
				t.Fatalf("compileToolSpec failed: %v", err)
			}
			if strings.TrimSpace(spec.Description) == "" {
				t.Fatalf("%s default description is empty", tc.uses)
			}
		})
	}
}

func TestCompileToolSpecKeepsExplicitDescription(t *testing.T) {
	spec, err := compileToolSpec(config.ToolConfig{
		Name:        "write",
		Uses:        "builtin:write",
		Description: "Project-specific write guidance.",
	})
	if err != nil {
		t.Fatalf("compileToolSpec failed: %v", err)
	}
	if spec.Description != "Project-specific write guidance." {
		t.Fatalf("description = %q, want explicit manifest description", spec.Description)
	}
}
