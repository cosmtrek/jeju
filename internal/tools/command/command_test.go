package command

import (
	"encoding/json"
	"testing"
)

func TestExpandArgsUsesSchemaDefaults(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"text": map[string]any{
				"type": "string",
			},
			"case_sensitive": map[string]any{
				"type":    "boolean",
				"default": false,
			},
		},
	}
	args, err := expandArgs(
		[]string{"--text", "{{.text}}", "--case-sensitive", "{{.case_sensitive}}"},
		json.RawMessage(`{"text":"Jeju"}`),
		schemaDefaults(schema),
	)
	if err != nil {
		t.Fatalf("expandArgs failed: %v", err)
	}
	expected := []string{"--text", "Jeju", "--case-sensitive", "false"}
	if len(args) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}
	for i := range args {
		if args[i] != expected[i] {
			t.Fatalf("expected %v, got %v", expected, args)
		}
	}
}

func TestExpandArgsRequiresMissingInput(t *testing.T) {
	_, err := expandArgs([]string{"--keyword", "{{.keyword}}"}, json.RawMessage(`{}`), nil)
	if err == nil {
		t.Fatal("expected missing template input to fail")
	}
}

func TestParseCommandOutputSupportsPlainStdout(t *testing.T) {
	result, err := parseCommandOutput([]byte("plain output\n"))
	if err != nil {
		t.Fatalf("parseCommandOutput failed: %v", err)
	}
	if result.Output != "plain output" {
		t.Fatalf("unexpected output %q", result.Output)
	}
}

func TestParseCommandOutputSupportsJejuEnvelope(t *testing.T) {
	result, err := parseCommandOutput([]byte(`{"ok":true,"output":{"count":2},"metadata":{"source":"test"}}`))
	if err != nil {
		t.Fatalf("parseCommandOutput failed: %v", err)
	}
	if result.Output != `{"count":2}` {
		t.Fatalf("unexpected output %q", result.Output)
	}
	if result.Metadata["source"] != "test" {
		t.Fatalf("unexpected metadata %v", result.Metadata)
	}
}
