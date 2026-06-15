package agentpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPackageFileModesArePreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not preserve Unix executable bits consistently")
	}
	root := writeValidPackage(t)
	scriptPath := filepath.Join(root, "bin", "tool.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("mkdir bin failed: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
	executableDigest, err := DigestDir(root)
	if err != nil {
		t.Fatalf("DigestDir failed: %v", err)
	}

	copyRoot := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(root, copyRoot); err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}
	assertExecutable(t, filepath.Join(copyRoot, "bin", "tool.sh"))

	packed, err := Pack(root, t.TempDir(), ValidateOptions{})
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}
	extracted, cleanup, err := ExtractArtifact(packed.Path, t.TempDir())
	if err != nil {
		t.Fatalf("ExtractArtifact failed: %v", err)
	}
	defer cleanup()
	assertExecutable(t, filepath.Join(extracted, "bin", "tool.sh"))

	if err := os.Chmod(scriptPath, 0o644); err != nil {
		t.Fatalf("chmod script failed: %v", err)
	}
	plainDigest, err := DigestDir(root)
	if err != nil {
		t.Fatalf("DigestDir after chmod failed: %v", err)
	}
	if plainDigest == executableDigest {
		t.Fatalf("digest should change when executable mode changes: %s", plainDigest)
	}
}

func assertExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s failed: %v", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s mode = %o, want executable bit", path, info.Mode().Perm())
	}
}
