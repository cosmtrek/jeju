package compiler

import (
	"encoding/json"
	"strings"

	"jeju/internal/skills"
)

func (a *CompiledAgent) SystemPrompt() string {
	if a.systemPrompt == "" {
		return a.renderSystemPrompt()
	}
	return a.systemPrompt
}

func (a *CompiledAgent) NativeSystemPrompt() string {
	var b strings.Builder
	b.WriteString(`You are running inside Jeju, a config-defined agent runtime.

Use the provided function tools when they are needed. Ask the user for more information by calling ask_user. When the task is complete, call final_answer.

Do not simulate tool calls in text. Do not write fake <tool_result> blocks. If a tool is needed, call the actual function tool and wait for Jeju to return the result.
`)
	a.writeSharedPromptSections(&b)
	b.WriteString(`
# Runtime Protocol
This run uses native function calling. If any agent or skill instruction mentions Jeju action JSON, ignore that output format and use the API function tools instead. Final answers must use the final_answer function tool.
`)
	return b.String()
}

func (a *CompiledAgent) renderSystemPrompt() string {
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
	a.writeSharedPromptSections(&b)
	return b.String()
}

func (a *CompiledAgent) writeSharedPromptSections(b *strings.Builder) {
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
			"capabilities": spec.Capabilities,
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
}
