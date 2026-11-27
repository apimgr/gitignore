package service

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/apimgr/gitignore/src/common/display"
	"github.com/apimgr/gitignore/src/common/i18n"
	apppath "github.com/apimgr/gitignore/src/path"
)

const (
	appName = "gitignore"
	orgName = "apimgr"

	// launchd label per spec: {org}.{app}
	launchdLabel = "apimgr.gitignore"
	// launchd plist path per spec
	launchdPlist = "/Library/LaunchDaemons/apimgr.gitignore.plist"
)

var serviceLanguage = i18n.DefaultLang

var serviceCommandRunner = func(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// SetLanguage selects the language used by service management messages.
func SetLanguage(language string) {
	serviceLanguage = language
}

func serviceText(key string, values ...interface{}) string {
	return i18n.TranslateFormat(serviceLanguage, key, values...)
}

func enableAndStartService(manager string, enableCommand, startCommand []string) error {
	if len(enableCommand) > 0 {
		if err := serviceCommandRunner(enableCommand[0], enableCommand[1:]...); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.enable_service_failed", "manager", manager), err)
		}
	}
	if err := serviceCommandRunner(startCommand[0], startCommand[1:]...); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.start_service_failed", "manager", manager), err)
	}
	return nil
}

// ServiceType represents the type of service manager
type ServiceType int

const (
	ServiceUnknown ServiceType = iota
	ServiceSystemd
	ServiceRunit
	ServiceS6
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
	if os.Getenv("NO_COLOR") != "" {
		return "[OK]"
	}
	return "✅"
}

