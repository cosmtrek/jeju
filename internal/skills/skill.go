package skills

type Manifest struct {
	Metadata   Metadata
	Disclosure Disclosure
	Path       string
}

type Metadata struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  string
}

type Disclosure struct {
}

type Skill struct {
	Manifest     Manifest
	Instructions string
	Active       bool
}
