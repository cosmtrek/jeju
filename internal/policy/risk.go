package policy

var KnownRisks = map[string]bool{
	"read":        true,
	"write":       true,
	"execute":     true,
	"network":     true,
	"credential":  true,
	"external":    true,
	"destructive": true,
	"production":  true,
	"payment":     true,
	"message":     true,
}
