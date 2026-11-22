package path

import (
	"os"
	"path/filepath"
	"strings"
)

// TempDir creates a private project-scoped temporary directory.
func TempDir() (string, error) {
	org := tempComponent(os.Getenv("PROJECT_ORG"), OrgName)
	internal := tempComponent(os.Getenv("INTERNAL_NAME"), ProjectName)
	base := filepath.Join(os.TempDir(), org)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(base, internal+"-")
}

func tempComponent(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return fallback
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return fallback
		}
	}
	return value
}
