package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAcceptsAgentSkillsFrontmatter(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "web-research")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: web-research
description: Research web sources. Use when current external information is needed.
license: Apache-2.0
compatibility: Requires network access.
metadata:
  jeju.capabilities: source_collection
allowed-tools: search_api write
---

# Web Research
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	skill, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if skill.Manifest.Metadata.Name != "web-research" {
		t.Fatalf("unexpected name %q", skill.Manifest.Metadata.Name)
	}
	if skill.Manifest.Metadata.AllowedTools != "search_api write" {
		t.Fatalf("unexpected allowed-tools %q", skill.Manifest.Metadata.AllowedTools)
	}
	if skill.Manifest.Metadata.Metadata["jeju.capabilities"] != "source_collection" {
		t.Fatalf("unexpected metadata %#v", skill.Manifest.Metadata.Metadata)
	}
}

func TestLoadRejectsNonSpecFrontmatter(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "web-research")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: web-research
description: Research web sources.
whenToUse:
  - Need current information.
---
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "field whenToUse not found") {
		t.Fatalf("expected unknown frontmatter field error, got %v", err)
	}
}

func TestLoadRequiresNameToMatchDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "web-research")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: web_research
description: Research web sources.
---
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "lowercase letters, numbers, and hyphens") {
		t.Fatalf("expected name validation error, got %v", err)
	}
}
