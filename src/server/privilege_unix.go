//go:build !windows

package server

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// ProcessStartedElevated reports whether this process started as root. Callers
// capture it before dropPrivileges so later update policy does not depend on
// the process's current identity.
func ProcessStartedElevated() bool { return os.Geteuid() == 0 }

// dropPrivileges drops root privileges to the named service user/group,
// once the listener is already bound to its (possibly privileged) port.
// No-op if the process is not running as root (see AI.md PART 23).
func dropPrivileges(username, groupname string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if username == "" {
		username = "gitignore"
	}
	if groupname == "" {
		groupname = username
	}

	grp, err := user.LookupGroup(groupname)
	if err != nil {
		return fmt.Errorf("group %q not found: %w", groupname, err)
	}
	usr, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("user %q not found: %w", username, err)
	}

	gid, err := strconv.Atoi(grp.Gid)
	if err != nil {
		return fmt.Errorf("invalid gid %q: %w", grp.Gid, err)
	}
	uid, err := strconv.Atoi(usr.Uid)
	if err != nil {
		return fmt.Errorf("invalid uid %q: %w", usr.Uid, err)
	}

	// Order matters: drop supplementary groups, then the primary group,
	// then the user last — setuid must be last since dropping the uid
	// first would remove permission to change the gid.
	if err := syscall.Setgroups([]int{gid}); err != nil {
		return fmt.Errorf("setgroups: %w", err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("setgid: %w", err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("setuid: %w", err)
	}

	return nil
}

// SetRuntimeOwnership gives the service account ownership of runtime state
// created during root startup, before the process permanently drops privileges.
func SetRuntimeOwnership(username, groupname string, roots ...string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	usr, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("service user %q not found: %w", username, err)
	}
	grp, err := user.LookupGroup(groupname)
	if err != nil {
		return fmt.Errorf("service group %q not found: %w", groupname, err)
	}
	uid, err := strconv.Atoi(usr.Uid)
	if err != nil {
		return fmt.Errorf("invalid uid %q: %w", usr.Uid, err)
	}
	gid, err := strconv.Atoi(grp.Gid)
	if err != nil {
		return fmt.Errorf("invalid gid %q: %w", grp.Gid, err)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			return os.Chown(path, uid, gid)
		})
		if err != nil {
			return fmt.Errorf("set service ownership for %s: %w", root, err)
		}
	}
	return nil
}
