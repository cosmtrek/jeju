package config

import (
	"os"
	"regexp"
)

var envRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func ResolveEnv(m *AgentManifest) {
	for name, provider := range m.Models.Providers {
		provider.BaseURL = expandEnv(provider.BaseURL)
		provider.Model = expandEnv(provider.Model)
		provider.EnvKey = expandEnv(provider.EnvKey)
		m.Models.Providers[name] = provider
	}
}

func expandEnv(value string) string {
	return envRefRe.ReplaceAllStringFunc(value, func(match string) string {
		key := envRefRe.FindStringSubmatch(match)[1]
		return os.Getenv(key)
	})
}
