package agentpkg

import (
	"sort"

	"github.com/cosmtrek/jeju/internal/config"
)

type RiskSummary struct {
	Level        string   `json:"level"`
	Access       string   `json:"access"`
	Approval     string   `json:"approval"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func DeriveRisk(manifest config.AgentManifest) RiskSummary {
	capabilities := map[string]bool{}
	for _, tool := range manifest.Tools {
		for _, capability := range tool.Capabilities {
			capabilities[capability] = true
		}
		for _, capability := range inferredCapabilities(tool.Uses) {
			capabilities[capability] = true
		}
	}
	list := make([]string, 0, len(capabilities))
	for capability := range capabilities {
		list = append(list, capability)
	}
	sort.Strings(list)
	level := "low"
	if manifest.Permissions.Access == "full" || capabilities["networkWrite"] {
		level = "high"
	} else if capabilities["command"] || capabilities["workspaceWrite"] || capabilities["networkRead"] {
		level = "medium"
	}
	return RiskSummary{
		Level:        level,
		Access:       manifest.Permissions.Access,
		Approval:     manifest.Permissions.Approval,
		Capabilities: list,
	}
}

func inferredCapabilities(uses string) []string {
	switch uses {
	case "builtin:read", "builtin:search":
		return []string{"workspaceRead"}
	case "builtin:write", "builtin:edit":
		return []string{"workspaceRead", "workspaceWrite"}
	case "builtin:shell", "command":
		return []string{"command", "workspaceRead", "workspaceWrite"}
	case "http":
		return []string{"networkRead"}
	default:
		return nil
	}
}