func warnMark() string {
	if os.Getenv("NO_COLOR") != "" {
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
		if _, err := os.Stat("/sbin/openrc-run"); err == nil {
			if _, err := os.Stat("/run/openrc"); err == nil {
				return ServiceOpenRC
			}
		}
		if _, err := os.Stat("/etc/runit"); err == nil || hasExecutable("sv") {
			return ServiceRunit
		}
		if isS6Manager(hasExecutable("s6-svc"), s6SvscanRunning()) {
			return ServiceS6
		}
		// SysVinit is the last Linux fallback after the supported managers.
		if !hasExecutable("systemctl") {
			if _, err := os.Stat("/etc/init.d"); err == nil {
				if hasExecutable("update-rc.d") || hasExecutable("chkconfig") {
					return ServiceSysVinit
				}
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

func isS6Manager(s6SvcAvailable, svscanRunning bool) bool {
	return s6SvcAvailable && svscanRunning
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
	case ServiceS6:
		return installS6()
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
		return fmt.Errorf("%s", serviceText("cli.service.unsupported_manager"))
	}
}

// Uninstall removes the service. Unless force is true, it prompts for
// confirmation before deleting all config/data/cache/log/backup
// directories, the PID file, and the system user/group it created
// (see AI.md PART 23).
func Uninstall(force bool) error {
	if !force {
		if !confirmPrompt(serviceText("cli.service.uninstall_confirm", "app", appName)) {
			fmt.Println(serviceText("cli.service.uninstall_cancelled"))
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
	case ServiceS6:
		err = uninstallS6()
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
		err = fmt.Errorf("%s", serviceText("cli.service.unsupported_manager"))
	}
	if err != nil {
		return err
	}

	removeAllData()
	removeSystemUser()

	fmt.Println(serviceText("cli.service.uninstall_completed", "mark", okMark(), "binary_path", GetBinaryPath()))
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
	configDir, dataDir, logsDir, backupDir := apppath.GetDefaultDirs(appName)
	dirs := []string{configDir, dataDir, apppath.GetCacheDir(), logsDir, backupDir}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			fmt.Println(serviceText("cli.service.remove_path_failed", "mark", warnMark(), "path", dir, "error", err))
		}
	}

	if pidFile := apppath.GetPIDFile(); pidFile != "" {
		if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
			fmt.Println(serviceText("cli.service.remove_pid_failed", "mark", warnMark(), "error", err))
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
			fmt.Println(serviceText("cli.service.remove_user_failed", "mark", warnMark(), "user", appName, "error", err))
		}
		if err := exec.Command("groupdel", appName).Run(); err != nil {
			fmt.Println(serviceText("cli.service.remove_group_failed", "mark", warnMark(), "group", appName, "error", err))
		}
		fmt.Println(serviceText("cli.service.account_removed", "mark", okMark(), "account", appName))

	case "darwin":
		if exec.Command("dscl", ".", "-read", "/Users/"+appName).Run() != nil {
			return
		}
		if err := exec.Command("dscl", ".", "-delete", "/Users/"+appName).Run(); err != nil {
			fmt.Println(serviceText("cli.service.remove_user_failed", "mark", warnMark(), "user", appName, "error", err))
			return
		}
		if err := exec.Command("dscl", ".", "-delete", "/Groups/"+appName).Run(); err != nil {
			fmt.Println(serviceText("cli.service.remove_group_failed", "mark", warnMark(), "group", appName, "error", err))
			return
		}
		fmt.Println(serviceText("cli.service.account_removed", "mark", okMark(), "account", appName))

	case "freebsd", "openbsd", "netbsd":
		if exec.Command("id", appName).Run() != nil {
			return
		}
		if err := exec.Command("pw", "userdel", "-n", appName).Run(); err != nil {
			fmt.Println(serviceText("cli.service.remove_user_failed", "mark", warnMark(), "user", appName, "error", err))
			return
		}
		if err := exec.Command("pw", "groupdel", "-n", appName).Run(); err != nil {
			fmt.Println(serviceText("cli.service.remove_group_failed", "mark", warnMark(), "group", appName, "error", err))
			return
		}
		fmt.Println(serviceText("cli.service.account_removed", "mark", okMark(), "account", appName))
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
	EscalateNone      EscalationMethod = ""
	EscalateSudo      EscalationMethod = "sudo"
	EscalateSu        EscalationMethod = "su"
	EscalatePkexec    EscalationMethod = "pkexec"
	EscalateDoas      EscalationMethod = "doas"
	EscalateRunas     EscalationMethod = "runas"
	EscalateOSAScript EscalationMethod = "osascript"
	EscalateUAC       EscalationMethod = "uac"
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

// DetectEscalation finds the first usable escalation method in the platform
// order specified by AI.md PART 23.
func DetectEscalation() EscalationMethod {
	if IsElevated() {
		return EscalateNone
	}

	if runtime.GOOS == "windows" {
		if display.DetectDisplayEnv().HasDisplay && hasExecutable("powershell") {
			return EscalateUAC
		}
		if hasExecutable("runas") {
			return EscalateRunas
		}
		return EscalateNone
	}

	sudoAvailable := func() bool {
		return hasExecutable("sudo") && (exec.Command("sudo", "-n", "true").Run() == nil || canSudoInteractively())
	}
	switch runtime.GOOS {
	case "darwin":
		if sudoAvailable() {
			return EscalateSudo
		}
		if hasExecutable("osascript") && display.DetectDisplayEnv().HasDisplay {
			return EscalateOSAScript
		}
	case "freebsd", "openbsd", "netbsd":
		if hasExecutable("doas") {
			return EscalateDoas
		}
		if sudoAvailable() {
			return EscalateSudo
		}
		if hasExecutable("su") {
			return EscalateSu
		}
	default:
		if sudoAvailable() {
			return EscalateSudo
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
		return fmt.Errorf("%s", serviceText("cli.service.reexecute_arguments_missing"))
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
	case EscalateOSAScript:
		command := shellJoin(args)
		command = strings.ReplaceAll(strings.ReplaceAll(command, `\`, `\\`), `"`, `\"`)
		cmd = exec.Command("osascript", "-e", `do shell script "`+command+`" with administrator privileges`)
	case EscalateUAC:
		return execElevatedUAC(args)
	default:
		return fmt.Errorf("%s", serviceText("cli.service.escalation_method_unavailable"))
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

func execElevatedUAC(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", serviceText("cli.service.reexecute_arguments_missing"))
	}
	executable := base64.StdEncoding.EncodeToString([]byte(args[0]))
	arguments := base64.StdEncoding.EncodeToString([]byte(windowsCommandLine(args[1:])))
	script := "$exe=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" + executable + "')); " +
		"$line=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" + arguments + "')); " +
		"$p=Start-Process -FilePath $exe -ArgumentList $line -Verb RunAs -Wait -PassThru; exit $p.ExitCode"
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func windowsCommandLine(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		var out strings.Builder
		out.WriteByte('"')
		backslashes := 0
		for _, char := range arg {
			if char == '\\' {
				backslashes++
				continue
			}
			if char == '"' {
				out.WriteString(strings.Repeat("\\", backslashes*2+1))
				out.WriteRune(char)
				backslashes = 0
				continue
			}
			out.WriteString(strings.Repeat("\\", backslashes))
			backslashes = 0
			out.WriteRune(char)
		}
		out.WriteString(strings.Repeat("\\", backslashes*2))
		out.WriteByte('"')
		quoted[index] = out.String()
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
		return fmt.Errorf("%s", serviceText("cli.service.user_install_unsupported", "platform", runtime.GOOS))
	}
}

func installSystemdUser() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.resolve_binary_failed"), err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.resolve_home_failed"), err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_path_failed", "path", unitDir), err)
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
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_unit_failed"), err)
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.reload_user_systemd_failed"), err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", appName).Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.enable_user_service_failed"), err)
	}

	fmt.Println(serviceText("cli.service.user_service_installed", "mark", okMark(), "manager", "systemd", "path", unitPath))
	return nil
}

func installLaunchdUser() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.resolve_binary_failed"), err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.resolve_home_failed"), err)
	}
	agentDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_path_failed", "path", agentDir), err)
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
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_plist_failed"), err)
	}

	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.load_user_agent_failed"), err)
	}

	fmt.Println(serviceText("cli.service.user_service_installed", "mark", okMark(), "manager", "launchd", "path", plistPath))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// User creation (Part 4 / Part 23)
