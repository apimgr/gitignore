package service

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/apimgr/gitignore/src/paths"
)

const (
	appName = "gitignore"
	orgName = "apimgr"

	// launchd label per spec: {org}.{app}
	launchdLabel = "apimgr.gitignore"
	// launchd plist path per spec
	launchdPlist = "/Library/LaunchDaemons/apimgr.gitignore.plist"
)

// ServiceType represents the type of service manager
type ServiceType int

const (
	ServiceUnknown ServiceType = iota
	ServiceSystemd
	ServiceRunit
	ServiceOpenRC
	ServiceSysVinit
	ServiceLaunchd
	ServiceWindows
	ServiceBSDRC
)

// okMark and warnMark return the emoji status marker, or a plain-text
// fallback when NO_COLOR is set, per the NO_COLOR convention used
// throughout this project (see src/main.go resolveColor).
func okMark() string {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return "[OK]"
	}
	return "✅"
}

func warnMark() string {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return "[WARN]"
	}
	return "⚠️ "
}

// DetectServiceManager detects the system's service manager
func DetectServiceManager() ServiceType {
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat("/run/systemd/system"); err == nil {
			return ServiceSystemd
		}
		if _, err := os.Stat("/run/runit"); err == nil {
			return ServiceRunit
		}
		if _, err := os.Stat("/sbin/openrc-run"); err == nil {
			return ServiceOpenRC
		}
		if _, err := os.Stat("/etc/systemd"); err == nil {
			return ServiceSystemd
		}
		// SysVinit only when openrc-run and systemctl are both absent, and
		// /etc/init.d exists with a working update-rc.d or chkconfig.
		if _, err := os.Stat("/etc/init.d"); err == nil {
			if hasExecutable("update-rc.d") || hasExecutable("chkconfig") {
				return ServiceSysVinit
			}
		}
		return ServiceUnknown

	case "darwin":
		return ServiceLaunchd

	case "windows":
		return ServiceWindows

	case "freebsd", "openbsd", "netbsd":
		return ServiceBSDRC

	default:
		return ServiceUnknown
	}
}

func hasExecutable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Install installs the service for the detected service manager
func Install() error {
	serviceType := DetectServiceManager()

	switch serviceType {
	case ServiceSystemd:
		return installSystemd()
	case ServiceRunit:
		return installRunit()
	case ServiceOpenRC:
		return installOpenRC()
	case ServiceSysVinit:
		return installSysVinit()
	case ServiceLaunchd:
		return installLaunchd()
	case ServiceWindows:
		return installWindows()
	case ServiceBSDRC:
		return installBSDRC()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

// Uninstall removes the service. Unless force is true, it prompts for
// confirmation before deleting all config/data/cache/log/backup
// directories, the PID file, and the system user/group it created
// (see AI.md PART 23).
func Uninstall(force bool) error {
	if !force {
		if !confirmPrompt(fmt.Sprintf(
			"This will stop the service and permanently delete all %s data,\nconfig, logs, backups, and the system user. Continue? [y/N]: ", appName)) {
			fmt.Println("Uninstall cancelled.")
			return nil
		}
	}

	serviceType := DetectServiceManager()

	var err error
	switch serviceType {
	case ServiceSystemd:
		err = uninstallSystemd()
	case ServiceRunit:
		err = uninstallRunit()
	case ServiceOpenRC:
		err = uninstallOpenRC()
	case ServiceSysVinit:
		err = uninstallSysVinit()
	case ServiceLaunchd:
		err = uninstallLaunchd()
	case ServiceWindows:
		err = uninstallWindows()
	case ServiceBSDRC:
		err = uninstallBSDRC()
	default:
		err = fmt.Errorf("unsupported service manager")
	}
	if err != nil {
		return err
	}

	removeAllData()
	removeSystemUser()

	fmt.Printf("%s Service uninstalled. Delete the binary manually if desired: rm %s\n", okMark(), GetBinaryPath())
	return nil
}

// confirmPrompt prints prompt and reads a y/N response from stdin.
func confirmPrompt(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// removeAllData deletes the config, data, cache, log, and backup
// directories plus the PID file created for this service.
func removeAllData() {
	configDir, dataDir, logsDir, backupDir := paths.GetDefaultDirs(appName)
	dirs := []string{configDir, dataDir, paths.GetCacheDir(), logsDir, backupDir}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			fmt.Printf("%s Failed to remove %s: %v\n", warnMark(), dir, err)
		}
	}

	if pidFile := paths.GetPIDFile(); pidFile != "" {
		if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
			fmt.Printf("%s Failed to remove PID file: %v\n", warnMark(), err)
		}
	}
}

