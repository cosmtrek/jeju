package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOpenAICompatibleClientMapsThinkingAndTokenLimit(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		provider                string
		wantThinking            bool
		wantReasoningEffort     bool
		wantMaxTokens           bool
		wantMaxCompletionTokens bool
	}{
		{
			name:                    "mimo",
			provider:                "mimo",
			wantThinking:            true,
			wantMaxCompletionTokens: true,
		},
		{
			name:                "deepseek",
			provider:            "deepseek",
			wantThinking:        true,
			wantReasoningEffort: true,
			wantMaxTokens:       true,
		},
		{
			name:                    "openai compatible",
			provider:                "openaiCompatible",
			wantReasoningEffort:     true,
			wantMaxCompletionTokens: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{}"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
			}))
			defer server.Close()

			t.Setenv("JEJU_TEST_API_KEY", "test")
			client := NewOpenAICompatibleClient(ProviderConfig{
				Provider:        tc.provider,
				Model:           "test-model",
				BaseURL:         server.URL,
				EnvKey:          "JEJU_TEST_API_KEY",
				MaxOutputTokens: 42,
				Thinking: ThinkingConfig{
					Type:   "disabled",
					Effort: "high",
				},
			})

			if _, err := client.Generate(context.Background(), Request{
				Messages: []Message{{Role: "user", Content: "hello"}},
			}); err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			_, hasThinking := request["thinking"]
			if hasThinking != tc.wantThinking {
				t.Fatalf("thinking presence got %v, want %v in request %#v", hasThinking, tc.wantThinking, request)
			}
			_, hasReasoningEffort := request["reasoning_effort"]
			if hasReasoningEffort != tc.wantReasoningEffort {
				t.Fatalf("reasoning_effort presence got %v, want %v in request %#v", hasReasoningEffort, tc.wantReasoningEffort, request)
			}
			_, hasMaxTokens := request["max_tokens"]
			if hasMaxTokens != tc.wantMaxTokens {
				t.Fatalf("max_tokens presence got %v, want %v in request %#v", hasMaxTokens, tc.wantMaxTokens, request)
			}
			_, hasMaxCompletionTokens := request["max_completion_tokens"]
			if hasMaxCompletionTokens != tc.wantMaxCompletionTokens {
				t.Fatalf("max_completion_tokens presence got %v, want %v in request %#v", hasMaxCompletionTokens, tc.wantMaxCompletionTokens, request)
			}
		})
	}
}

func TestOpenAICompatibleClientRequiresAPIKey(t *testing.T) {
	t.Setenv("JEJU_MISSING_API_KEY", "")
	_, err := NewOpenAICompatibleClient(ProviderConfig{
		Provider: "openaiCompatible",
		Model:    "test-model",
		EnvKey:   "JEJU_MISSING_API_KEY",
	}).Generate(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil {
		t.Fatal("expected missing API key error")
	}
	_ = os.Unsetenv("JEJU_MISSING_API_KEY")
}

func TestOpenAICompatibleClientSendsToolsAndParsesToolCalls(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"test-model",
			"choices":[{
				"message":{
					"role":"assistant",
					"content":"",
					"reasoning_content":"I need to write a file.",
					"tool_calls":[{
						"id":"call_1",
						"type":"function",
						"function":{"name":"write","arguments":"{\"path\":\"notes.md\",\"content\":\"hello\"}"}
					}]
				}
			}],
			"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}
		}`))
	}))
	defer server.Close()

	t.Setenv("JEJU_TEST_API_KEY", "test")
	client := NewOpenAICompatibleClient(ProviderConfig{
		Provider:       "openaiCompatible",
		Model:          "test-model",
		BaseURL:        server.URL,
		EnvKey:         "JEJU_TEST_API_KEY",
		ToolCalling:    true,
		JSONSchemaMode: true,
	})

	resp, err := client.Generate(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools: []ToolDefinition{{
			Name:        "write",
			Description: "Write a file",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required": []string{"path", "content"},
			},
			Strict: false,
		}},
		ResponseFormat: &ResponseFormat{
			Type:   "jsonSchema",
			Name:   "final",
			Strict: true,
			Schema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"content": map[string]any{"type": "string"}},
				"required":             []string{"content"},
				"additionalProperties": false,
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", resp.ToolCalls)
	}
	if resp.ReasoningContent != "I need to write a file." {
		t.Fatalf("unexpected reasoning content %q", resp.ReasoningContent)
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "write" {
		t.Fatalf("unexpected tool call: %+v", resp.ToolCalls[0])
	}
	var args map[string]string
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("tool arguments are not JSON: %v", err)
	}
	if args["path"] != "notes.md" || args["content"] != "hello" {
		t.Fatalf("unexpected arguments: %+v", args)
	}
	if request["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice auto, got %#v", request["tool_choice"])
	}
	if _, ok := request["tools"].([]any); !ok {
		t.Fatalf("expected tools array in request: %#v", request)
	}
	responseFormat, ok := request["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_schema" {
		t.Fatalf("expected json_schema response_format, got %#v", request["response_format"])
	}
}

func TestOpenAICompatibleClientDoesNotForceJSONModeWhenToolsArePresent(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"test-model",
			"choices":[{
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call_1",
						"type":"function",
						"function":{"name":"write","arguments":"{\"path\":\"notes.md\",\"content\":\"hello\"}"}
					}]
				}
			}],
			"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}
		}`))
	}))
	defer server.Close()

	t.Setenv("JEJU_TEST_API_KEY", "test")
	client := NewOpenAICompatibleClient(ProviderConfig{
		Provider:    "mimo",
		Model:       "test-model",
		BaseURL:     server.URL,
		EnvKey:      "JEJU_TEST_API_KEY",
		JSONMode:    true,
		ToolCalling: true,
	})

	if _, err := client.Generate(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools: []ToolDefinition{{
			Name:       "write",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		}},
	}); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, ok := request["response_format"]; ok {
		t.Fatalf("did not expect automatic JSON response_format with tools: %#v", request["response_format"])
	}
	if request["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice auto, got %#v", request["tool_choice"])
	}
}

