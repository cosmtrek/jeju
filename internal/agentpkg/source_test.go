package agentpkg

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveRegistrySourceFromHTTPIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `entries:
  - id: test/review
    version: 0.1.0
    source: github:owner/repo//agents/review?ref=v0.1.0
    digest: sha256:abc
`)
	}))
	defer server.Close()
	t.Setenv(RegistryIndexEnv, server.URL)

	entry, err := resolveRegistrySource(context.Background(), "jeju:test/review@0.1.0")
	if err != nil {
		t.Fatalf("resolveRegistrySource failed: %v", err)
	}
	if entry.Source != "github:owner/repo//agents/review?ref=v0.1.0" || entry.Digest != "sha256:abc" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestReadRegistryIndexRejectsLargeHTTPBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", registryIndexMaxByte+1)))
	}))
	defer server.Close()

	_, err := readRegistryIndex(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected large registry index error")
	}
	if !strings.Contains(err.Error(), "registry index exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSourceRefRejectsOptionLikeRef(t *testing.T) {
	_, err := sourceRefQuery("ref=-main")
	if err == nil {
		t.Fatal("expected option-like git ref to fail")
	}
	if !strings.Contains(err.Error(), "must not start") {
		t.Fatalf("unexpected error: %v", err)
	}
}
