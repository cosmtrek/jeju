package skills

import (
	"encoding/json"
	"strings"
)

func DisclosureText(registry *Registry) string {
	if registry == nil || len(registry.All()) == 0 {
		return "No skills disclosed."
	}
	var b strings.Builder
	for _, skill := range registry.All() {
		m := skill.Manifest
		payload := map[string]any{
			"name":         m.Metadata.Name,
			"description":  m.Metadata.Description,
			"when_to_use":  m.Disclosure.WhenToUse,
			"capabilities": m.Disclosure.Capabilities,
			"inputs":       m.Disclosure.Inputs,
			"outputs":      m.Disclosure.Outputs,
			"requires":     m.Disclosure.Requires,
			"risk":         m.Disclosure.Risk,
		}
		data, _ := json.Marshal(payload)
		b.WriteString("- ")
		b.Write(data)
		b.WriteString("\n")
	}
	return b.String()
}

func ActiveInstructionsText(registry *Registry) string {
	if registry == nil {
		return ""
	}
	var b strings.Builder
	for _, skill := range registry.Active() {
		b.WriteString("## Skill: ")
		b.WriteString(skill.Manifest.Metadata.Name)
		b.WriteString("\n")
		b.WriteString(skill.Instructions)
		if !strings.HasSuffix(skill.Instructions, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}
