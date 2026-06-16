package agentpkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const StoreEnv = "JEJU_PACKAGES_DIR"

const (
	packageScheme      = "package://"
	packageShortScheme = "p:"
)

type Store struct {
	Root string
}

type AddOptions struct {
	Source         string
	Resolved       ResolvedSource
	ExpectedDigest string
	Activate       bool
	JejuVersion    string
	Replace        bool
}

type AddResult struct {
	ID                string
	Version           string
	Digest            string
	StorePath         string
	AgentManifestPath string
	Source            string
	Resolved          ResolvedSource
	Activated         bool
	Warnings          []string
}

type RunRef struct {
	AgentManifestPath string
	Package           *RunProvenance
}

type RunProvenance struct {
	ID            string         `json:"id"`
	Version       string         `json:"version"`
	Digest        string         `json:"digest"`
	Source        string         `json:"source,omitempty"`
	StorePath     string         `json:"store_path,omitempty"`
	AgentManifest string         `json:"agent_manifest,omitempty"`
	Resolved      ResolvedSource `json:"resolved,omitempty"`
}

type Installed struct {
	Packages map[string]InstalledPackage `yaml:"packages,omitempty"`
}

type InstalledPackage struct {
	Active   string                      `yaml:"active,omitempty"`
	Versions map[string]InstalledVersion `yaml:"versions,omitempty"`
}

type InstalledVersion struct {
	Digest   string         `yaml:"digest"`
	Source   string         `yaml:"source,omitempty"`
	AddedAt  string         `yaml:"addedAt,omitempty"`
	Resolved ResolvedSource `yaml:"resolved,omitempty"`
}

func NewStore(root string) *Store {
	return &Store{Root: root}
}

func DefaultStore() (*Store, error) {
	if root := os.Getenv(StoreEnv); root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		return NewStore(abs), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return NewStore(filepath.Join(home, ".jeju", "packages")), nil
}

func IsPackageBackedRef(ref string) bool {
	return isInstalledPackageRef(ref) ||
		strings.HasPrefix(ref, "github:") ||
		strings.HasPrefix(ref, "git+") ||
		strings.HasPrefix(ref, "jeju:")
}

func isInstalledPackageRef(ref string) bool {
	return strings.HasPrefix(ref, packageScheme) ||
		strings.HasPrefix(ref, packageShortScheme)
}

func (s *Store) Add(ctx context.Context, source string, activate bool, jejuVersion string) (AddResult, error) {
	return s.AddWithOptions(ctx, source, AddOptions{
		Activate:    activate,
		JejuVersion: jejuVersion,
	})
}

func (s *Store) AddWithOptions(ctx context.Context, source string, opts AddOptions) (AddResult, error) {
	materialized, err := s.materialize(ctx, source)
	if err != nil {
		return AddResult{}, err
	}
	defer materialized.cleanup()
	if opts.Source == "" {
		opts.Source = source
	}
	if opts.Resolved.Type == "" {
		opts.Resolved = materialized.Resolved
	}
	if opts.ExpectedDigest == "" {
		opts.ExpectedDigest = materialized.ExpectedDigest
	}
	return s.AddDir(materialized.Root, AddOptions{
		Source:         opts.Source,
		Resolved:       opts.Resolved,
		ExpectedDigest: opts.ExpectedDigest,
		Activate:       opts.Activate,
		JejuVersion:    opts.JejuVersion,
		Replace:        opts.Replace,
	})
}

