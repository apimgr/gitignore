package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	displayformat "github.com/apimgr/gitignore/src/common/format"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/email"
	"github.com/apimgr/gitignore/src/server"
	"github.com/apimgr/gitignore/src/tor"
)

// detectGatewayIP returns the default IPv4 gateway from /proc/net/route, used as
// an SMTP auto-detection candidate (AI.md PART 17). Returns "" when unavailable.
func detectGatewayIP() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// Default route: destination 00000000, flags include RTF_GATEWAY (0x2).
		if fields[1] != "00000000" {
			continue
		}
		gw, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil || gw == 0 {
			continue
		}
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(gw))
		return net.IP(b).String()
	}
	return ""
}

// detectGlobalIPv4 returns the host's outbound-facing IPv4 address by inspecting
// the local end of a UDP socket to a public address. No packets are sent. Returns
// "" when the address is loopback/private-only or undiscoverable.
func detectGlobalIPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()
	if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok && ua.IP.To4() != nil && !ua.IP.IsLoopback() {
		return ua.IP.String()
	}
	return ""
}

// notifier is the process-wide operator email notifier. It is nil until
// initEmail runs during server startup, and it stays nil on CLI-only paths so
// notification emit helpers safely no-op there.
var notifier *emailNotifier

// emailNotifier bundles the SMTP sender, the template renderer, and the context
// needed to build template variables. A nil sender (or CanSend()==false) means
// email is disabled and every emit is a no-op (AI.md PART 17: no SMTP, no email).
type emailNotifier struct {
	sender    *email.Sender
	renderer  *email.Renderer
	cfg       *config.Config
	dataDir   string
	recipient string
	i2pStatus func() server.I2PInfo
}

// buildSender constructs an email.Sender from config, applying the AI.md PART 17
// sender defaults: From Name defaults to the app title, From Address to
// no-reply@{fqdn}.
func buildSender(cfg *config.Config) *email.Sender {
	e := cfg.Server.Notifications.Email
	fromName := e.From.Name
	if fromName == "" {
		fromName = cfg.Server.Branding.Title
	}
	if fromName == "" {
		fromName = projectName
	}
	fromEmail := e.From.Email
	if fromEmail == "" {
		fqdn := cfg.Server.FQDN
		if fqdn == "" {
			fqdn = "localhost"
		}
		fromEmail = "no-reply@" + fqdn
	}
	return &email.Sender{
		Host:      e.SMTP.Host,
		Port:      e.SMTP.Port,
		Username:  e.SMTP.Username,
		Password:  e.SMTP.Password,
		TLSMode:   e.SMTP.TLS,
		FromName:  fromName,
		FromEmail: fromEmail,
		ReplyTo:   e.ReplyTo,
	}
}

// templateDir returns the on-disk custom template override directory
// ({config_dir}/template/email/) per AI.md PART 17.
func templateDir(configDir string) string {
	return filepath.Join(configDir, "template", "email")
}

func emailText(lang, key string, values ...interface{}) string {
	return i18n.TranslateFormat(lang, "cli.email."+key, values...)
}

// notifyRecipient resolves the operator recipient address: the configured admin
// email, then the Reply-To address, then the From address.
func notifyRecipient(cfg *config.Config, sender *email.Sender) string {
	if cfg.Server.Admin.Email != "" {
		return cfg.Server.Admin.Email
	}
	if cfg.Server.Notifications.Email.ReplyTo != "" {
		return cfg.Server.Notifications.Email.ReplyTo
	}
	return sender.FromEmail
}

// initEmail wires up the operator email subsystem (AI.md PART 17). It applies
// SMTP_* env overrides, either auto-detects a local SMTP server (when no host is
// configured) or tests the configured connection, sets the runtime Enabled flag,
// persists a newly detected host, and logs email.configured=true/false. It never
// fails the server: no working SMTP simply means email stays disabled.
func initEmail(cfg *config.Config, configDir, dataDir string, i2pStatus func() server.I2PInfo) {
	config.ApplySMTPEnv(cfg)
	e := &cfg.Server.Notifications.Email

	if strings.TrimSpace(e.SMTP.Host) == "" {
		// Auto-detect a local SMTP server.
		cands := email.DefaultCandidates(detectGatewayIP(), cfg.Server.FQDN, detectGlobalIPv4())
		if host, port, ok := email.Detect(cands); ok {
			e.SMTP.Host = host
			e.SMTP.Port = port
			e.Enabled = true
			fmt.Println(emailText(cfg.Server.I18n.DefaultLanguage, "smtp_detected", "host", host, "port", e.SMTP.Port))
			// Persist the detected host so future startups skip detection.
			if err := config.Update(func(c *config.Config) {
				c.Server.Notifications.Email.SMTP.Host = host
				c.Server.Notifications.Email.SMTP.Port = port
			}); err != nil {
				fmt.Println(emailText(cfg.Server.I18n.DefaultLanguage, "smtp_persist_failed", "error", err))
			}
		} else {
			e.Enabled = false
			fmt.Println(emailText(cfg.Server.I18n.DefaultLanguage, "smtp_not_detected"))
		}
	} else {
		// Test the configured connection.
		if err := email.TestConnection(e.SMTP.Host, e.SMTP.Port, e.SMTP.TLS); err != nil {
			e.Enabled = false
			fmt.Println(emailText(cfg.Server.I18n.DefaultLanguage, "smtp_connection_failed", "host", e.SMTP.Host, "port", e.SMTP.Port, "error", err))
		} else {
			e.Enabled = true
			fmt.Println(emailText(cfg.Server.I18n.DefaultLanguage, "smtp_connected", "host", e.SMTP.Host, "port", e.SMTP.Port))
		}
	}

	sender := buildSender(cfg)
	if e.Enabled && sender.CanSend() {
		notifier = &emailNotifier{
			sender:    sender,
			renderer:  email.NewRenderer(templateDir(configDir)),
			cfg:       cfg,
			dataDir:   dataDir,
			recipient: notifyRecipient(cfg, sender),
			i2pStatus: i2pStatus,
		}
		fmt.Println("email.configured=true")
	} else {
		notifier = nil
		fmt.Println("email.configured=false")
	}
}