// removeSystemUser deletes the system user/group created by
// EnsureSystemUser / ensureMacOSServiceUser / ensureBSDServiceUser, if any.
func removeSystemUser() {
	switch runtime.GOOS {
	case "linux":
		if exec.Command("id", appName).Run() != nil {
			return
		}
		if err := exec.Command("userdel", "-r", appName).Run(); err != nil {
			fmt.Printf("%s Failed to remove user %s: %v\n", warnMark(), appName, err)
		}
		exec.Command("groupdel", appName).Run()
		fmt.Printf("%s System user '%s' removed\n", okMark(), appName)

	case "darwin":
		if exec.Command("dscl", ".", "-read", "/Users/"+appName).Run() != nil {
			return
		}
		exec.Command("dscl", ".", "-delete", "/Users/"+appName).Run()
		exec.Command("dscl", ".", "-delete", "/Groups/"+appName).Run()
		fmt.Printf("%s System account '%s' removed\n", okMark(), appName)

	case "freebsd", "openbsd", "netbsd":
		if exec.Command("id", appName).Run() != nil {
			return
		}
		exec.Command("pw", "userdel", "-n", appName).Run()
		exec.Command("pw", "groupdel", "-n", appName).Run()
		fmt.Printf("%s System user '%s' removed\n", okMark(), appName)
	}
	// Windows Virtual Service Accounts are managed automatically by the
	// OS and removed with the service — nothing to clean up here.
}

// GetBinaryPath returns the path where the binary should be installed
func GetBinaryPath() string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(`C:\Program Files\%s\%s\%s.exe`, orgName, appName, appName)
	default:
		return fmt.Sprintf("/usr/local/bin/%s", appName)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Privilege escalation (Part 23 / Part 5 "Smart escalation flow")
// ─────────────────────────────────────────────────────────────────────────────

// EscalationMethod names an available way to gain elevated privileges.
type EscalationMethod string

const (
	EscalateNone   EscalationMethod = ""
	EscalateSudo   EscalationMethod = "sudo"
	EscalateSu     EscalationMethod = "su"
	EscalatePkexec EscalationMethod = "pkexec"
	EscalateDoas   EscalationMethod = "doas"
	EscalateRunas  EscalationMethod = "runas"
)

// IsElevated reports whether the process already has the privileges
// needed to install a system-wide service (root on Unix, Administrator
// on Windows).
func IsElevated() bool {
	if runtime.GOOS == "windows" {
		// "net session" only succeeds for an elevated/Administrator process.
		return exec.Command("net", "session").Run() == nil
	}
	return os.Geteuid() == 0
}

// DetectEscalation finds the first usable escalation method for the
// current platform, tried in the order specified by AI.md PART 23
// (Linux: sudo -> su -> pkexec -> doas; Windows: runas).
func DetectEscalation() EscalationMethod {
	if IsElevated() {
		return EscalateNone
	}

	if runtime.GOOS == "windows" {
		if hasExecutable("runas") {
			return EscalateRunas
		}
		return EscalateNone
	}

	if hasExecutable("sudo") {
		if exec.Command("sudo", "-n", "true").Run() == nil || canSudoInteractively() {
			return EscalateSudo
		}
	}
	if hasExecutable("su") {
		return EscalateSu
	}
	if hasExecutable("pkexec") {
		return EscalatePkexec
	}
	if hasExecutable("doas") {
		return EscalateDoas
	}
	return EscalateNone
}

// canSudoInteractively checks group membership for sudo/wheel/admin,
// indicating the user can sudo with an interactive password prompt.
func canSudoInteractively() bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	gids, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, gid := range gids {
		g, err := user.LookupGroupId(gid)
		if err != nil {
			continue
		}
		switch g.Name {
		case "sudo", "wheel", "admin":
			return true
		}
	}
	return false
}

