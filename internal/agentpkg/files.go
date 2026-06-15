package agentpkg

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var excludedPackageDirs = map[string]bool{
	".git":      true,
	".jeju-dev": true,
	"runs":      true,
	"cache":     true,
}

func DigestDir(root string) (string, error) {
	files, err := collectPackageFiles(root)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, rel); err != nil {
			return "", err
		}
		h.Write([]byte{0})
		if _, err := io.WriteString(h, fmt.Sprintf("%04o", packageFileMode(info.Mode()))); err != nil {
			return "", err
		}
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

func CopyDir(src, dst string) error {
	files, err := collectPackageFiles(src)
	if err != nil {
		return err
	}
	for _, rel := range files {
		srcPath := filepath.Join(src, filepath.FromSlash(rel))
		dstPath := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

type PackResult struct {
	Path    string
	Digest  string
	ID      string
	Version string
}

func Pack(root, outDir string, opts ValidateOptions) (PackResult, error) {
	pkg, err := Validate(root, opts)
	if err != nil {
		return PackResult{}, err
	}
	digest, err := DigestDir(pkg.Root)
	if err != nil {
		return PackResult{}, err
	}
	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return PackResult{}, err
	}
	filename := fmt.Sprintf("%s-%s.jpkg", sanitizeArtifactName(pkg.Manifest.Metadata.ID), pkg.Manifest.Metadata.Version)
	artifactPath := filepath.Join(outDir, filename)
	if err := writeArchive(pkg.Root, artifactPath); err != nil {
		return PackResult{}, err
	}
	return PackResult{
		Path:    artifactPath,
		Digest:  digest,
		ID:      pkg.Manifest.Metadata.ID,
		Version: pkg.Manifest.Metadata.Version,
	}, nil
}

func ExtractArtifact(path, tempRoot string) (string, func(), error) {
	if tempRoot == "" {
		tempRoot = os.TempDir()
	}
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		return "", nil, err
	}
	root, err := os.MkdirTemp(tempRoot, "jeju-package-artifact-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	file, err := os.Open(path)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return "", nil, err
		}
		rel, err := cleanArchivePath(header.Name)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if err := ensureInsideRoot(root, target); err != nil {
			cleanup()
			return "", nil, err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				cleanup()
				return "", nil, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				cleanup()
				return "", nil, err
			}
			mode := archiveFileMode(header.Mode)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				cleanup()
				return "", nil, err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				cleanup()
				return "", nil, err
			}
			if err := out.Close(); err != nil {
				cleanup()
				return "", nil, err
			}
			if err := os.Chmod(target, mode); err != nil {
				cleanup()
				return "", nil, err
			}
		default:
			cleanup()
			return "", nil, fmt.Errorf("archive entry %s has unsupported type %d", header.Name, header.Typeflag)
		}
	}
	return root, cleanup, nil
}

func collectPackageFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink %s is not allowed in package content", path)
		}
		if entry.IsDir() {
			if excludedPackageDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func writeArchive(root, path string) error {
	files, err := collectPackageFiles(root)
	if err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	gz.Name = ""
	gz.ModTime = time.Unix(0, 0)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for _, rel := range files {
		srcPath := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(srcPath)
		if err != nil {
			return err
		}
		header := &tar.Header{
			Name:    rel,
			Mode:    int64(packageFileMode(info.Mode())),
			Size:    info.Size(),
			ModTime: time.Unix(0, 0),
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		file, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, file); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := packageFileMode(info.Mode())
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func packageFileMode(mode os.FileMode) os.FileMode {
	perm := mode.Perm() & 0o777
	if perm == 0 {
		return 0o644
	}
	return perm
}

func archiveFileMode(mode int64) os.FileMode {
	return packageFileMode(os.FileMode(mode))
}

func cleanArchivePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("archive entry path is empty")
	}
	name = filepath.ToSlash(name)
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("archive entry %q is absolute", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive entry %q escapes package root", name)
	}
	return clean, nil
}

func sanitizeArtifactName(value string) string {
	value = strings.ReplaceAll(value, "/", "-")
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return "agent-package"
	}
	return name
}