func (s *Store) AddDir(root string, opts AddOptions) (AddResult, error) {
	pkg, err := Validate(root, ValidateOptions{JejuVersion: opts.JejuVersion})
	if err != nil {
		return AddResult{}, err
	}
	digest, err := DigestDir(pkg.Root)
	if err != nil {
		return AddResult{}, err
	}
	if opts.ExpectedDigest != "" && opts.ExpectedDigest != digest {
		return AddResult{}, fmt.Errorf("digest mismatch: expected %s, got %s", opts.ExpectedDigest, digest)
	}
	storePath := s.contentPath(digest)
	if err := s.ensureContent(pkg.Root, storePath); err != nil {
		return AddResult{}, err
	}
	result := AddResult{
		ID:                pkg.Manifest.Metadata.ID,
		Version:           pkg.Manifest.Metadata.Version,
		Digest:            digest,
		StorePath:         storePath,
		AgentManifestPath: filepath.Join(storePath, filepath.FromSlash(pkg.Manifest.Agent.Manifest)),
		Source:            opts.Source,
		Resolved:          opts.Resolved,
		Activated:         opts.Activate,
		Warnings:          pkg.Warnings,
	}
	if opts.Activate {
		if err := s.activate(result, opts.Replace); err != nil {
			return AddResult{}, err
		}
	}
	return result, nil
}

func (s *Store) ResolveRunRef(ctx context.Context, ref string, jejuVersion string) (RunRef, error) {
	if isInstalledPackageRef(ref) {
		result, err := s.resolveInstalledRunRef(ref, jejuVersion)
		if err != nil {
			return RunRef{}, err
		}
		return result.RunRef(), nil
	}
	if IsPackageBackedRef(ref) {
		result, err := s.Add(ctx, ref, false, jejuVersion)
		if err != nil {
			return RunRef{}, err
		}
		return result.RunRef(), nil
	}
	return RunRef{AgentManifestPath: ref}, nil
}

func (r AddResult) RunRef() RunRef {
	provenance := &RunProvenance{
		ID:            r.ID,
		Version:       r.Version,
		Digest:        r.Digest,
		Source:        r.Source,
		StorePath:     r.StorePath,
		AgentManifest: r.AgentManifestPath,
		Resolved:      r.Resolved,
	}
	return RunRef{AgentManifestPath: r.AgentManifestPath, Package: provenance}
}

func (p RunProvenance) Map() map[string]any {
	out := map[string]any{
		"id":             p.ID,
		"version":        p.Version,
		"digest":         p.Digest,
		"store_path":     p.StorePath,
		"agent_manifest": p.AgentManifest,
	}
	if p.Source != "" {
		out["source"] = p.Source
	}
	if resolved := p.Resolved.Map(); len(resolved) > 0 {
		out["resolved"] = resolved
	}
	return out
}

type PackageListItem struct {
	ID      string
	Version string
	Active  bool
	Digest  string
	Source  string
}

