package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type MockClient struct {
	Config ProviderConfig
}

func NewMockClient(cfg ProviderConfig) *MockClient {
	return &MockClient{Config: cfg}
}

func (c *MockClient) Generate(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	default:
	}

	task := metadataString(req.Metadata, "task")
	observations := strings.ToLower(metadataString(req.Metadata, "observations"))
	taskLower := strings.ToLower(task)

	var payload map[string]any
	if shouldMockWrite(taskLower, observations) {
		payload = map[string]any{
			"type":    "tool_call",
			"thought": "The task asks for a saved file, so I will write the requested notes into the workspace.",
			"tool":    "write",
			"input": map[string]any{
				"path":    mockWritePath(taskLower),
				"content": mockReport(task),
			},
		}
	} else {
		payload = map[string]any{
			"type":    "final",
			"thought": "The mock model has enough information to finish.",
			"content": mockReport(task),
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:      string(data),
		Raw:       data,
		LatencyMS: time.Since(start).Milliseconds(),
		Model:     c.Config.Model,
		Provider:  c.Config.Provider,
	}, nil
}

func shouldMockWrite(taskLower, observations string) bool {
	if strings.Contains(observations, "tool write completed") || strings.Contains(observations, "write ok") {
		return false
	}
	return strings.Contains(taskLower, "notes.md") ||
		strings.Contains(taskLower, "保存") ||
		strings.Contains(taskLower, "save") ||
		strings.Contains(taskLower, "write") ||
		strings.Contains(taskLower, "写入")
}

func mockWritePath(taskLower string) string {
	if strings.Contains(taskLower, "notes.md") {
		return "notes.md"
	}
	return "notes.md"
}

func mockReport(task string) string {
	if strings.TrimSpace(task) == "" {
		task = "local agent task"
	}
	return fmt.Sprintf(`# Jeju Mock Result

Task: %s

This is a deterministic mock response. It demonstrates the full Jeju run lifecycle: model action parsing, permission checking, tool execution, trajectory recording, final output, and rule-based evaluation.
`, task)
}

func metadataString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
