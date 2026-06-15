package agentpkg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	RegistryIndexEnv     = "JEJU_REGISTRY_INDEX"
	registryHTTPTimeout  = 30 * time.Second
	registryIndexMaxByte = 4 * 1024 * 1024
)

type ResolvedSource struct {
	Type            string `yaml:"type,omitempty" json:"type,omitempty"`
	URL             string `yaml:"url,omitempty" json:"url,omitempty"`
	Ref             string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Commit          string `yaml:"commit,omitempty" json:"commit,omitempty"`
	Subdir          string `yaml:"subdir,omitempty" json:"subdir,omitempty"`
	Registry        string `yaml:"registry,omitempty" json:"registry,omitempty"`
	CanonicalSource string `yaml:"canonicalSource,omitempty" json:"canonical_source,omitempty"`
	Unstable        bool   `yaml:"unstable,omitempty" json:"unstable,omitempty"`
}

func (r ResolvedSource) Map() map[string]any {
	out := map[string]any{}
	if r.Type != "" {
		out["type"] = r.Type
	}
	if r.URL != "" {
		out["url"] = r.URL
	}
	if r.Ref != "" {
		out["ref"] = r.Ref
	}
	if r.Commit != "" {
		out["commit"] = r.Commit
	}
	if r.Subdir != "" {
		out["subdir"] = r.Subdir
	}
	if r.Registry != "" {
		out["registry"] = r.Registry
	}
	if r.CanonicalSource != "" {
		out["canonical_source"] = r.CanonicalSource
	}
	if r.Unstable {
		out["unstable"] = true
	}
	return out
}

type materializedPackage struct {
	Root           string
	Resolved       ResolvedSource
	ExpectedDigest string
	cleanup        func()
}

type gitSource struct {
	URL    string
	Ref    string
	Subdir string
}

func (s *Store) materialize(ctx context.Context, source string) (materializedPackage, error) {
	if source == "" {
		return materializedPackage{}, fmt.Errorf("package source is required")
	}
	if strings.HasPrefix(source, "jeju:") {
		entry, err := resolveRegistrySource(ctx, source)
		if err != nil {
			return materializedPackage{}, err
		}
		materialized, err := s.materialize(ctx, entry.Source)
		if err != nil {
			return materializedPackage{}, err
		}
		materialized.ExpectedDigest = entry.Digest
		materialized.Resolved.Registry = source
		materialized.Resolved.CanonicalSource = entry.Source
		return materialized, nil
	}
	if git, ok, err := parseGitSource(source); err != nil {
		return materializedPackage{}, err
	} else if ok {
		return s.materializeGit(ctx, git)
	}
	if strings.HasSuffix(source, ".jpkg") {
		root, cleanup, err := ExtractArtifact(source, s.cachePath())
		if err != nil {
			return materializedPackage{}, err
		}
		return materializedPackage{
			Root:     root,
			Resolved: ResolvedSource{Type: "artifact", URL: source},
			cleanup:  cleanup,
		}, nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return materializedPackage{}, err
	}
	if !info.IsDir() {
		return materializedPackage{}, fmt.Errorf("unsupported package source %q", source)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return materializedPackage{}, err
	}
	return materializedPackage{
		Root:     abs,
		Resolved: ResolvedSource{Type: "local", URL: abs},
		cleanup:  func() {},
	}, nil
}