func (s *Store) List() ([]PackageListItem, error) {
	installed, err := s.loadInstalled()
	if err != nil {
		return nil, err
	}
	var items []PackageListItem
	for id, pkg := range installed.Packages {
		for version, item := range pkg.Versions {
			items = append(items, PackageListItem{
				ID:      id,
				Version: version,
				Active:  version == pkg.Active,
				Digest:  item.Digest,
				Source:  item.Source,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ID != items[j].ID {
			return items[i].ID < items[j].ID
		}
		return items[i].Version < items[j].Version
	})
	return items, nil
}

type InspectResult struct {
	ID        string
	Version   string
	Active    bool
	Digest    string
	Source    string
	Resolved  ResolvedSource
	StorePath string
	Manifest  Manifest
	Risk      RiskSummary
	Warnings  []string
}

func (s *Store) Inspect(selector string, jejuVersion string) (InspectResult, error) {
	id, version, err := parsePackageSelector(selector)
	if err != nil {
		return InspectResult{}, err
	}
	installed, err := s.loadInstalled()
	if err != nil {
		return InspectResult{}, err
	}
	pkg, ok := installed.Packages[id]
	if !ok {
		return InspectResult{}, fmt.Errorf("package %q is not installed", id)
	}
	if version == "" {
		version = pkg.Active
	}
	if version == "" {
		return InspectResult{}, fmt.Errorf("package %q has no active version", id)
	}
	item, ok := pkg.Versions[version]
	if !ok {
		return InspectResult{}, fmt.Errorf("package %q version %q is not installed", id, version)
	}
	storePath := s.contentPath(item.Digest)
	validated, err := Validate(storePath, ValidateOptions{JejuVersion: jejuVersion})
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{
		ID:        id,
		Version:   version,
		Active:    version == pkg.Active,
		Digest:    item.Digest,
		Source:    item.Source,
		Resolved:  item.Resolved,
		StorePath: storePath,
		Manifest:  validated.Manifest,
		Risk:      DeriveRisk(validated.AgentManifest),
		Warnings:  validated.Warnings,
	}, nil
}

func (s *Store) Update(ctx context.Context, selector string, version string, jejuVersion string, replace bool) (AddResult, error) {
	id, selectedVersion, err := parsePackageSelector(selector)
	if err != nil {
		return AddResult{}, err
	}
	if version != "" {
		selectedVersion = version
	}
	installed, err := s.loadInstalled()
	if err != nil {
		return AddResult{}, err
	}
	pkg, ok := installed.Packages[id]
	if !ok {
		return AddResult{}, fmt.Errorf("package %q is not installed", id)
	}
	if selectedVersion == "" {
		selectedVersion = pkg.Active
	}
	item, ok := pkg.Versions[selectedVersion]
	if !ok {
		return AddResult{}, fmt.Errorf("package %q version %q is not installed", id, selectedVersion)
	}
	if item.Source == "" {
		return AddResult{}, fmt.Errorf("package %q version %q has no saved source", id, selectedVersion)
	}
	return s.AddWithOptions(ctx, item.Source, AddOptions{
		Activate:    true,
		JejuVersion: jejuVersion,
		Replace:     replace,
	})
}

func (s *Store) UpdateAll(ctx context.Context, jejuVersion string, replace bool) ([]AddResult, error) {
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	var results []AddResult
	for _, item := range items {
		if !item.Active {
			continue
		}
		result, err := s.Update(ctx, item.ID, item.Version, jejuVersion, replace)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Store) Remove(selector string) error {
	id, version, err := parsePackageSelector(selector)
	if err != nil {
		return err
	}
	installed, err := s.loadInstalled()
	if err != nil {
		return err
	}
	pkg, ok := installed.Packages[id]
	if !ok {
		return fmt.Errorf("package %q is not installed", id)
	}
	if version == "" {
		delete(installed.Packages, id)
		_ = os.RemoveAll(filepath.Join(s.Root, "refs", filepath.FromSlash(id)))
		return s.saveInstalled(installed)
	}
	if _, ok := pkg.Versions[version]; !ok {
		return fmt.Errorf("package %q version %q is not installed", id, version)
	}
	delete(pkg.Versions, version)
	_ = os.Remove(s.refPath(id, version))
	if pkg.Active == version {
		pkg.Active = firstVersion(pkg.Versions)
	}
	if len(pkg.Versions) == 0 {
		delete(installed.Packages, id)
	} else {
		installed.Packages[id] = pkg
	}
	return s.saveInstalled(installed)
}

func (s *Store) resolveInstalledRunRef(ref string, jejuVersion string) (AddResult, error) {
	id, version, err := parsePackageURL(ref)
	if err != nil {
		return AddResult{}, err
	}
	installed, err := s.loadInstalled()
	if err != nil {
		return AddResult{}, err
	}
	pkg, ok := installed.Packages[id]
	if !ok {
		return AddResult{}, fmt.Errorf("package %q is not installed", id)
	}
	if version == "" {
		version = pkg.Active
	}
	if version == "" {
		return AddResult{}, fmt.Errorf("package %q has no active version", id)
	}
	item, ok := pkg.Versions[version]
	if !ok {
		return AddResult{}, fmt.Errorf("package %q version %q is not installed", id, version)
	}
	storePath := s.contentPath(item.Digest)
	result, err := s.loadStoredPackageResult(id, version, item, jejuVersion)
	if err != nil {
		return AddResult{}, err
	}
	result.Activated = true
	result.StorePath = storePath
	return result, nil
}

func (s *Store) loadStoredPackageResult(id, version string, item InstalledVersion, jejuVersion string) (AddResult, error) {
	storePath := s.contentPath(item.Digest)
	pkg, err := Load(storePath)
	if err != nil {
		return AddResult{}, err
	}
	if err := validatePackageManifest(pkg); err != nil {
		return AddResult{}, err
	}
	if pkg.Manifest.Metadata.ID != id {
		return AddResult{}, fmt.Errorf("stored package id mismatch: installed %q, manifest %q", id, pkg.Manifest.Metadata.ID)
	}
	if pkg.Manifest.Metadata.Version != version {
		return AddResult{}, fmt.Errorf("stored package version mismatch: installed %q, manifest %q", version, pkg.Manifest.Metadata.Version)
	}
	if err := validateCompatibility(pkg.Manifest.Compatibility.Jeju, jejuVersion); err != nil {
		return AddResult{}, err
	}
	agentPath, err := resolvePackageRelativePath(pkg.Root, pkg.Manifest.Agent.Manifest)
	if err != nil {
		return AddResult{}, fmt.Errorf("agent.manifest: %w", err)
	}
	if err := ensureRegularFile(agentPath); err != nil {
		return AddResult{}, fmt.Errorf("agent.manifest %q: %w", pkg.Manifest.Agent.Manifest, err)
	}
	return AddResult{
		ID:                pkg.Manifest.Metadata.ID,
		Version:           pkg.Manifest.Metadata.Version,
		Digest:            item.Digest,
		StorePath:         storePath,
		AgentManifestPath: agentPath,
		Source:            item.Source,
		Resolved:          item.Resolved,
	}, nil
}

func (s *Store) ensureContent(srcRoot, storePath string) error {
	if _, err := os.Stat(filepath.Join(storePath, ManifestFile)); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(storePath), ".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := CopyDir(srcRoot, tmp); err != nil {
		return err
	}
	if err := os.RemoveAll(storePath); err != nil {
		return err
	}
	return os.Rename(tmp, storePath)
}

func (s *Store) activate(result AddResult, replace bool) error {
	installed, err := s.loadInstalled()
	if err != nil {
		return err
	}
	if installed.Packages == nil {
		installed.Packages = map[string]InstalledPackage{}
	}
	pkg := installed.Packages[result.ID]
	if pkg.Versions == nil {
		pkg.Versions = map[string]InstalledVersion{}
	}
	if existing, ok := pkg.Versions[result.Version]; ok && existing.Digest != result.Digest && !replace {
		return fmt.Errorf("package %s@%s already exists with digest %s; refusing to replace with %s without --replace", result.ID, result.Version, existing.Digest, result.Digest)
	}
	pkg.Active = result.Version
	pkg.Versions[result.Version] = InstalledVersion{
		Digest:   result.Digest,
		Source:   result.Source,
		AddedAt:  time.Now().UTC().Format(time.RFC3339),
		Resolved: result.Resolved,
	}
	installed.Packages[result.ID] = pkg
	if err := s.saveInstalled(installed); err != nil {
		return err
	}
	s.writeRef(result.ID, result.Version, result.StorePath)
	return nil
}

func (s *Store) loadInstalled() (Installed, error) {
	var installed Installed
	path := filepath.Join(s.Root, "installed.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		installed.Packages = map[string]InstalledPackage{}
		return installed, nil
	}
	if err != nil {
		return installed, err
	}
	if err := yaml.Unmarshal(data, &installed); err != nil {
		return installed, err
	}
	if installed.Packages == nil {
		installed.Packages = map[string]InstalledPackage{}
	}
	return installed, nil
}

func (s *Store) saveInstalled(installed Installed) error {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(installed)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Root, "installed.yaml"), data, 0o644)
}