// ExecElevated re-executes args (args[0] is the binary path) with elevated
// privileges using the given method, keeping stdio attached so any
// password/consent prompt stays interactive.
func ExecElevated(method EscalationMethod, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no arguments to re-execute")
	}

	var cmd *exec.Cmd
	switch method {
	case EscalateSudo:
		cmd = exec.Command("sudo", args...)
	case EscalateSu:
		cmd = exec.Command("su", "-c", shellJoin(args))
	case EscalatePkexec:
		cmd = exec.Command("pkexec", args...)
	case EscalateDoas:
		cmd = exec.Command("doas", args...)
	case EscalateRunas:
		cmd = exec.Command("runas", append([]string{"/user:Administrator"}, args...)...)
	default:
		return fmt.Errorf("no escalation method available")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// shellJoin quotes args for a POSIX shell -c string, as required by `su -c`.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// InstallUser installs a user-level (non-privileged) service, used as a
// fallback when no privilege escalation method is available.
func InstallUser() error {
	switch runtime.GOOS {
	case "linux":
		return installSystemdUser()
	case "darwin":
		return installLaunchdUser()
	default:
		return fmt.Errorf("user-level service install is not supported on %s", runtime.GOOS)
	}
}

func installSystemdUser() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current binary: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve home directory: %w", err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", unitDir, err)
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=gitignore API Server (user service)
After=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
`, binaryPath)

	unitPath := filepath.Join(unitDir, appName+".service")
	if err := os.WriteFile(unitPath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write unit file: %w", err)
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload user systemd: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", appName).Run(); err != nil {
		return fmt.Errorf("failed to enable user service: %w", err)
	}

	fmt.Printf("%s User-level systemd service installed at: %s\n", okMark(), unitPath)
	return nil
}

func installLaunchdUser() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current binary: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve home directory: %w", err)
	}
	agentDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", agentDir, err)
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>

    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`, launchdLabel, binaryPath)

	plistPath := filepath.Join(agentDir, launchdLabel+".plist")
	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("failed to load user agent: %w", err)
	}

	fmt.Printf("%s User-level LaunchAgent installed at: %s\n", okMark(), plistPath)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// User creation (Part 4 / Part 23)
// ─────────────────────────────────────────────────────────────────────────────

// EnsureSystemUser creates the Linux system user/group if they don't
// already exist. No-op on other platforms (see ensureMacOSServiceUser /
// ensureBSDServiceUser for their platform-specific equivalents).
func EnsureSystemUser() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// Check if user already exists
	if err := exec.Command("id", appName).Run(); err == nil {
		return nil // already exists
	}

	id, err := findAvailableSystemID()
	if err != nil {
		return fmt.Errorf("no available UID/GID in safe system range 200-899: %w", err)
	}

	// Create group
	if err := exec.Command("groupadd",
		"--system",
		"--gid", strconv.Itoa(id),
		appName,
	).Run(); err != nil {
		return fmt.Errorf("failed to create group %s: %w", appName, err)
	}

	// Create user
	configDir := fmt.Sprintf("/etc/%s/%s", orgName, appName)
	if err := exec.Command("useradd",
		"--system",
		"--uid", strconv.Itoa(id),
		"--gid", strconv.Itoa(id),
		"--home-dir", configDir,
		"--shell", "/sbin/nologin",
		"--comment", appName+" service account",
		appName,
	).Run(); err != nil {
		return fmt.Errorf("failed to create user %s: %w", appName, err)
	}

	fmt.Printf("%s System user '%s' created (uid=%d gid=%d)\n", okMark(), appName, id, id)
	return nil
}

// ensureMacOSServiceUser creates the dedicated macOS service account via
// dscl (UID/GID 200-399, hidden from login window), if it doesn't already
// exist. The launchd plist deliberately omits UserName/GroupName (see
// installLaunchd) — the account exists purely so the binary can drop
// privileges to it in-process after binding, per AI.md PART 23.
func ensureMacOSServiceUser(homeDir string) error {
	if exec.Command("dscl", ".", "-read", "/Users/"+appName).Run() == nil {
		return nil // already exists
	}

	id, err := findAvailableMacOSSystemID()
	if err != nil {
		return fmt.Errorf("no available UID/GID in macOS safe range 200-399: %w", err)
	}

	commands := [][]string{
		{"dscl", ".", "-create", "/Groups/" + appName},
		{"dscl", ".", "-create", "/Groups/" + appName, "PrimaryGroupID", strconv.Itoa(id)},
		{"dscl", ".", "-create", "/Groups/" + appName, "Password", "*"},
		{"dscl", ".", "-create", "/Users/" + appName},
		{"dscl", ".", "-create", "/Users/" + appName, "UniqueID", strconv.Itoa(id)},
		{"dscl", ".", "-create", "/Users/" + appName, "PrimaryGroupID", strconv.Itoa(id)},
		{"dscl", ".", "-create", "/Users/" + appName, "UserShell", "/usr/bin/false"},
		{"dscl", ".", "-create", "/Users/" + appName, "RealName", appName + " service account"},
		{"dscl", ".", "-create", "/Users/" + appName, "NFSHomeDirectory", homeDir},
		{"dscl", ".", "-create", "/Users/" + appName, "Password", "*"},
		{"dscl", ".", "-create", "/Users/" + appName, "IsHidden", "1"},
	}
	for _, c := range commands {
		if err := exec.Command(c[0], c[1:]...).Run(); err != nil {
			return fmt.Errorf("failed to run %v: %w", c, err)
		}
	}

	fmt.Printf("%s macOS service account '%s' created (uid=%d gid=%d)\n", okMark(), appName, id, id)
	return nil
}