func (s *Store) materializeGit(ctx context.Context, spec gitSource) (materializedPackage, error) {
	if err := os.MkdirAll(s.cachePath(), 0o755); err != nil {
		return materializedPackage{}, err
	}
	cloneDir, err := os.MkdirTemp(s.cachePath(), "git-")
	if err != nil {
		return materializedPackage{}, err
	}
	cleanup := func() { _ = os.RemoveAll(cloneDir) }
	if err := runGit(ctx, "", "clone", "--quiet", "--no-checkout", spec.URL, cloneDir); err != nil {
		cleanup()
		return materializedPackage{}, err
	}
	if spec.Ref != "" {
		if err := runGit(ctx, cloneDir, "checkout", "--quiet", spec.Ref); err != nil {
			cleanup()
			return materializedPackage{}, err
		}
	} else if err := runGit(ctx, cloneDir, "checkout", "--quiet", "HEAD"); err != nil {
		cleanup()
		return materializedPackage{}, err
	}
	commitBytes, err := gitOutput(ctx, cloneDir, "rev-parse", "HEAD")
	if err != nil {
		cleanup()
		return materializedPackage{}, err
	}
	root := cloneDir
	if spec.Subdir != "" && spec.Subdir != "." {
		root = filepath.Join(cloneDir, filepath.FromSlash(spec.Subdir))
	}
	if err := ensureInsideRoot(cloneDir, root); err != nil {
		cleanup()
		return materializedPackage{}, err
	}
	return materializedPackage{
		Root: root,
		Resolved: ResolvedSource{
			Type:     "git",
			URL:      spec.URL,
			Ref:      spec.Ref,
			Commit:   strings.TrimSpace(string(commitBytes)),
			Subdir:   spec.Subdir,
			Unstable: !looksImmutableRef(spec.Ref),
		},
		cleanup: cleanup,
	}, nil
}

func parseGitSource(source string) (gitSource, bool, error) {
	if strings.HasPrefix(source, "github:") {
		rest := strings.TrimPrefix(source, "github:")
		main, rawQuery := splitQuery(rest)
		parts := strings.SplitN(main, "//", 2)
		repo := parts[0]
		subdir := "."
		if len(parts) == 2 {
			subdir = parts[1]
		}
		repoParts := strings.Split(repo, "/")
		if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
			return gitSource{}, true, fmt.Errorf("github source must be github:owner/repo//subdir")
		}
		ref, err := sourceRefQuery(rawQuery)
		if err != nil {
			return gitSource{}, true, err
		}
		cleanSubdir, err := cleanSourceSubdir(subdir)
		if err != nil {
			return gitSource{}, true, err
		}
		return gitSource{
			URL:    fmt.Sprintf("https://github.com/%s/%s.git", repoParts[0], repoParts[1]),
			Ref:    ref,
			Subdir: cleanSubdir,
		}, true, nil
	}
	if strings.HasPrefix(source, "git+") {
		rest := strings.TrimPrefix(source, "git+")
		urlPart := rest
		subdirAndQuery := ""
		if idx := strings.Index(rest, ".git//"); idx >= 0 {
			urlPart = rest[:idx+len(".git")]
			subdirAndQuery = rest[idx+len(".git//"):]
		} else {
			urlPart, subdirAndQuery = splitQuery(rest)
			if subdirAndQuery != "" {
				subdirAndQuery = "?" + subdirAndQuery
			}
		}
		subdir, rawQuery := splitQuery(subdirAndQuery)
		ref, err := sourceRefQuery(rawQuery)
		if err != nil {
			return gitSource{}, true, err
		}
		parsed, err := url.Parse(urlPart)
		if err != nil {
			return gitSource{}, true, fmt.Errorf("invalid git source URL: %w", err)
		}
		if parsed.Scheme == "" {
			return gitSource{}, true, fmt.Errorf("invalid git source URL %q: scheme is required", urlPart)
		}
		cleanSubdir, err := cleanSourceSubdir(subdir)
		if err != nil {
			return gitSource{}, true, err
		}
		return gitSource{URL: urlPart, Ref: ref, Subdir: cleanSubdir}, true, nil
	}
	return gitSource{}, false, nil
}

func splitQuery(value string) (string, string) {
	main := value
	query := ""
	if idx := strings.Index(value, "?"); idx >= 0 {
		main = value[:idx]
		query = value[idx+1:]
	}
	return main, query
}