func (s *Store) contentPath(digest string) string {
	hex := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(s.Root, "store", "sha256", hex)
}

func (s *Store) refPath(id, version string) string {
	return filepath.Join(s.Root, "refs", filepath.FromSlash(id), version)
}

func (s *Store) writeRef(id, version, storePath string) {
	path := s.refPath(id, version)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.Remove(path)
	rel, err := filepath.Rel(filepath.Dir(path), storePath)
	if err != nil {
		return
	}
	if err := os.Symlink(rel, path); err == nil {
		return
	}
	_ = os.WriteFile(path, []byte(storePath+"\n"), 0o644)
}

func parsePackageURL(ref string) (string, string, error) {
	value := ""
	switch {
	case strings.HasPrefix(ref, packageScheme):
		value = strings.TrimPrefix(ref, packageScheme)
	case strings.HasPrefix(ref, packageShortScheme):
		value = strings.TrimPrefix(ref, packageShortScheme)
	default:
		return "", "", fmt.Errorf("package ref must start with package:// or p:")
	}
	if value == "" {
		return "", "", fmt.Errorf("package ref must include namespace/name")
	}
	return parsePackageSelector(value)
}

func parsePackageSelector(value string) (string, string, error) {
	at := strings.LastIndex(value, "@")
	id := value
	version := ""
	if at >= 0 {
		id = value[:at]
		version = value[at+1:]
	}
	if !packageIDRe.MatchString(id) {
		return "", "", fmt.Errorf("package id must match namespace/name, got %q", id)
	}
	if version != "" && !semverRe.MatchString(version) {
		return "", "", fmt.Errorf("package version must be semantic version, got %q", version)
	}
	return id, version, nil
}