func TestOpenAICompatibleClientReplaysReasoningContent(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{\"content\":\"ok\"}"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()

	t.Setenv("JEJU_TEST_API_KEY", "test")
	client := NewOpenAICompatibleClient(ProviderConfig{
		Provider: "mimo",
		Model:    "test-model",
		BaseURL:  server.URL,
		EnvKey:   "JEJU_TEST_API_KEY",
	})

	_, err := client.Generate(context.Background(), Request{
		Messages: []Message{{
			Role:             "assistant",
			ReasoningContent: "preserved reasoning",
			ToolCalls: []ToolCall{{
				ID:        "call_1",
				Name:      "write",
				Arguments: json.RawMessage(`{"path":"notes.md","content":"hello"}`),
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one message in request: %#v", request["messages"])
	}
	message := messages[0].(map[string]any)
	if message["reasoning_content"] != "preserved reasoning" {
		t.Fatalf("reasoning_content was not replayed: %#v", message)
	}
}

func TestOpenAICompatibleClientParsesCacheHitTokens(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage string
		want  int
	}{
		{
			name:  "deepseek prompt_cache_hit_tokens",
			usage: `{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102,"prompt_cache_hit_tokens":64,"prompt_cache_miss_tokens":36}`,
			want:  64,
		},
		{
			name:  "openai prompt_tokens_details.cached_tokens",
			usage: `{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102,"prompt_tokens_details":{"cached_tokens":80}}`,
			want:  80,
		},
		{
			name:  "no cache fields",
			usage: `{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102}`,
			want:  0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"role":"assistant","content":"{}"}}],"usage":` + tc.usage + `}`))
			}))
			defer server.Close()

			t.Setenv("JEJU_TEST_API_KEY", "test")
			client := NewOpenAICompatibleClient(ProviderConfig{
				Provider: "deepseek",
				Model:    "test-model",
				BaseURL:  server.URL,
				EnvKey:   "JEJU_TEST_API_KEY",
			})

			resp, err := client.Generate(context.Background(), Request{
				Messages: []Message{{Role: "user", Content: "hello"}},
			})
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}
			if resp.Usage.CacheHitTokens != tc.want {
				t.Fatalf("CacheHitTokens got %d, want %d", resp.Usage.CacheHitTokens, tc.want)
			}
			if resp.Usage.InputTokens != 100 {
				t.Fatalf("InputTokens got %d, want 100", resp.Usage.InputTokens)
			}
		})
	}
}