// ─────────────────────────────────────────────────────────────────────────────

// EnsureSystemUser creates the dedicated service account when root startup needs
// one. The optional home path is the resolved data directory.
func EnsureSystemUser(homeDirs ...string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	homeDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if len(homeDirs) > 0 && strings.TrimSpace(homeDirs[0]) != "" {
		homeDir = strings.TrimSpace(homeDirs[0])
	}
	if homeDir == "" {
		_, homeDir, _, _ = apppath.GetDefaultDirs(appName)
	}
	switch runtime.GOOS {
	case "linux":
		return ensureLinuxServiceUser(homeDir)
	case "darwin":
		return ensureMacOSServiceUser(homeDir)
	case "freebsd", "openbsd", "netbsd":
		return ensureBSDServiceUser(homeDir)
	default:
		return nil
	}
}

func ensureLinuxServiceUser(homeDir string) error {
	serviceUser, userErr := user.Lookup(appName)
	serviceGroup, groupErr := user.LookupGroup(appName)
	if userErr == nil && groupErr == nil {
		uid, uidErr := strconv.Atoi(serviceUser.Uid)
		gid, gidErr := strconv.Atoi(serviceGroup.Gid)
		userGID, userGIDErr := strconv.Atoi(serviceUser.Gid)
		if uidErr != nil || gidErr != nil || userGIDErr != nil || uid != gid || uid != userGID {
			return fmt.Errorf("service account %s does not use a matching UID and GID", appName)
		}
		return nil
	}

	id := 0
	if userErr == nil {
		uid, uidErr := strconv.Atoi(serviceUser.Uid)
		userGID, gidErr := strconv.Atoi(serviceUser.Gid)
		if uidErr != nil || gidErr != nil || uid != userGID || uid <= 0 {
			return fmt.Errorf("service account %s does not use a matching UID and GID", appName)
		}
		id = uid
	}
	if groupErr == nil {
		gid, gidErr := strconv.Atoi(serviceGroup.Gid)
		if gidErr != nil {
			return fmt.Errorf("invalid service group ID for %s: %w", appName, gidErr)
		}
		if userErr != nil {
			if _, lookupErr := user.LookupId(strconv.Itoa(gid)); lookupErr == nil {
				return fmt.Errorf("service group %s has a GID that is already assigned to another user", appName)
			}
			id = gid
		} else if id != gid {
			return fmt.Errorf("service account %s does not use a matching UID and GID", appName)
		}
	}
	if id == 0 {
		var err error
		id, err = findAvailableSystemID()
		if err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.no_available_account_id", "range", "200-899"), err)
		}
	}
	if groupErr != nil {
		groupArgs := []string{"--system", "--gid", strconv.Itoa(id), appName}
		groupTool := "groupadd"
		if _, err := exec.LookPath(groupTool); err != nil {
			groupTool = "addgroup"
			groupArgs = []string{"-S", "-g", strconv.Itoa(id), appName}
		}
		if err := exec.Command(groupTool, groupArgs...).Run(); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.create_group_failed", "group", appName), err)
		}
	}
	if userErr != nil {
		userTool := "useradd"
		userArgs := []string{"--system", "--uid", strconv.Itoa(id), "--gid", strconv.Itoa(id), "--home-dir", homeDir, "--no-create-home", "--shell", "/sbin/nologin", "--comment", appName + " service account", appName}
		if _, err := exec.LookPath(userTool); err != nil {
			userTool = "adduser"
			userArgs = []string{"-S", "-D", "-H", "-u", strconv.Itoa(id), "-G", appName, "-h", homeDir, "-s", "/sbin/nologin", "-g", appName + " service account", appName}
		}
		if err := exec.Command(userTool, userArgs...).Run(); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.create_user_failed", "user", appName), err)
		}
	}
	fmt.Println(serviceText("cli.service.account_created", "mark", okMark(), "account", appName, "uid", id, "gid", id))
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
		return fmt.Errorf("%s: %w", serviceText("cli.service.no_available_account_id", "range", "200-399"), err)
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
			return fmt.Errorf("%s: %w", serviceText("cli.service.command_failed", "command", strings.Join(c, " ")), err)
		}
	}

	fmt.Println(serviceText("cli.service.account_created", "mark", okMark(), "account", appName, "uid", id, "gid", id))
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
	return 0, fmt.Errorf("%s", serviceText("cli.service.no_available_account_id", "range", "200-399"))
}