// globals returns the global template variables available in every template
// (AI.md PART 17 "Global Variables").
func (n *emailNotifier) globals() map[string]string {
	cfg := n.cfg
	fqdn := cfg.Server.FQDN
	scheme := "http"
	if cfg.Server.SSL.Enabled {
		scheme = "https"
	}
	appURL := ""
	if fqdn != "" {
		appURL = scheme + "://" + fqdn
	}
	appName := cfg.Server.Branding.Title
	if appName == "" {
		appName = projectName
	}
	now := time.Now()
	vars := map[string]string{
		"app_name":              appName,
		"app_url":               appURL,
		"fqdn":                  fqdn,
		"onion_url":             "",
		"onion_address":         "",
		"i2p_url":               "",
		"i2p_address":           "",
		"notification_reply_to": cfg.Server.Notifications.Email.ReplyTo,
		"timestamp":             now.Format(time.RFC1123Z),
		"year":                  strconv.Itoa(now.Year()),
	}
	if addr := tor.ReadHostname(n.dataDir); addr != "" {
		vars["onion_address"] = addr
		vars["onion_url"] = "http://" + addr
	}
	if n.i2pStatus != nil {
		if status := n.i2pStatus(); status.Enabled && status.Running && status.Hostname != "" {
			vars["i2p_address"] = status.Hostname
			vars["i2p_url"] = "http://" + status.Hostname
		}
	}
	return vars
}

// emit renders template and sends it to the operator recipient when the event is
// enabled and SMTP is usable. extra overrides/extends the global variables.
func (n *emailNotifier) emit(template string, enabled bool, extra map[string]string) {
	if n == nil || !enabled || !n.sender.CanSend() {
		return
	}
	vars := n.globals()
	for k, v := range extra {
		vars[k] = v
	}
	subject, body, err := n.renderer.RenderLocalized(template, n.cfg.Server.I18n.DefaultLanguage, vars)
	if err != nil {
		fmt.Println(emailText(n.cfg.Server.I18n.DefaultLanguage, "render_failed", "template", template, "error", err))
		return
	}
	if err := n.sender.Send(n.recipient, subject, body); err != nil {
		fmt.Println(emailText(n.cfg.Server.I18n.DefaultLanguage, "send_failed", "template", template, "error", err))
	}
}

// emitBackupComplete sends the backup_complete email (opt-in, default off).
func emitBackupComplete(filename, size string) {
	if notifier == nil {
		return
	}
	notifier.emit("backup_complete", notifier.cfg.Server.Notifications.Email.Events.BackupComplete, map[string]string{
		"filename": filename,
		"size":     size,
	})
}

// emitBackupFailed sends the backup_failed email (default on).
func emitBackupFailed(filename, errMsg string) {
	if notifier == nil {
		return
	}
	notifier.emit("backup_failed", notifier.cfg.Server.Notifications.Email.Events.BackupFailed, map[string]string{
		"filename": filename,
		"error":    errMsg,
	})
}

func emitAbuseSecurityAlert(block db.IPBlock) {
	if notifier == nil {
		return
	}
	details := fmt.Sprintf("Automatic temporary block for %s until %s (%d requests in the flood window).", block.Reason, block.ExpiresAt.UTC().Format(time.RFC3339), block.OffenseCount)
	notifier.emit("security_alert", notifier.cfg.Server.Notifications.Email.Events.SecurityAlert, map[string]string{
		"event":   "Request flood detected",
		"ip":      block.IP,
		"details": details,
	})
}

