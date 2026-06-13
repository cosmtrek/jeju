package jsonschemautil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func Normalize(raw any) (any, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func Compile(name string, raw any) (*jsonschema.Schema, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	location := schemaLocation(name)
	if err := compiler.AddResource(location, doc); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

func ValidateJSON(schema *jsonschema.Schema, text string) error {
	if schema == nil {
		return nil
	}
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(strings.TrimSpace(text)))
	if err != nil {
		return fmt.Errorf("output is not valid JSON: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("output does not match schema: %w", err)
	}
	return nil
}

func schemaLocation(name string) string {
	if name == "" {
		name = "response"
	}
	return "jeju-output-" + sanitize(name) + ".schema.json"
}

func sanitize(text string) string {
	var b strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "response"
	}
	return b.String()
}
