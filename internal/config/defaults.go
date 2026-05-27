package config

import "jeju/internal/runs"

func ApplyDefaults(m *AgentManifest) {
	for name, provider := range m.Models.Providers {
		if provider.Provider == "deepseek" {
			if provider.BaseURL == "" {
				provider.BaseURL = "https://api.deepseek.com"
			}
			if provider.EnvKey == "" {
				provider.EnvKey = "DEEPSEEK_API_KEY"
			}
		}
		m.Models.Providers[name] = provider
	}

	if m.Runtime.Mode == "" {
		m.Runtime.Mode = "react"
	}
	if m.Runtime.Limits.MaxSteps == 0 {
		if m.Runtime.MaxSteps > 0 {
			m.Runtime.Limits.MaxSteps = m.Runtime.MaxSteps
		} else {
			m.Runtime.Limits.MaxSteps = 20
		}
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
	if m.Runtime.Models.Reasoning == "" {
		m.Runtime.Models.Reasoning = m.Models.Default
	}
	if m.Runtime.Models.Utility == "" {
		m.Runtime.Models.Utility = m.Models.Default
	}
	if m.Runtime.Models.Evaluation == "" {
		m.Runtime.Models.Evaluation = m.Models.Default
	}
	if m.Runtime.React.ActionMode == "" {
		m.Runtime.React.ActionMode = "combined"
	}
	if m.Runtime.React.Reflection == "" {
		m.Runtime.React.Reflection = "off"
	}
	if m.Runtime.React.Compaction == "" {
		m.Runtime.React.Compaction = "off"
	}
	if len(m.Runtime.Interactive.PauseOn) == 0 {
		m.Runtime.Interactive.PauseOn = []string{"permission_required", "agent_question"}
	}
	if m.Skills.Mode == "" {
		m.Skills.Mode = "disclose"
	}
	if m.Skills.Activation.Policy == "" {
		m.Skills.Activation.Policy = "manual"
	}
	if m.Skills.Activation.MaxActive == 0 {
		m.Skills.Activation.MaxActive = 3
	}
	if m.Skills.Loading.Strategy == "" {
		m.Skills.Loading.Strategy = "lazy"
	}
	if m.Sandbox.Type == "" {
		m.Sandbox.Type = "local"
	}
	if m.Sandbox.Workdir == "" {
		m.Sandbox.Workdir = m.Workspace.Path
	}
	if m.Policy.DefaultPermission == "" {
		m.Policy.DefaultPermission = "ask"
	}
	m.Trajectory.Enabled = true
	if m.Trajectory.Format == "" {
		m.Trajectory.Format = "jsonl"
	}
	if m.Trajectory.Store.Type == "" {
		m.Trajectory.Store.Type = "file"
	}
	if m.Trajectory.Store.Path == "" {
		m.Trajectory.Store.Path = "./runs"
	}
	if len(m.Trajectory.Sinks) == 0 {
		m.Trajectory.Sinks = []SinkConfig{
			{Type: "console", Level: "info"},
			{Type: "file", Path: m.Trajectory.Store.Path},
		}
	}
	if m.Evaluate.Enabled && m.Evaluate.Outputs.File == "" {
		m.Evaluate.Outputs.File = runs.EvaluationFile
	}
	if m.Evaluate.Enabled {
		m.Evaluate.OnRunComplete = true
	}
	if m.Evaluate.Enabled && m.Evaluate.Outputs.Path == "" {
		m.Evaluate.Outputs.Path = m.Trajectory.Store.Path
	}
}
