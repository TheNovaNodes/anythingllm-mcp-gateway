package etl

import (
	"strings"
)

var defaultBlacklist = map[string]bool{
	".agents":       true,
	"scratch":       true,
	"tmp":           true,
	"cache":         true,
	".git":          true,
	"node_modules":  true,
	"vendor":        true,
	"__pycache__":   true,
	"logs":          true,
	".venv":         true,
	"venv":          true,
	"dist":          true,
	"build":         true,
	".pytest_cache": true,
	".gemini":       true,
}

// IsBlacklisted returns true if any path segment in the relative path matches the blacklist or snapshot patterns.
func IsBlacklisted(relPath string) bool {
	norm := strings.ReplaceAll(relPath, "\\", "/")
	parts := strings.Split(norm, "/")
	for _, part := range parts {
		clean := strings.TrimSpace(part)
		if clean == "" || clean == "." || clean == ".." {
			continue
		}
		if defaultBlacklist[clean] {
			return true
		}
		if strings.HasPrefix(clean, ".") {
			return true
		}
		// Filter out snapshot/fork directories (-f04, -f03, -poc, -mvp)
		if IsSnapshotRepo(clean) {
			return true
		}
	}
	return false
}

// IsSnapshotRepo returns true if the repository or directory name is a snapshot or draft fork.
func IsSnapshotRepo(name string) bool {
	clean := strings.ToLower(strings.TrimSpace(name))
	suffixes := []string{"-f04", "-f03", "-f02", "-f01", "-poc", "-mvp", "-draft"}
	for _, s := range suffixes {
		if strings.HasSuffix(clean, s) {
			return true
		}
	}
	return false
}

// SlugFromRepo converts "organization" and "repository" names to a normalized workspace slug.
func SlugFromRepo(account, repo string) string {
	accClean := strings.ToLower(strings.TrimSpace(account))
	repoClean := strings.ToLower(strings.TrimSpace(repo))
	combined := accClean + "-" + repoClean
	combined = strings.ReplaceAll(combined, "_", "-")
	combined = strings.ReplaceAll(combined, " ", "-")
	for strings.Contains(combined, "--") {
		combined = strings.ReplaceAll(combined, "--", "-")
	}
	return strings.Trim(combined, "-")
}
