package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
		if _, err := os.Stat("/etc/systemd"); err == nil {
			return ServiceSystemd
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

// Install installs the service for the detected service manager
func Install() error {
	serviceType := DetectServiceManager()

	switch serviceType {
	case ServiceSystemd:
		return installSystemd()
	case ServiceRunit:
		return installRunit()
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

// Uninstall removes the service
func Uninstall() error {
	serviceType := DetectServiceManager()

	switch serviceType {
	case ServiceSystemd:
		return uninstallSystemd()
	case ServiceRunit:
		return uninstallRunit()
	case ServiceLaunchd:
		return uninstallLaunchd()
	case ServiceWindows:
		return uninstallWindows()
	case ServiceBSDRC:
		return uninstallBSDRC()
	default:
		return fmt.Errorf("unsupported service manager")
	}
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
// User creation (Part 4)
// ─────────────────────────────────────────────────────────────────────────────

// EnsureSystemUser creates the system user/group if they don't already exist.
// Only supported on Linux; no-op on other platforms.
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

	// Ensure service user exists before installing
	if err := EnsureSystemUser(); err != nil {
		fmt.Printf("%s Could not create system user (continuing): %v\n", warnMark(), err)
	}

	// Create required directories
	dirs := []string{
		fmt.Sprintf("/var/lib/%s/%s", orgName, appName),
		fmt.Sprintf("/var/log/%s/%s", orgName, appName),
		fmt.Sprintf("/etc/%s/%s", orgName, appName),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
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
// launchd (macOS)  — label: apimgr.gitignore  (spec Part 4 & 5)
// ─────────────────────────────────────────────────────────────────────────────

func installLaunchd() error {
	binaryPath := GetBinaryPath()

	dirs := []string{
		fmt.Sprintf("/Library/Application Support/%s/%s", orgName, appName),
		fmt.Sprintf("/Library/Logs/%s/%s", orgName, appName),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
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

    <!-- Run as dedicated service user, NOT root -->
    <key>UserName</key>
    <string>%s</string>

    <key>GroupName</key>
    <string>%s</string>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <key>WorkingDirectory</key>
    <string>/Library/Application Support/%s/%s</string>

    <key>StandardOutPath</key>
    <string>/Library/Logs/%s/%s/stdout.log</string>

    <key>StandardErrorPath</key>
    <string>/Library/Logs/%s/%s/stderr.log</string>
</dict>
</plist>
`, launchdLabel, binaryPath,
		appName, appName,
		orgName, appName,
		orgName, appName,
		orgName, appName)

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

	// Use Virtual Service Account (NT SERVICE\gitignore) — empty ServiceStartName
	displayName := strings.Title(appName)
	cmd := exec.Command("sc.exe", "create", appName,
		"binPath=", binaryPath,
		"DisplayName=", displayName+" API",
		"start=", "auto",
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create Windows service: %w", err)
	}

	fmt.Printf("%s Windows service '%s' installed\n", okMark(), appName)
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
		return Restart()
	}
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
