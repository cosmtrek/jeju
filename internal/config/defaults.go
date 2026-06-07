package config

func ApplyDefaults(m *AgentManifest) {
	for name, provider := range m.Models.Providers {
		switch provider.Preset {
		case "deepseek":
			if provider.Type == "" {
				provider.Type = "openaiCompatible"
			}
			if provider.BaseURL == "" {
				provider.BaseURL = "https://api.deepseek.com"
			}
			if provider.EnvKey == "" {
				provider.EnvKey = "DEEPSEEK_API_KEY"
			}
			if provider.Thinking.Type == "" {
				provider.Thinking.Type = "disabled"
			}
			if provider.ContextWindow == 0 {
				provider.ContextWindow = 128000
			}
		case "mimo":
			if provider.Type == "" {
				provider.Type = "openaiCompatible"
			}
			if provider.BaseURL == "" {
				provider.BaseURL = "https://api.xiaomimimo.com/v1"
			}
			if provider.EnvKey == "" {
				provider.EnvKey = "MIMO_API_KEY"
			}
			if provider.Thinking.Type == "" {
				provider.Thinking.Type = "disabled"
			}
			if provider.ContextWindow == 0 {
				provider.ContextWindow = 128000
			}
		}
		if provider.Type == "mock" && provider.ContextWindow == 0 {
			provider.ContextWindow = 128000
		}
		m.Models.Providers[name] = provider
	}

	if m.Runtime.Loop.Type == "" {
		m.Runtime.Loop.Type = "react"
	}
	if m.Runtime.CompressionThreshold == 0 {
		m.Runtime.CompressionThreshold = 0.8
	}
	if m.Runtime.RecentTokenBudget == 0 {
		m.Runtime.RecentTokenBudget = 20000
	}
	if m.Runtime.Model == "" && len(m.Models.Providers) == 1 {
		for name := range m.Models.Providers {
			m.Runtime.Model = name
		}
	}
	if m.Runtime.Limits.MaxSteps == 0 {
		m.Runtime.Limits.MaxSteps = 20
	}
	if m.Runtime.Limits.MaxDurationSec == 0 {
		m.Runtime.Limits.MaxDurationSec = 900
	}
	if m.Runtime.Limits.MaxToolCalls == 0 {
		m.Runtime.Limits.MaxToolCalls = 50
	}
	if m.Runtime.Limits.MaxConsecutiveErrors == 0 {
		m.Runtime.Limits.MaxConsecutiveErrors = 3
	}
	if m.Permissions.Access == "" {
		m.Permissions.Access = "workspace"
	}
	if m.Permissions.Approval == "" {
		m.Permissions.Approval = "onRequest"
	}
}