// ensureBSDServiceUser creates the FreeBSD/OpenBSD/NetBSD service user via
// pw, if it doesn't already exist.
func ensureBSDServiceUser(homeDir string) error {
	if exec.Command("id", appName).Run() == nil {
		return nil // already exists
	}

	id, err := findAvailableSystemID()
	if err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.no_available_account_id", "range", "200-899"), err)
	}

	if err := exec.Command("pw", "groupadd", "-n", appName, "-g", strconv.Itoa(id)).Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_group_failed", "group", appName), err)
	}
	if err := exec.Command("pw", "useradd",
		"-n", appName,
		"-u", strconv.Itoa(id),
		"-g", strconv.Itoa(id),
		"-d", homeDir,
		"-s", "/usr/sbin/nologin",
		"-c", appName+" service account",
	).Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_user_failed", "user", appName), err)
	}

	fmt.Println(serviceText("cli.service.account_created", "mark", okMark(), "account", appName, "uid", id, "gid", id))
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
		if _, err := user.LookupId(strconv.Itoa(id)); err == nil {
			continue
		}
		if _, err := user.LookupGroupId(strconv.Itoa(id)); err == nil {
			continue
		}
		return id, nil
	}
	return 0, fmt.Errorf("%s", serviceText("cli.service.no_available_account_id", "range", "200-899"))
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
			return fmt.Errorf("%s: %w", serviceText("cli.service.create_path_failed", "path", dir), err)
		}
	}

	// Ensure service user exists now that its home directory is in place
	if err := EnsureSystemUser(); err != nil {
		return err
	}

	for _, dir := range dirs {
		// Set ownership if user exists
		_ = exec.Command("chown", appName+":"+appName, dir).Run()
	}

	serviceContent := systemdServiceContent(binaryPath)

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", appName)

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_service_file_failed"), err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.reload_systemd_failed"), err)
	}
	if err := enableAndStartService("systemd", []string{"systemctl", "enable", appName}, []string{"systemctl", "start", appName}); err != nil {
		return err
	}

	fmt.Println(serviceText("cli.service.service_installed", "mark", okMark(), "manager", "systemd", "path", servicePath))
	fmt.Println(serviceText("cli.service.binary_installed", "mark", okMark(), "path", binaryPath))
	return nil
}

