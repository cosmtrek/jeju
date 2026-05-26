package compiler

import (
	"encoding/json"
	"strings"

	"jeju/internal/skills"
)

func (a *CompiledAgent) SystemPrompt() string {
	var b strings.Builder
	b.WriteString(`You are running inside Jeju, a config-defined agent runtime.

You can either:
1. call a tool
2. ask the user for more information
3. provide a final answer

Return ONLY valid JSON.

Tool call format:
{"type":"tool_call","thought":"...","tool":"tool_name","input":{}}

Ask user format:
{"type":"ask_user","thought":"...","question":"..."}

Final format:
{"type":"final","thought":"...","content":"..."}
`)
	b.WriteString("\n# Agent Instructions\n")
	b.WriteString(a.Instructions)
	if !strings.HasSuffix(a.Instructions, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# Workspace\n")
	b.WriteString(a.Sandbox.Workdir())
	b.WriteString("\n")
	b.WriteString("\n# Available Tools\n")
	for _, spec := range a.Tools.Specs() {
		data, _ := json.Marshal(map[string]any{
			"name":         spec.Name,
			"description":  spec.Description,
			"input_schema": spec.InputSchema,
			"permission":   spec.Permission,
			"risks":        spec.Risks,
			"side_effect":  spec.SideEffect,
		})
		b.WriteString("- ")
		b.Write(data)
		b.WriteString("\n")
	}
	b.WriteString("\n# Disclosed Skills\n")
	b.WriteString(skills.DisclosureText(a.Skills))
	active := skills.ActiveInstructionsText(a.Skills)
	if strings.TrimSpace(active) != "" {
		b.WriteString("\n# Active Skill Instructions\n")
		b.WriteString(active)
	}
	return b.String()
}
