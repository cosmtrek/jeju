package runtime

import "testing"

func TestStepArtifactNameSortsByStep(t *testing.T) {
	tests := []struct {
		name   string
		step   int
		typ    string
		suffix string
		ext    string
		want   string
	}{
		{
			name: "model input",
			step: 1,
			typ:  "model_input",
			ext:  "json",
			want: "step001_model_input.json",
		},
		{
			name: "model output double digit",
			step: 12,
			typ:  "model_output",
			ext:  "txt",
			want: "step012_model_output.txt",
		},
		{
			name:   "tool output",
			step:   2,
			typ:    "tool_output",
			suffix: "shell",
			ext:    "json",
			want:   "step002_tool_output_shell.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stepArtifactName(tc.step, tc.typ, tc.suffix, tc.ext)
			if got != tc.want {
				t.Fatalf("stepArtifactName() = %q, want %q", got, tc.want)
			}
		})
	}
}
