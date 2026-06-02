package cli

import (
	"context"
	"strings"
	"testing"
)

func TestVersionCommandPrintsBuildInfo(t *testing.T) {
	text := captureStdout(t, func() {
		if err := Execute(context.Background(), []string{"version"}); err != nil {
			t.Fatalf("version command failed: %v", err)
		}
	})

	for _, want := range []string{"jeju dev", "commit:", "branch:", "built:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("version output missing %q:\n%s", want, text)
		}
	}
}