func systemdServiceContent(binaryPath string) string {
	return fmt.Sprintf(`[Unit]
Description=%s service
Documentation=https://%s.github.io/%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=65535
StandardOutput=journal
StandardError=journal

# Security hardening; the server drops privileges after binding.
NoNewPrivileges=true
ProtectSystem=true
ProtectHome=yes
PrivateTmp=true
# Create the writable runtime directories before applying the filesystem sandbox.
ExecStartPre=+/usr/bin/install -d -m 0755 /etc/%s/%s /var/lib/%s/%s /var/cache/%s/%s /var/log/%s/%s /var/run/%s/%s
ReadWritePaths=-/etc/%s/%s
ReadWritePaths=-/var/lib/%s/%s
ReadWritePaths=-/var/cache/%s/%s
ReadWritePaths=-/var/log/%s/%s
ReadWritePaths=-/var/run/%s/%s
ReadWritePaths=-/mnt/Backups/%s/%s
ReadWritePaths=-/usr/local/bin

[Install]
WantedBy=multi-user.target
`, appName, orgName, appName, binaryPath,
		orgName, appName, orgName, appName, orgName, appName, orgName, appName, orgName, appName,
		orgName, appName, orgName, appName, orgName, appName, orgName, appName, orgName, appName, orgName, appName)
}

func uninstallSystemd() error {
	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", appName)
	_ = exec.Command("systemctl", "stop", appName).Run()
	_ = exec.Command("systemctl", "disable", appName).Run()
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_service_file_failed"), err)
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	fmt.Println(serviceText("cli.service.service_removed", "mark", okMark(), "manager", "systemd", "path", servicePath))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// runit
// ─────────────────────────────────────────────────────────────────────────────

func installRunit() error {
	svDir := fmt.Sprintf("/etc/sv/%s", appName)
	binaryPath := GetBinaryPath()

	if err := os.MkdirAll(svDir, 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_service_directory_failed", "path", svDir), err)
	}

	runScript := fmt.Sprintf("#!/bin/sh\nexec %s 2>&1\n", binaryPath)
	if err := os.WriteFile(filepath.Join(svDir, "run"), []byte(runScript), 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_run_script_failed"), err)
	}

	logDir := filepath.Join(svDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_log_directory_failed", "path", logDir), err)
	}
	logRun := "#!/bin/sh\nexec svlogd -tt ./main\n"
	if err := os.WriteFile(filepath.Join(logDir, "run"), []byte(logRun), 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_log_script_failed"), err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}

	if err := os.Symlink(svDir, fmt.Sprintf("/var/service/%s", appName)); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.link_service_failed"), err)
	}
	if err := enableAndStartService("runit", nil, []string{"sv", "up", appName}); err != nil {
		return err
	}
	fmt.Println(serviceText("cli.service.service_installed", "mark", okMark(), "manager", "runit", "path", svDir))
	return nil
}

func installS6() error {
	serviceDir := filepath.Join("/etc/s6/sv", appName)
	logDir := filepath.Join(serviceDir, "log")
	scanDir := s6ScanDirectory()
	binaryPath := GetBinaryPath()
	if err := EnsureSystemUser(); err != nil {
		return err
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_path_failed", "path", logDir), err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "run"), []byte(fmt.Sprintf("#!/bin/sh\nexec %s 2>&1\n", binaryPath)), 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_run_script_failed"), err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "run"), []byte(fmt.Sprintf("#!/bin/sh\nexec s6-log n10 s1000000 /var/log/%s/%s\n", orgName, appName)), 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_log_script_failed"), err)
	}
	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}
	linkPath := filepath.Join(scanDir, appName)
	if err := os.Symlink(serviceDir, linkPath); err != nil && !os.IsExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.link_service_failed"), err)
	}
	if err := enableAndStartService("s6", []string{"s6-svscanctl", "-a", scanDir}, []string{"s6-svc", "-u", linkPath}); err != nil {
		return err
	}
	fmt.Println(serviceText("cli.service.service_installed", "mark", okMark(), "manager", "s6", "path", serviceDir))
	return nil
}

func uninstallS6() error {
	scanDir := s6ScanDirectory()
	linkPath := filepath.Join(scanDir, appName)
	_ = serviceCommandRunner("s6-svc", "-d", linkPath)
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_service_link_failed"), err)
	}
	if err := serviceCommandRunner("s6-svscanctl", "-a", scanDir); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.command_failed", "command", "s6-svscanctl -a "+scanDir), err)
	}
	if err := os.RemoveAll(filepath.Join("/etc/s6/sv", appName)); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_service_directory_failed"), err)
	}
	fmt.Println(serviceText("cli.service.service_uninstalled", "mark", okMark(), "manager", "s6"))
	return nil
}

