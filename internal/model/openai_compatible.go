package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type OpenAICompatibleClient struct {
	Config ProviderConfig
	client *http.Client
}

func NewOpenAICompatibleClient(cfg ProviderConfig) *OpenAICompatibleClient {
	timeout := 120 * time.Second
	if cfg.TimeoutSec > 0 {
		timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	return &OpenAICompatibleClient{
		Config: cfg,
		client: &http.Client{Timeout: timeout},
	}
}

func (c *OpenAICompatibleClient) Generate(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	apiKey := ""
	apiKeyEnv := c.Config.EnvKey
	if apiKeyEnv != "" {
		apiKey = os.Getenv(apiKeyEnv)
	}
	if apiKey == "" {
		return Response{}, fmt.Errorf("missing API key env %q", apiKeyEnv)
	}
	baseURL := strings.TrimRight(c.Config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	modelName := req.Model
	if modelName == "" {
		modelName = c.Config.Model
	}
	body := chatRequest{
		Model:       modelName,
		Messages:    req.Messages,
		Temperature: req.Temperature,
	}
	if body.Temperature == nil {
		body.Temperature = c.Config.Temperature
	}
	tokenLimit := req.MaxTokens
	if tokenLimit == 0 {
		tokenLimit = c.Config.MaxOutputTokens
	}
	if tokenLimit > 0 {
		if c.usesMaxCompletionTokens() {
			body.MaxCompletionTokens = tokenLimit
		} else {
			body.MaxTokens = tokenLimit
		}
	}
	if c.Config.Thinking.Type != "" && c.Config.Thinking.Type != "auto" && c.supportsThinkingObject() {
		body.Thinking = &thinkingConfig{Type: c.Config.Thinking.Type}
	}
	if c.Config.Thinking.Effort != "" && c.supportsReasoningEffort() {
		body.ReasoningEffort = c.Config.Thinking.Effort
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]chatTool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			body.Tools = append(body.Tools, chatTool{
				Type: "function",
				Function: chatFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  normalizeToolParameters(tool.Parameters),
					Strict:      tool.Strict && c.supportsStrictTools(),
				},
			})
		}
		body.ToolChoice = "auto"
	}
	if req.ResponseFormat != nil {
		body.ResponseFormat = c.responseFormat(req.ResponseFormat)
	} else if c.Config.JSONMode {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()
	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("chat completions status %d: %s", httpResp.StatusCode, string(raw))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, err
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("chat completions returned no choices")
	}
	message := parsed.Choices[0].Message
	return Response{
		Text:             message.Content,
		ReasoningContent: message.ReasoningContent,
		ToolCalls:        message.ToolCalls,
		Raw:              raw,
		Usage: Usage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
			TotalTokens:  parsed.Usage.TotalTokens,
		},
		LatencyMS: time.Since(start).Milliseconds(),
		Model:     parsed.Model,
		Provider:  c.Config.Provider,
	}, nil
}

func (c *OpenAICompatibleClient) usesMaxCompletionTokens() bool {
	return c.Config.Provider == "mimo" || c.Config.Provider == "openaiCompatible"
}

func (c *OpenAICompatibleClient) supportsThinkingObject() bool {
	return c.Config.Provider == "deepseek" || c.Config.Provider == "mimo"
}

func (c *OpenAICompatibleClient) supportsReasoningEffort() bool {
	return c.Config.Provider == "deepseek" || c.Config.Provider == "openaiCompatible"
}

func (c *OpenAICompatibleClient) supportsStrictTools() bool {
	return c.Config.Provider != "deepseek"
}

func (c *OpenAICompatibleClient) responseFormat(format *ResponseFormat) *responseFormat {
	if format == nil {
		return nil
	}
	if format.Type == "jsonSchema" && c.Config.JSONSchemaMode {
		name := format.Name
		if name == "" {
			name = "jeju_response"
		}
		return &responseFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchemaResponseFormat{
				Name:   name,
				Schema: normalizeToolParameters(format.Schema),
				Strict: format.Strict,
			},
		}
	}
	return &responseFormat{Type: "json_object"}
}

func normalizeToolParameters(schema any) any {
	if schema == nil {
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		}
	}
	return schema
}

type chatRequest struct {
	Model               string          `json:"model"`
	Messages            []Message       `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	Thinking            *thinkingConfig `json:"thinking,omitempty"`
	ResponseFormat      *responseFormat `json:"response_format,omitempty"`
	Tools               []chatTool      `json:"tools,omitempty"`
	ToolChoice          string          `json:"tool_choice,omitempty"`
}

type responseFormat struct {
	Type       string                    `json:"type"`
	JSONSchema *jsonSchemaResponseFormat `json:"json_schema,omitempty"`
}

type jsonSchemaResponseFormat struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
}

type thinkingConfig struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage chatUsage `json:"usage"`
}

type chatMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content"`
	ToolCalls        []ToolCall `json:"tool_calls"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