func sourceRefQuery(rawQuery string) (string, error) {
	if rawQuery == "" {
		return "", nil
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", err
	}
	ref := values.Get("ref")
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("git ref must not start with '-'")
	}
	return ref, nil
}

func cleanSourceSubdir(subdir string) (string, error) {
	if subdir == "" {
		subdir = "."
	}
	subdir = filepath.ToSlash(filepath.Clean(subdir))
	if subdir == "." {
		return ".", nil
	}
	if strings.HasPrefix(subdir, "../") || subdir == ".." || strings.HasPrefix(subdir, "/") {
		return "", fmt.Errorf("source subdir escapes repository root")
	}
	return subdir, nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	out, err := gitOutput(ctx, dir, args...)
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.CombinedOutput()
}

var fullCommitRe = regexp.MustCompile(`^[0-9a-fA-F]{12,40}$`)

func looksImmutableRef(ref string) bool {
	if ref == "" {
		return false
	}
	if fullCommitRe.MatchString(ref) {
		return true
	}
	return strings.HasPrefix(ref, "v") && len(ref) > 1 && ref[1] >= '0' && ref[1] <= '9'
}

type registryEntry struct {
	ID      string `yaml:"id"`
	Version string `yaml:"version"`
	Source  string `yaml:"source"`
	Digest  string `yaml:"digest,omitempty"`
}

type registryIndex struct {
	Entries  []registryEntry                    `yaml:"entries,omitempty"`
	Packages map[string]registryPackageVersions `yaml:"packages,omitempty"`
}

type registryPackageVersions struct {
	Versions map[string]registryVersion `yaml:"versions,omitempty"`
}

type registryVersion struct {
	Source string `yaml:"source"`
	Digest string `yaml:"digest,omitempty"`
}

func resolveRegistrySource(ctx context.Context, source string) (registryEntry, error) {
	id, version, err := parseJejuRef(source)
	if err != nil {
		return registryEntry{}, err
	}
	indexPath := os.Getenv(RegistryIndexEnv)
	if indexPath == "" {
		return registryEntry{}, fmt.Errorf("official registry resolver is not configured; set %s to a registry index YAML", RegistryIndexEnv)
	}
	data, err := readRegistryIndex(ctx, indexPath)
	if err != nil {
		return registryEntry{}, err
	}
	var index registryIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return registryEntry{}, err
	}
	for _, entry := range index.Entries {
		if entry.ID == id && entry.Version == version {
			if entry.Source == "" {
				return registryEntry{}, fmt.Errorf("registry entry %s@%s has no source", id, version)
			}
			return entry, nil
		}
	}
	if pkg, ok := index.Packages[id]; ok {
		if item, ok := pkg.Versions[version]; ok {
			if item.Source == "" {
				return registryEntry{}, fmt.Errorf("registry entry %s@%s has no source", id, version)
			}
			return registryEntry{ID: id, Version: version, Source: item.Source, Digest: item.Digest}, nil
		}
	}
	return registryEntry{}, fmt.Errorf("registry entry %s@%s not found", id, version)
}

func parseJejuRef(source string) (string, string, error) {
	value := strings.TrimPrefix(source, "jeju:")
	if value == source || value == "" {
		return "", "", fmt.Errorf("jeju ref must start with jeju:")
	}
	id, version, err := parsePackageSelector(value)
	if err != nil {
		return "", "", err
	}
	if version == "" {
		return "", "", fmt.Errorf("jeju ref must include @version")
	}
	return id, version, nil
}

func readRegistryIndex(ctx context.Context, path string) ([]byte, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: registryHTTPTimeout}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("registry index returned status %d", resp.StatusCode)
		}
		return readLimited(resp.Body, registryIndexMaxByte)
	}
	return os.ReadFile(path)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("registry index exceeds %d bytes", limit)
	}
	return data, nil
}

func (s *Store) cachePath() string {
	return filepath.Join(s.Root, "cache")
}
