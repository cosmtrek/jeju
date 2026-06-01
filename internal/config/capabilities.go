package config

type Capabilities struct {
	ModelProviderTypes []string
	ModelPresets       []string
	ToolUses           []string
	EvaluatorUses      []string
	TrajectoryFormats  []string
}

func SupportedCapabilities() Capabilities {
	return Capabilities{
		ModelProviderTypes: copyStrings(supportedModelTypes),
		ModelPresets:       copyStrings(supportedModelPresets),
		ToolUses:           copyStrings(supportedToolUses),
		EvaluatorUses:      copyStrings(supportedEvaluatorUses),
		TrajectoryFormats:  copyStrings(supportedTrajectoryFormats),
	}
}

func copyStrings(values []string) []string {
	return append([]string(nil), values...)
}

func boolSet(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
