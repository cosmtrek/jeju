package compiler

import (
	"encoding/json"
	"strings"

	"github.com/cosmtrek/jeju/internal/model"
	"github.com/cosmtrek/jeju/internal/skills"
)

func (a *CompiledAgent) SystemPrompt() string {
	if a.systemPrompt == "" {
		return a.renderSystemPrompt()
	}
	return a.systemPrompt
}

func (a *CompiledAgent) NativeSystemPrompt() string {
	return flattenPromptMessages(a.PromptMessages(true))
}

func (a *CompiledAgent) PromptMessages(nativeToolCalling bool) []model.Message {
	var messages []model.Message
	if !nativeToolCalling {
		messages = append(messages, model.Message{
			Role:    "system",
			Content: runtimeProtocolText(),
		})
	}
	if text := strings.TrimSpace(a.agentContextText()); text != "" {
		messages = append(messages, model.Message{Role: "system", Content: text})
	}
	if text := strings.TrimSpace(a.toolContextText()); text != "" {
		messages = append(messages, model.Message{Role: "system", Content: text})
	}
	if text := strings.TrimSpace(a.outputContextText(nativeToolCalling)); text != "" {
		messages = append(messages, model.Message{Role: "system", Content: text})
	}
	if text := strings.TrimSpace(skills.DisclosureText(a.Skills)); text != "" {
		messages = append(messages, model.Message{Role: "system", Content: "# Disclosed Skills\n" + text})
	}
	if active := strings.TrimSpace(skills.ActiveInstructionsText(a.Skills)); active != "" {
		messages = append(messages, model.Message{Role: "user", Content: "# Active Skill Instructions\n" + active})
	}
	return messages
}

func runtimeProtocolText() string {
	return `You are running inside Jeju, a config-defined agent runtime.

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

Active skill instructions are task instructions for this run, not optional reference material.
`
}

func (a *CompiledAgent) agentContextText() string {
	var b strings.Builder
	b.WriteString("# Agent Instructions\n")
	b.WriteString(a.Instructions)
	if !strings.HasSuffix(a.Instructions, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# Workspace\n")
	b.WriteString(a.Sandbox.Workdir())
	b.WriteString("\n")
	return b.String()
}

func (a *CompiledAgent) toolContextText() string {
	var b strings.Builder
	b.WriteString("# Available Tools\n")
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
	return b.String()
}

func (a *CompiledAgent) outputContextText(nativeToolCalling bool) string {
	if a.Output.CompiledSchema == nil {
		return ""
	}
	data, _ := json.MarshalIndent(a.Output.Schema, "", "  ")
	var b strings.Builder
	b.WriteString("# Final Output Schema\n")
	b.WriteString("Name: ")
	b.WriteString(a.Output.Name)
	b.WriteString("\n")
	if nativeToolCalling {
		b.WriteString("When you are ready to finish, return only a JSON value that matches this schema. Intermediate tool calls use each tool's own input schema and do not need to match this final output schema.\n")
	} else {
		b.WriteString("When you are ready to finish, the final action content must be a JSON value encoded as a string that matches this schema.\n")
	}
	b.Write(data)
	b.WriteString("\n")
	return b.String()
}

func (a *CompiledAgent) renderSystemPrompt() string {
	return flattenPromptMessages(a.PromptMessages(false))
}

func flattenPromptMessages(messages []model.Message) string {
	var b strings.Builder
	for i, message := range messages {
		if i > 0 {
			b.WriteString("\n")
		}
		if len(messages) > 1 {
			b.WriteString("# Message: ")
			b.WriteString(message.Role)
			b.WriteString("\n")
		}
		b.WriteString(message.Content)
		if !strings.HasSuffix(message.Content, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}