func s6SvscanRunning() bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == "s6-svscan" {
			return true
		}
	}
	return false
}

func s6ScanDirectory() string {
	entries, err := os.ReadDir("/proc")
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, err := strconv.Atoi(entry.Name()); err != nil {
				continue
			}
			procDir := filepath.Join("/proc", entry.Name())
			comm, err := os.ReadFile(filepath.Join(procDir, "comm"))
			if err != nil || strings.TrimSpace(string(comm)) != "s6-svscan" {
				continue
			}
			cmdline, err := os.ReadFile(filepath.Join(procDir, "cmdline"))
			if err != nil {
				continue
			}
			if scanDir := s6ScanDirectoryFromCmdline(cmdline); scanDir != "" {
				return scanDir
			}
		}
	}
	return "/run/service"
}

func s6ScanDirectoryFromCmdline(cmdline []byte) string {
	args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	if len(args) > 1 && args[1] != "" {
		return args[1]
	}
	return ""
}

func uninstallRunit() error {
	_ = exec.Command("sv", "stop", appName).Run()
	if err := os.Remove(fmt.Sprintf("/var/service/%s", appName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_service_link_failed"), err)
	}
	if err := os.RemoveAll(fmt.Sprintf("/etc/sv/%s", appName)); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_service_directory_failed"), err)
	}
	fmt.Println(serviceText("cli.service.service_uninstalled", "mark", okMark(), "manager", "runit"))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// OpenRC
// ─────────────────────────────────────────────────────────────────────────────

func installOpenRC() error {
	binaryPath := GetBinaryPath()

	if err := EnsureSystemUser(); err != nil {
		return err
	}

	scriptContent := fmt.Sprintf(`#!/sbin/openrc-run

name="%s"
description="gitignore API Server"
command="%s"
command_args=""
pidfile="/var/run/%s-%s.pid"
command_background=true
output_log="/var/log/%s/%s/server.log"
error_log="/var/log/%s/%s/error.log"

depend() {
    need net
    after firewall
    use dns logger
}

start_pre() {
    checkpath -d -m 0755 /var/run/%s
    checkpath -d -m 0755 /var/log/%s
    checkpath -d -m 0755 -o %s:%s /var/run/%s/%s
    checkpath -d -m 0755 -o %s:%s /var/log/%s/%s
}
`, appName, binaryPath, orgName, appName,
		orgName, appName,
		orgName, appName,
		orgName, orgName,
		appName, appName, orgName, appName,
		appName, appName, orgName, appName)

	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_openrc_script_failed"), err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}

	if err := enableAndStartService("OpenRC", []string{"rc-update", "add", appName, "default"}, []string{"rc-service", appName, "start"}); err != nil {
		return err
	}

	fmt.Println(serviceText("cli.service.service_installed", "mark", okMark(), "manager", "OpenRC", "path", scriptPath))
	return nil
}

func uninstallOpenRC() error {
	_ = exec.Command("rc-service", appName, "stop").Run()
	_ = exec.Command("rc-update", "del", appName, "default").Run()
	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_openrc_script_failed"), err)
	}
	fmt.Println(serviceText("cli.service.service_uninstalled", "mark", okMark(), "manager", "OpenRC"))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// SysVinit
// ─────────────────────────────────────────────────────────────────────────────

func installSysVinit() error {
	binaryPath := GetBinaryPath()

	if err := EnsureSystemUser(); err != nil {
		return err
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
PIDFILE=/var/run/%s-%s.pid
LOGFILE=/var/log/%s/%s/server.log

case "$1" in
    start)
        echo "Starting $NAME..."
        mkdir -p $(dirname $LOGFILE)
        chown -R $DAEMON_USER:$DAEMON_USER $(dirname $LOGFILE)
        start-stop-daemon --start --quiet --background --make-pidfile \
            --pidfile $PIDFILE --exec $DAEMON \
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
        echo "Usage: $0 {start|stop|restart|status}" >&2
        exit 2
        ;;
esac
exit 0
`, appName, appName, binaryPath, appName, orgName, appName, orgName, appName)

	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_sysv_script_failed"), err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}

	if hasExecutable("update-rc.d") {
		if err := serviceCommandRunner("update-rc.d", appName, "defaults"); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.enable_service_failed", "manager", "SysVinit"), err)
		}
	} else if hasExecutable("chkconfig") {
		if err := serviceCommandRunner("chkconfig", "--add", appName); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.enable_service_failed", "manager", "SysVinit"), err)
		}
		if err := serviceCommandRunner("chkconfig", appName, "on"); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.enable_service_failed", "manager", "SysVinit"), err)
		}
	} else {
		return fmt.Errorf("%s", serviceText("cli.service.enable_service_failed", "manager", "SysVinit"))
	}
	if err := enableAndStartService("SysVinit", nil, []string{scriptPath, "start"}); err != nil {
		return err
	}

	fmt.Println(serviceText("cli.service.service_installed", "mark", okMark(), "manager", "SysVinit", "path", scriptPath))
	return nil
}

func uninstallSysVinit() error {
	_ = exec.Command("/etc/init.d/"+appName, "stop").Run()
	if hasExecutable("update-rc.d") {
		_ = exec.Command("update-rc.d", "-f", appName, "remove").Run()
	} else if hasExecutable("chkconfig") {
		_ = exec.Command("chkconfig", "--del", appName).Run()
	}
	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_sysv_script_failed"), err)
	}
	fmt.Println(serviceText("cli.service.service_uninstalled", "mark", okMark(), "manager", "SysVinit"))
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
			return fmt.Errorf("%s: %w", serviceText("cli.service.create_path_failed", "path", dir), err)
		}
	}

	// Create the dedicated macOS service account so the binary can drop
	// privileges to it in-process after binding (see PART 23) — the plist
	// intentionally does NOT hardcode UserName/GroupName.
	if err := ensureMacOSServiceUser(dataDir); err != nil {
		return err
	} else {
		_ = exec.Command("chown", "-R", appName+":"+appName, dataDir).Run()
		_ = exec.Command("chown", "-R", appName+":"+appName, logDir).Run()
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
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_plist_failed"), err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}

	if err := enableAndStartService("launchd", nil, []string{"launchctl", "load", launchdPlist}); err != nil {
		return err
	}
	fmt.Println(serviceText("cli.service.service_installed", "mark", okMark(), "manager", "launchd", "path", launchdPlist))
	return nil
}

func uninstallLaunchd() error {
	_ = exec.Command("launchctl", "unload", launchdPlist).Run()
	if err := os.Remove(launchdPlist); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_plist_failed"), err)
	}
	fmt.Println(serviceText("cli.service.service_uninstalled", "mark", okMark(), "manager", "launchd"))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Windows Service Manager
// ─────────────────────────────────────────────────────────────────────────────

func installWindows() error {
	binaryPath := GetBinaryPath()
	binDir := filepath.Dir(binaryPath)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_path_failed", "path", binDir), err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}

	// Run as a Virtual Service Account (NT SERVICE\gitignore) rather than
	// defaulting to LocalSystem (see PART 23).
	displayName := strings.ToUpper(appName[:1]) + appName[1:]
	cmd := exec.Command("sc.exe", "create", appName,
		"binPath=", binaryPath,
		"DisplayName=", displayName+" API",
		"start=", "auto",
		"obj=", `NT SERVICE\`+appName,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_windows_service_failed"), err)
	}

	if err := enableAndStartService("Windows Service", nil, []string{"sc.exe", "start", appName}); err != nil {
		return err
	}
	fmt.Println(serviceText("cli.service.windows_service_installed", "mark", okMark(), "service", appName))
	return nil
}

func uninstallWindows() error {
	_ = exec.Command("sc.exe", "stop", appName).Run()
	if err := exec.Command("sc.exe", "delete", appName).Run(); err != nil {
		return fmt.Errorf("%s: %w", serviceText("cli.service.delete_windows_service_failed"), err)
	}
	fmt.Println(serviceText("cli.service.windows_service_uninstalled", "mark", okMark(), "service", appName))
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
		return fmt.Errorf("%s: %w", serviceText("cli.service.create_path_failed", "path", dataDir), err)
	}

	// Create the service user before writing the rc script (see PART 23)
	if err := ensureBSDServiceUser(dataDir); err != nil {
		return err
	} else {
		_ = exec.Command("chown", "-R", appName+":"+appName, dataDir).Run()
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
		return fmt.Errorf("%s: %w", serviceText("cli.service.write_bsd_script_failed"), err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("%s: %w", serviceText("cli.service.copy_binary_failed"), err)
		}
	}

	if err := enableAndStartService("BSD rc.d", []string{"sysrc", appName + "_enable=YES"}, []string{"service", appName, "start"}); err != nil {
		return err
	}
	fmt.Println(serviceText("cli.service.service_installed", "mark", okMark(), "manager", "BSD rc.d", "path", rcPath))
	return nil
}

func uninstallBSDRC() error {
	_ = exec.Command("service", appName, "stop").Run()
	rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", appName)
	if err := os.Remove(rcPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", serviceText("cli.service.remove_bsd_script_failed"), err)
	}
	fmt.Println(serviceText("cli.service.service_uninstalled", "mark", okMark(), "manager", "BSD rc.d"))
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
	case ServiceS6:
		return exec.Command("s6-svc", "-u", filepath.Join(s6ScanDirectory(), appName)).Run()
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
		return fmt.Errorf("%s", serviceText("cli.service.unsupported_manager"))
	}
}

func Stop() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "stop", appName).Run()
	case ServiceRunit:
		return exec.Command("sv", "stop", appName).Run()
	case ServiceS6:
		return exec.Command("s6-svc", "-d", filepath.Join(s6ScanDirectory(), appName)).Run()
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
		return fmt.Errorf("%s", serviceText("cli.service.unsupported_manager"))
	}
}

// Disable stops the service and removes it from automatic startup while
// leaving its service definition and application data in place.
func Disable() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "disable", "--now", appName).Run()
	case ServiceOpenRC:
		_ = exec.Command("rc-service", appName, "stop").Run()
		return exec.Command("rc-update", "del", appName, "default").Run()
	case ServiceRunit:
		_ = exec.Command("sv", "down", appName).Run()
		return os.Remove(filepath.Join("/var/service", appName))
	case ServiceS6:
		linkPath := filepath.Join(s6ScanDirectory(), appName)
		_ = exec.Command("s6-svc", "-d", linkPath).Run()
		return os.Remove(linkPath)
	case ServiceSysVinit:
		_ = exec.Command(filepath.Join("/etc/init.d", appName), "stop").Run()
		if hasExecutable("update-rc.d") {
			return exec.Command("update-rc.d", "-f", appName, "remove").Run()
		}
		return exec.Command("chkconfig", appName, "off").Run()
	case ServiceLaunchd:
		return exec.Command("launchctl", "unload", launchdPlist).Run()
	case ServiceWindows:
		_ = exec.Command("sc.exe", "stop", appName).Run()
		return exec.Command("sc.exe", "config", appName, "start=", "disabled").Run()
	case ServiceBSDRC:
		_ = exec.Command("service", appName, "stop").Run()
		return exec.Command("sysrc", appName+"_enable=NO").Run()
	default:
		return fmt.Errorf("%s", serviceText("cli.service.unsupported_manager"))
	}
}

func Restart() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "restart", appName).Run()
	case ServiceRunit:
		return exec.Command("sv", "restart", appName).Run()
	case ServiceS6:
		return exec.Command("s6-svc", "-r", filepath.Join(s6ScanDirectory(), appName)).Run()
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "restart").Run()
	case ServiceSysVinit:
		return exec.Command("/etc/init.d/"+appName, "restart").Run()
	case ServiceLaunchd:
		if err := Stop(); err != nil {
			return err
		}
		return Start()
	case ServiceWindows:
		if err := exec.Command("sc.exe", "stop", appName).Run(); err != nil {
			return err
		}
		return exec.Command("sc.exe", "start", appName).Run()
	case ServiceBSDRC:
		return exec.Command("service", appName, "restart").Run()
	default:
		return fmt.Errorf("%s", serviceText("cli.service.unsupported_manager"))
	}
}

func Reload() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", "reload", appName).Run()
	case ServiceRunit:
		return exec.Command("sv", "hup", appName).Run()
	case ServiceS6:
		return exec.Command("s6-svc", "-h", filepath.Join(s6ScanDirectory(), appName)).Run()
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

	case ServiceS6:
		serviceDir := filepath.Join("/etc/s6/sv", appName)
		linkPath := filepath.Join(s6ScanDirectory(), appName)
		installed = fileExists(serviceDir)
		enabled = fileExists(linkPath)
		out, err := exec.Command("s6-svstat", linkPath).CombinedOutput()
		running = err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "up ")

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
