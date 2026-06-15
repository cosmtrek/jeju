package agentpkg

import (
	"fmt"
	"strconv"
	"strings"
)

type semverCore struct {
	major int
	minor int
	patch int
}

func validateCompatibility(expr, current string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" || current == "" || current == "dev" {
		return nil
	}
	currentVersion, ok := parseSemverCore(current)
	if !ok {
		return nil
	}
	for _, token := range strings.Fields(expr) {
		op, versionText, err := splitConstraint(token)
		if err != nil {
			return fmt.Errorf("compatibility.jeju %q: %w", expr, err)
		}
		required, ok := parseSemverCore(versionText)
		if !ok {
			return fmt.Errorf("compatibility.jeju %q has invalid version %q", expr, versionText)
		}
		if !constraintMatches(currentVersion, op, required) {
			return fmt.Errorf("compatibility.jeju %q does not allow current version %q", expr, current)
		}
	}
	return nil
}

func splitConstraint(token string) (string, string, error) {
	for _, op := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(token, op) {
			return op, strings.TrimPrefix(token, op), nil
		}
	}
	return "=", token, nil
}

func parseSemverCore(value string) (semverCore, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if idx := strings.IndexAny(value, "+-"); idx >= 0 {
		value = value[:idx]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semverCore{}, false
	}
	var parsed [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return semverCore{}, false
		}
		parsed[i] = n
	}
	return semverCore{major: parsed[0], minor: parsed[1], patch: parsed[2]}, true
}

func constraintMatches(current semverCore, op string, required semverCore) bool {
	cmp := compareSemver(current, required)
	switch op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	case "=":
		return cmp == 0
	default:
		return false
	}
}

func compareSemver(a, b semverCore) int {
	if a.major != b.major {
		return compareInt(a.major, b.major)
	}
	if a.minor != b.minor {
		return compareInt(a.minor, b.minor)
	}
	return compareInt(a.patch, b.patch)
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