func emitDiskLow(path string, usedPercent float64, freeBytes uint64, thresholdPercent float64) {
	if notifier == nil {
		return
	}
	notifier.emit("disk_low", notifier.cfg.Server.Notifications.DiskLow.Enabled, map[string]string{
		"path":              path,
		"used_percent":      strconv.FormatFloat(usedPercent, 'f', 1, 64),
		"free":              displayformat.Size(int64(freeBytes), notifier.cfg.Server.I18n.DefaultLanguage),
		"threshold_percent": strconv.FormatFloat(thresholdPercent, 'f', 1, 64),
	})
}

// emitUpdateAvailable sends the update_available email when the update_check
// task first sees a newer eligible release (opt-in, default off; AI.md PART 22).
func emitUpdateAvailable(currentVersion, newVersion string) {
	if notifier == nil {
		return
	}
	notifier.emit("update_available", notifier.cfg.Server.Notifications.Email.Events.UpdateAvailable, map[string]string{
		"current_version": currentVersion,
		"new_version":     newVersion,
	})
}

// emitUpdateInstalled sends the update_installed email after a successful
// self-update (default on; AI.md PART 22).
func emitUpdateInstalled(currentVersion, newVersion string) {
	if notifier == nil {
		return
	}
	notifier.emit("update_installed", notifier.cfg.Server.Notifications.Email.Events.UpdateInstalled, map[string]string{
		"current_version": currentVersion,
		"new_version":     newVersion,
	})
}

// suppressSchedulerError lists task IDs that emit their own dedicated failure
// notification, so the generic scheduler_error is suppressed for them (AI.md
// PART 17 suppression rules). Backup and SSL-renewal tasks are covered.
var suppressSchedulerError = map[string]bool{
	"backup_daily":  true,
	"backup_hourly": true,
	"ssl_renewal":   true,
}

// emitSchedulerError sends the scheduler_error email for a failed task, unless
// that task has a dedicated failure event (backup_failed / ssl_renewal_failed).
func emitSchedulerError(taskID, taskName, errMsg, nextRun string) {
	if notifier == nil || suppressSchedulerError[taskID] {
		return
	}
	notifier.emit("scheduler_error", notifier.cfg.Server.Notifications.Email.Events.SchedulerError, map[string]string{
		"task_name": taskName,
		"error":     errMsg,
		"next_run":  nextRun,
	})
}

// handleEmailCommand implements the `email` CLI subcommand (AI.md PART 17):
// `email test [recipient]`, `email status`, and `email validate`.
func handleEmailCommand(args []string, cfg *config.Config, configDir string) {
	config.ApplySMTPEnv(cfg)
	lang := outputLanguage
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	e := &cfg.Server.Notifications.Email
	sender := buildSender(cfg)
	renderer := email.NewRenderer(templateDir(configDir))

	switch sub {
	case "status":
		if strings.TrimSpace(e.SMTP.Host) == "" {
			fmt.Println(emailText(lang, "disabled_no_host"))
			return
		}
		fmt.Println(emailText(lang, "host_label", "host", e.SMTP.Host, "port", e.SMTP.Port))
		fmt.Println(emailText(lang, "tls_mode_label", "mode", nonEmpty(e.SMTP.TLS, "auto")))
		fmt.Println(emailText(lang, "from_label", "name", sender.FromName, "email", sender.FromEmail))
		if err := email.TestConnection(e.SMTP.Host, e.SMTP.Port, e.SMTP.TLS); err != nil {
			fmt.Println(emailText(lang, "unreachable", "error", err))
			os.Exit(exUnavailable)
		}
		fmt.Println(emailText(lang, "reachable"))

	case "validate":
		if err := email.ValidateAll(renderer); err != nil {
			fmt.Println(emailText(lang, "template_validation_failed", "error", err))
			os.Exit(exConfig)
		}
		fmt.Println(emailText(lang, "templates_valid", "count", len(email.TemplateNames)))

	case "test":
		if !sender.CanSend() {
			fmt.Println(emailText(lang, "cannot_send_test"))
			os.Exit(exConfig)
		}
		recipient := e.ReplyTo
		if len(args) > 1 && args[1] != "" {
			recipient = args[1]
		}
		if recipient == "" {
			recipient = cfg.Server.Admin.Email
		}
		if recipient == "" {
			fmt.Println(emailText(lang, "test_usage", "binary", projectName))
			os.Exit(exUsage)
		}
		n := &emailNotifier{sender: sender, renderer: renderer, cfg: cfg, dataDir: ""}
		vars := n.globals()
		subject, body, err := renderer.RenderLocalized("test", lang, vars)
		if err != nil {
			fmt.Println(emailText(lang, "render_test_failed", "error", err))
			os.Exit(exConfig)
		}
		subject = i18n.Translate(lang, "email.test_prefix") + " " + subject
		if err := sender.Send(recipient, subject, body); err != nil {
			fmt.Println(emailText(lang, "send_test_failed", "error", err))
			os.Exit(exUnavailable)
		}
		fmt.Println(emailText(lang, "test_sent", "recipient", recipient, "subject", subject))

	default:
		fmt.Println(emailText(lang, "commands_help"))
		os.Exit(exUsage)
	}
}
