package model

import (
	"context"
	"encoding/json"
)

type Client interface {
	Generate(ctx context.Context, req Request) (Response, error)
}

type ProviderConfig struct {
	Name            string
	Provider        string
	Model           string
	BaseURL         string
	EnvKey          string
	JSONMode        bool
	Temperature     *float64
	Thinking        ThinkingConfig
	MaxOutputTokens int
	ContextWindow   int
	TimeoutSec      int
	ToolCalling     bool
	JSONSchemaMode  bool
}

type ThinkingConfig struct {
	Type   string
	Effort string
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

type Request struct {
	Model          string
	Messages       []Message
	Temperature    *float64
	MaxTokens      int
	Tools          []ToolDefinition
	ResponseFormat *ResponseFormat
	Metadata       map[string]any
}

type Response struct {
	Text             string
	ReasoningContent string
	ToolCalls        []ToolCall
	Raw              []byte
	Usage            Usage
	LatencyMS        int64
	Model            string
	Provider         string
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  any
	Strict      bool
}

type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ResponseFormat struct {
	Type   string
	Name   string
	Schema any
	Strict bool
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type Registry struct {
	clients map[string]Client
	configs map[string]ProviderConfig
}

func NewRegistry() *Registry {
	return &Registry{
		clients: map[string]Client{},
		configs: map[string]ProviderConfig{},
	}
}

func (r *Registry) Add(name string, cfg ProviderConfig, client Client) {
	r.configs[name] = cfg
	r.clients[name] = client
}

func (r *Registry) Get(name string) (Client, ProviderConfig, bool) {
	client, ok := r.clients[name]
	if !ok {
		return nil, ProviderConfig{}, false
	}
	return client, r.configs[name], true
}
