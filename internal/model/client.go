package model

import "context"

type Client interface {
	Generate(ctx context.Context, req Request) (Response, error)
}

type ProviderConfig struct {
	Name            string
	Provider        string
	Model           string
	BaseURL         string
	EnvKey          string
	APIKeyEnv       string
	JSONMode        bool
	Temperature     *float64
	MaxOutputTokens int
	TimeoutSec      int
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model       string
	Messages    []Message
	Temperature *float64
	MaxTokens   int
	Metadata    map[string]any
}

type Response struct {
	Text      string
	Raw       []byte
	Usage     Usage
	LatencyMS int64
	Model     string
	Provider  string
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