// findAvailableMacOSSystemID finds an unused UID/GID pair in the macOS
// safe 200-399 range, skipping the same reserved well-known service IDs.
func findAvailableMacOSSystemID() (int, error) {
	for id := 399; id >= 200; id-- {
		if reservedIDs[id] {
			continue
		}
		uidOut, _ := exec.Command("dscl", ".", "-search", "/Users", "UniqueID", strconv.Itoa(id)).CombinedOutput()
		gidOut, _ := exec.Command("dscl", ".", "-search", "/Groups", "PrimaryGroupID", strconv.Itoa(id)).CombinedOutput()
		if strings.TrimSpace(string(uidOut)) == "" && strings.TrimSpace(string(gidOut)) == "" {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no available UID/GID in safe range 200-399")
}

// ensureBSDServiceUser creates the FreeBSD/OpenBSD/NetBSD service user via
// pw, if it doesn't already exist.
func ensureBSDServiceUser(homeDir string) error {
	if exec.Command("id", appName).Run() == nil {
		return nil // already exists
	}

	id, err := findAvailableSystemID()
	if err != nil {
		return fmt.Errorf("no available UID/GID in safe system range 200-899: %w", err)
	}

	if err := exec.Command("pw", "groupadd", "-n", appName, "-g", strconv.Itoa(id)).Run(); err != nil {
		return fmt.Errorf("failed to create group %s: %w", appName, err)
	}
	if err := exec.Command("pw", "useradd",
		"-n", appName,
		"-u", strconv.Itoa(id),
		"-g", strconv.Itoa(id),
		"-d", homeDir,
		"-s", "/usr/sbin/nologin",
		"-c", appName+" service account",
	).Run(); err != nil {
		return fmt.Errorf("failed to create user %s: %w", appName, err)
	}

	fmt.Printf("%s System user '%s' created (uid=%d gid=%d)\n", okMark(), appName, id, id)
	return nil
}

// reservedIDs lists UIDs/GIDs used by well-known services across distros.
// These are NEVER used even if they appear available on the current system,
// to avoid conflicts if those services are installed later (see PART 23).
var reservedIDs = map[int]bool{
	// nobody
	65534: true,
	// systemd-*, docker
	999: true, 998: true, 997: true, 996: true, 995: true,
	// systemd-*, kvm
	994: true, 993: true, 992: true, 991: true, 990: true,
	// sgx, pipewire, colord
	989: true, 988: true, 987: true, 986: true, 985: true,
	// avahi, rtkit, saned, usbmux, cups-pk-helper
	984: true, 983: true, 982: true, 981: true, 980: true,
	// common services (101-110) and legacy DB servers (170-179)
	101: true, 102: true, 103: true, 104: true, 105: true,
	106: true, 107: true, 108: true, 109: true, 110: true,
	170: true, 171: true, 172: true, 173: true, 174: true,
	175: true, 176: true, 177: true, 178: true, 179: true,
}

// findAvailableSystemID finds an unused UID/GID pair in the safe 200-899 range,
// skipping reserved well-known service IDs (see PART 23). The same value is used
// for both UID and GID, and both must be free.
func findAvailableSystemID() (int, error) {
	for id := 899; id >= 200; id-- {
		if reservedIDs[id] {
			continue
		}
		uidTaken := exec.Command("getent", "passwd", strconv.Itoa(id)).Run() == nil
		gidTaken := exec.Command("getent", "group", strconv.Itoa(id)).Run() == nil
		if !uidTaken && !gidTaken {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no available UID/GID in safe range 200-899")
}

// ─────────────────────────────────────────────────────────────────────────────
// systemd
// ─────────────────────────────────────────────────────────────────────────────

func installSystemd() error {
	binaryPath := GetBinaryPath()

	// Create required directories before the system user, since the home
	// directory (/etc/{org}/{app}) must exist before useradd references it.
	dirs := []string{
		fmt.Sprintf("/var/lib/%s/%s", orgName, appName),
		fmt.Sprintf("/var/log/%s/%s", orgName, appName),
		fmt.Sprintf("/etc/%s/%s", orgName, appName),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Ensure service user exists now that its home directory is in place
	if err := EnsureSystemUser(); err != nil {
		fmt.Printf("%s Could not create system user (continuing): %v\n", warnMark(), err)
	}

	for _, dir := range dirs {
		// Set ownership if user exists
		exec.Command("chown", appName+":"+appName, dir).Run()
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=gitignore API Server
Documentation=https://gitignore.apimgr.us
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%s
Group=%s
ExecStart=%s
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ReadWritePaths=/var/lib/%s/%s /var/log/%s/%s /etc/%s/%s

[Install]
WantedBy=multi-user.target
`, appName, appName, binaryPath,
		orgName, appName, orgName, appName, orgName, appName)

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", appName)

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}
	if err := exec.Command("systemctl", "enable", appName).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	fmt.Printf("%s Service installed at: %s\n", okMark(), servicePath)
	fmt.Printf("%s Binary installed at: %s\n", okMark(), binaryPath)
	fmt.Printf("\nTo start the service:\n  sudo systemctl start %s\n", appName)
	return nil
}

func uninstallSystemd() error {
	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", appName)
	exec.Command("systemctl", "stop", appName).Run()
	exec.Command("systemctl", "disable", appName).Run()
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove service file: %w", err)
	}
	exec.Command("systemctl", "daemon-reload").Run()
	fmt.Printf("%s Service uninstalled: %s\n", okMark(), servicePath)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// runit
// ─────────────────────────────────────────────────────────────────────────────

func installRunit() error {
	svDir := fmt.Sprintf("/etc/sv/%s", appName)
	binaryPath := GetBinaryPath()

	if err := os.MkdirAll(svDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	runScript := fmt.Sprintf("#!/bin/sh\nexec %s 2>&1\n", binaryPath)
	if err := os.WriteFile(filepath.Join(svDir, "run"), []byte(runScript), 0755); err != nil {
		return fmt.Errorf("failed to write run script: %w", err)
	}

	logDir := filepath.Join(svDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	logRun := "#!/bin/sh\nexec svlogd -tt ./main\n"
	if err := os.WriteFile(filepath.Join(logDir, "run"), []byte(logRun), 0755); err != nil {
		return fmt.Errorf("failed to write log run script: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	os.Symlink(svDir, fmt.Sprintf("/var/service/%s", appName))
	fmt.Printf("%s Runit service installed at: %s\n", okMark(), svDir)
	return nil
}

func uninstallRunit() error {
	exec.Command("sv", "stop", appName).Run()
	os.Remove(fmt.Sprintf("/var/service/%s", appName))
	os.RemoveAll(fmt.Sprintf("/etc/sv/%s", appName))
	fmt.Printf("%s Runit service uninstalled\n", okMark())
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// OpenRC
// ─────────────────────────────────────────────────────────────────────────────

func installOpenRC() error {
	binaryPath := GetBinaryPath()

	if err := EnsureSystemUser(); err != nil {
		fmt.Printf("%s Could not create system user (continuing): %v\n", warnMark(), err)
	}

	scriptContent := fmt.Sprintf(`#!/sbin/openrc-run

name="%s"
description="gitignore API Server"
command="%s"
command_args=""
command_user="%s:%s"
pidfile="/var/run/%s/%s.pid"
command_background=true
output_log="/var/log/%s/%s/server.log"
error_log="/var/log/%s/%s/error.log"

depend() {
    need net
    after firewall
    use dns logger
}

start_pre() {
    checkpath -d -m 0755 -o %s:%s /var/run/%s
    checkpath -d -m 0755 -o %s:%s /var/log/%s/%s
}
`, appName, binaryPath, appName, appName,
		orgName, appName,
		orgName, appName,
		orgName, appName,
		appName, appName, orgName,
		appName, appName, orgName, appName)

	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write OpenRC script: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	if err := exec.Command("rc-update", "add", appName, "default").Run(); err != nil {
		return fmt.Errorf("failed to enable OpenRC service: %w", err)
	}

	fmt.Printf("%s OpenRC service installed at: %s\n", okMark(), scriptPath)
	fmt.Printf("\nTo start the service:\n  sudo rc-service %s start\n", appName)
	return nil
}

func uninstallOpenRC() error {
	exec.Command("rc-service", appName, "stop").Run()
	exec.Command("rc-update", "del", appName, "default").Run()
	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove OpenRC script: %w", err)
	}
	fmt.Printf("%s OpenRC service uninstalled\n", okMark())
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// SysVinit
// ─────────────────────────────────────────────────────────────────────────────

func installSysVinit() error {
	binaryPath := GetBinaryPath()

	if err := EnsureSystemUser(); err != nil {
		fmt.Printf("%s Could not create system user (continuing): %v\n", warnMark(), err)
	}

	scriptContent := fmt.Sprintf(`#!/bin/sh
### BEGIN INIT INFO
# Provides:          %s
# Required-Start:    $network $remote_fs $syslog
# Required-Stop:     $network $remote_fs $syslog
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: gitignore API Server
# Description:       gitignore API Server daemon
### END INIT INFO

NAME=%s
DAEMON=%s
DAEMON_USER=%s
PIDFILE=/var/run/%s/%s.pid
LOGFILE=/var/log/%s/%s/server.log

case "$1" in
    start)
        echo "Starting $NAME..."
        mkdir -p $(dirname $PIDFILE) $(dirname $LOGFILE)
        chown -R $DAEMON_USER:$DAEMON_USER $(dirname $PIDFILE) $(dirname $LOGFILE)
        start-stop-daemon --start --quiet --background --make-pidfile \
            --pidfile $PIDFILE --chuid $DAEMON_USER --exec $DAEMON \
            --no-close >> $LOGFILE 2>&1
        ;;
    stop)
        echo "Stopping $NAME..."
        start-stop-daemon --stop --quiet --pidfile $PIDFILE --retry 30
        rm -f $PIDFILE
        ;;
    restart)
        $0 stop
        sleep 1
        $0 start
        ;;
    status)
        if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
            echo "$NAME is running (pid $(cat $PIDFILE))"
            exit 0
        else
            echo "$NAME is stopped"
            exit 3
        fi
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
exit 0
`, appName, appName, binaryPath, appName, orgName, appName, orgName, appName)

	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write SysVinit script: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	if hasExecutable("update-rc.d") {
		exec.Command("update-rc.d", appName, "defaults").Run()
	} else if hasExecutable("chkconfig") {
		exec.Command("chkconfig", "--add", appName).Run()
		exec.Command("chkconfig", appName, "on").Run()
	}

	fmt.Printf("%s SysVinit script installed at: %s\n", okMark(), scriptPath)
	fmt.Printf("\nTo start the service:\n  sudo /etc/init.d/%s start\n", appName)
	return nil
}

func uninstallSysVinit() error {
	exec.Command("/etc/init.d/"+appName, "stop").Run()
	if hasExecutable("update-rc.d") {
		exec.Command("update-rc.d", "-f", appName, "remove").Run()
	} else if hasExecutable("chkconfig") {
		exec.Command("chkconfig", "--del", appName).Run()
	}
	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove SysVinit script: %w", err)
	}
	fmt.Printf("%s SysVinit script uninstalled\n", okMark())
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// launchd (macOS)  — label: apimgr.gitignore  (spec Part 4, 5 & 23)
// ─────────────────────────────────────────────────────────────────────────────

func installLaunchd() error {
	binaryPath := GetBinaryPath()

	dataDir := fmt.Sprintf("/Library/Application Support/%s/%s", orgName, appName)
	logDir := fmt.Sprintf("/Library/Logs/%s/%s", orgName, appName)
	dirs := []string{dataDir, logDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create the dedicated macOS service account so the binary can drop
	// privileges to it in-process after binding (see PART 23) — the plist
	// intentionally does NOT hardcode UserName/GroupName.
	if err := ensureMacOSServiceUser(dataDir); err != nil {
		fmt.Printf("%s Could not create service account (continuing): %v\n", warnMark(), err)
	} else {
		exec.Command("chown", "-R", appName+":"+appName, dataDir).Run()
		exec.Command("chown", "-R", appName+":"+appName, logDir).Run()
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>

    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>

    <!-- No UserName/GroupName - starts as root, binary drops to %s user -->

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <key>WorkingDirectory</key>
    <string>%s</string>

    <key>StandardOutPath</key>
    <string>%s/stdout.log</string>

    <key>StandardErrorPath</key>
    <string>%s/stderr.log</string>
</dict>
</plist>
`, launchdLabel, binaryPath, appName, dataDir, logDir, logDir)

	if err := os.WriteFile(launchdPlist, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	fmt.Printf("%s LaunchDaemon installed at: %s\n", okMark(), launchdPlist)
	fmt.Printf("\nTo load the service:\n  sudo launchctl load %s\n", launchdPlist)
	return nil
}

func uninstallLaunchd() error {
	exec.Command("launchctl", "unload", launchdPlist).Run()
	if err := os.Remove(launchdPlist); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}
	fmt.Printf("%s LaunchDaemon uninstalled\n", okMark())
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Windows Service Manager
// ─────────────────────────────────────────────────────────────────────────────

func installWindows() error {
	binaryPath := GetBinaryPath()
	binDir := filepath.Dir(binaryPath)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	// Run as a Virtual Service Account (NT SERVICE\gitignore) rather than
	// defaulting to LocalSystem (see PART 23).
	displayName := strings.Title(appName)
	cmd := exec.Command("sc.exe", "create", appName,
		"binPath=", binaryPath,
		"DisplayName=", displayName+" API",
		"start=", "auto",
		"obj=", `NT SERVICE\`+appName,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create Windows service: %w", err)
	}

	fmt.Printf("%s Windows service '%s' installed (Virtual Service Account)\n", okMark(), appName)
	fmt.Printf("\nTo start the service:\n  sc.exe start %s\n", appName)
	return nil
}

func uninstallWindows() error {
	exec.Command("sc.exe", "stop", appName).Run()
	if err := exec.Command("sc.exe", "delete", appName).Run(); err != nil {
		return fmt.Errorf("failed to delete Windows service: %w", err)
	}
	fmt.Printf("%s Windows service '%s' uninstalled\n", okMark(), appName)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// BSD rc.d
// ─────────────────────────────────────────────────────────────────────────────

func installBSDRC() error {
	binaryPath := GetBinaryPath()
	rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", appName)

	dataDir := fmt.Sprintf("/var/db/%s/%s", orgName, appName)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dataDir, err)
	}

	// Create the service user before writing the rc script (see PART 23)
	if err := ensureBSDServiceUser(dataDir); err != nil {
		fmt.Printf("%s Could not create service user (continuing): %v\n", warnMark(), err)
	} else {
		exec.Command("chown", "-R", appName+":"+appName, dataDir).Run()
	}

	rcContent := fmt.Sprintf(`#!/bin/sh

# PROVIDE: %s
# REQUIRE: NETWORKING
# KEYWORD: shutdown

. /etc/rc.subr

name="%s"
rcvar="%s_enable"
command="%s"
pidfile="/var/run/%s/%s.pid"
command_args=""

load_rc_config $name
: ${%s_enable:="NO"}

run_rc_command "$1"
`, appName, appName, appName, binaryPath, orgName, appName, appName)

	if err := os.WriteFile(rcPath, []byte(rcContent), 0755); err != nil {
		return fmt.Errorf("failed to write rc.d script: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	fmt.Printf("%s BSD rc.d script installed at: %s\n", okMark(), rcPath)
	fmt.Printf("\nAdd '%s_enable=\"YES\"' to /etc/rc.conf\n", appName)
	fmt.Printf("\nTo start the service:\n  service %s start\n", appName)
	return nil
}

func uninstallBSDRC() error {
	exec.Command("service", appName, "stop").Run()
	rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", appName)
	if err := os.Remove(rcPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove rc.d script: %w", err)
	}
	fmt.Printf("%s BSD rc.d script uninstalled\n", okMark())
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Control (start / stop / restart / reload)
// ─────────────────────────────────────────────────────────────────────────────

func Start() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "start", appName).Run()
	case ServiceRunit:
		return exec.Command("sv", "start", appName).Run()
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "start").Run()
	case ServiceSysVinit:
		return exec.Command("/etc/init.d/"+appName, "start").Run()
	case ServiceLaunchd:
		return exec.Command("launchctl", "load", launchdPlist).Run()
	case ServiceWindows:
		return exec.Command("sc.exe", "start", appName).Run()
	case ServiceBSDRC:
		return exec.Command("service", appName, "start").Run()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

func Stop() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "stop", appName).Run()
	case ServiceRunit:
		return exec.Command("sv", "stop", appName).Run()
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "stop").Run()
	case ServiceSysVinit:
		return exec.Command("/etc/init.d/"+appName, "stop").Run()
	case ServiceLaunchd:
		return exec.Command("launchctl", "unload", launchdPlist).Run()
	case ServiceWindows:
		return exec.Command("sc.exe", "stop", appName).Run()
	case ServiceBSDRC:
		return exec.Command("service", appName, "stop").Run()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

func Restart() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "restart", appName).Run()
	case ServiceRunit:
		return exec.Command("sv", "restart", appName).Run()
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "restart").Run()
	case ServiceSysVinit:
		return exec.Command("/etc/init.d/"+appName, "restart").Run()
	case ServiceLaunchd:
		Stop()
		return Start()
	case ServiceWindows:
		exec.Command("sc.exe", "stop", appName).Run()
		return exec.Command("sc.exe", "start", appName).Run()
	case ServiceBSDRC:
		return exec.Command("service", appName, "restart").Run()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

func Reload() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "reload", appName).Run()
	case ServiceRunit:
		return exec.Command("sv", "hup", appName).Run()
	default:
		// OpenRC, SysVinit, launchd, Windows, and BSD rc.d have no generic
		// reload primitive in their standard tooling — fall back to a
		// full restart.
		return Restart()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Status (Part 23 / Part 24 status block)
// ─────────────────────────────────────────────────────────────────────────────

// Status reports whether the service is installed, running, and set to
// auto-start, plus its PID if running — matching the fields printed by
// `--service status` / `--service --help` per AI.md PART 23/24.
func Status() (installed, running, enabled bool, pid int) {
	switch DetectServiceManager() {
	case ServiceSystemd:
		installed = exec.Command("systemctl", "cat", appName).Run() == nil
		running = exec.Command("systemctl", "is-active", "--quiet", appName).Run() == nil
		enabled = exec.Command("systemctl", "is-enabled", "--quiet", appName).Run() == nil
		if running {
			out, _ := exec.Command("systemctl", "show", "-p", "MainPID", "--value", appName).Output()
			pid, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		}

	case ServiceOpenRC:
		scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
		installed = fileExists(scriptPath)
		running = exec.Command(scriptPath, "status").Run() == nil
		out, _ := exec.Command("rc-update", "show", "default").CombinedOutput()
		enabled = strings.Contains(string(out), appName)

	case ServiceSysVinit:
		scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
		installed = fileExists(scriptPath)
		running = exec.Command(scriptPath, "status").Run() == nil
		matches, _ := filepath.Glob("/etc/rc2.d/S*" + appName)
		enabled = len(matches) > 0

	case ServiceRunit:
		svDir := fmt.Sprintf("/etc/sv/%s", appName)
		installed = fileExists(svDir)
		out, err := exec.Command("sv", "status", appName).CombinedOutput()
		running = err == nil && strings.Contains(string(out), "run:")
		enabled = fileExists(fmt.Sprintf("/var/service/%s", appName))

	case ServiceLaunchd:
		installed = fileExists(launchdPlist)
		out, err := exec.Command("launchctl", "list", launchdLabel).CombinedOutput()
		running = err == nil
		enabled = installed
		if running {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "\"PID\"") {
					fields := strings.Fields(line)
					if len(fields) >= 3 {
						pid, _ = strconv.Atoi(strings.TrimRight(fields[2], ";"))
					}
				}
			}
		}

	case ServiceWindows:
		out, err := exec.Command("sc.exe", "query", appName).CombinedOutput()
		installed = err == nil
		running = strings.Contains(string(out), "RUNNING")
		qc, _ := exec.Command("sc.exe", "qc", appName).CombinedOutput()
		enabled = strings.Contains(string(qc), "AUTO_START")

	case ServiceBSDRC:
		rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", appName)
		installed = fileExists(rcPath)
		running = exec.Command("service", appName, "status").Run() == nil
		out, _ := exec.Command("sysrc", "-n", appName+"_enable").CombinedOutput()
		enabled = strings.TrimSpace(string(out)) == "YES"
	}

	return
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func copyBinary(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
