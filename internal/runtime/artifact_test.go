package runtime

import "testing"

func TestArtifactIDSortsByStep(t *testing.T) {
	tests := []struct {
		name   string
		step   int
		typ    string
		suffix string
		want   string
	}{
		{
			name: "model input",
			step: 1,
			typ:  "model_input",
			want: "art_step001_model_input",
		},
		{
			name: "model output double digit",
			step: 12,
			typ:  "model_output",
			want: "art_step012_model_output",
		},
		{
			name:   "tool output",
			step:   2,
			typ:    "tool_output",
			suffix: "shell",
			want:   "art_step002_tool_output_shell",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := artifactID(tc.step, tc.typ, tc.suffix)
			if got != tc.want {
				t.Fatalf("artifactID() = %q, want %q", got, tc.want)
			}
		})
	}
}