func firstVersion(versions map[string]InstalledVersion) string {
	best := ""
	for version := range versions {
		if best == "" || comparePackageVersion(version, best) > 0 {
			best = version
		}
	}
	return best
}

func comparePackageVersion(a, b string) int {
	aVersion, aOK := parsePackageVersion(a)
	bVersion, bOK := parsePackageVersion(b)
	if aOK && bOK {
		return compareParsedPackageVersion(aVersion, bVersion)
	}
	if aOK {
		return 1
	}
	if bOK {
		return -1
	}
	return strings.Compare(a, b)
}

type packageVersion struct {
	core          semverCore
	prerelease    []string
	hasPrerelease bool
}

func parsePackageVersion(value string) (packageVersion, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if idx := strings.Index(value, "+"); idx >= 0 {
		value = value[:idx]
	}
	coreText := value
	prereleaseText := ""
	if idx := strings.Index(value, "-"); idx >= 0 {
		coreText = value[:idx]
		prereleaseText = value[idx+1:]
	}
	core, ok := parseSemverCore(coreText)
	if !ok {
		return packageVersion{}, false
	}
	version := packageVersion{core: core}
	if prereleaseText != "" {
		version.hasPrerelease = true
		version.prerelease = strings.Split(prereleaseText, ".")
	}
	return version, true
}

func compareParsedPackageVersion(a, b packageVersion) int {
	if cmp := compareSemver(a.core, b.core); cmp != 0 {
		return cmp
	}
	if a.hasPrerelease != b.hasPrerelease {
		if a.hasPrerelease {
			return -1
		}
		return 1
	}
	if !a.hasPrerelease {
		return 0
	}
	limit := len(a.prerelease)
	if len(b.prerelease) < limit {
		limit = len(b.prerelease)
	}
	for i := 0; i < limit; i++ {
		if cmp := comparePrereleaseIdentifier(a.prerelease[i], b.prerelease[i]); cmp != 0 {
			return cmp
		}
	}
	return compareInt(len(a.prerelease), len(b.prerelease))
}

func comparePrereleaseIdentifier(a, b string) int {
	aNum, aIsNum := parsePrereleaseNumber(a)
	bNum, bIsNum := parsePrereleaseNumber(b)
	switch {
	case aIsNum && bIsNum:
		return compareInt(aNum, bNum)
	case aIsNum:
		return -1
	case bIsNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func parsePrereleaseNumber(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return n, true
}
