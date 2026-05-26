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
	if c.Config.APIKeyEnv != "" {
		apiKey = os.Getenv(c.Config.APIKeyEnv)
	}
	if apiKey == "" {
		return Response{}, fmt.Errorf("missing API key env %q", c.Config.APIKeyEnv)
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
		MaxTokens:   req.MaxTokens,
	}
	if body.Temperature == nil {
		body.Temperature = c.Config.Temperature
	}
	if body.MaxTokens == 0 {
		body.MaxTokens = c.Config.MaxOutputTokens
	}
	if c.Config.JSONMode {
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
	return Response{
		Text: parsed.Choices[0].Message.Content,
		Raw:  raw,
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

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage chatUsage `json:"usage"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
