//go:build windows

package server

// dropPrivileges is a no-op on Windows — the service runs as a Virtual
// Service Account (NT SERVICE\gitignore), which is already least-privilege
// and has no analogous setuid/setgid drop (see AI.md PART 23).
func dropPrivileges(_, _ string) error {
	return nil
}

// SetRuntimeOwnership is unnecessary on Windows, where ACL inheritance follows
// the service identity and no Unix ownership transition occurs.
func SetRuntimeOwnership(_, _ string, _ ...string) error {
	return nil
}
