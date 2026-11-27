package app

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/pgp"
	"github.com/apimgr/gitignore/src/server"
	"github.com/apimgr/gitignore/src/service"
	"golang.org/x/term"
)

type securityReportContact struct {
	Email         string `json:"email"`
	ResearcherGPG string `json:"researcher_gpg,omitempty"`
}

func handleSecurityMaintenance(configPath, dataDir, logsDir string, args []string) error {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.security.load_config_failed"), err)
	}
	if !service.IsElevated() && !authenticateSecurityMaintenance(cfg.Server.Token) {
		return errors.New(cliText("cli.security.operator_required"))
	}
	if err := db.Init(dataDir); err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.security.database_init_failed"), err)
	}
	defer func() { _ = db.Close() }()
	if len(args) == 0 {
		return errors.New(cliText("cli.security.usage"))
	}
	switch args[0] {
	case "list":
		return listSecurityReports(args[1:])
	case "show":
		if len(args) != 2 || !validSecurityTrackingID(args[1]) {
			return errors.New(cliText("cli.security.usage_show"))
		}
		report, err := db.GetSecurityReport(args[1])
		if err != nil {
			return fmt.Errorf("%s: %w", cliText("cli.security.load_report_failed"), err)
		}
		printSecurityReport(report)
		return nil
	case "update":
		return updateSecurityReport(cfg, logsDir, args[1:])
	default:
		return errors.New(cliText("cli.security.usage"))
	}
}

func authenticateSecurityMaintenance(operatorToken string) bool {
	if operatorToken == "" || !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprint(os.Stderr, cliText("cli.security.operator_token_prompt"))
	supplied, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return err == nil && subtle.ConstantTimeCompare(supplied, []byte(operatorToken)) == 1
}

func listSecurityReports(args []string) error {
	status := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--status" || status != "" || i+1 == len(args) {
			return errors.New(cliText("cli.security.usage_list"))
		}
		status = args[i+1]
		i++
	}
	if status != "" && !validMaintenanceSecurityStatus(status) {
		return errors.New(cliText("cli.security.invalid_status", "status", status))
	}
	reports, err := db.ListSecurityReports(status)
	if err != nil {
		return err
	}
	for _, report := range reports {
		fmt.Printf("%s\t%s\t%s\t%q\t%s\t%s\n", report.TrackingID, securityStatusLabel(report.Status), securitySeverityLabel(report.Severity), report.Component, report.CreatedAt, report.UpdatedAt)
	}
	return nil
}

func updateSecurityReport(cfg *config.ServerConfig, logsDir string, args []string) error {
	if len(args) == 0 || !validSecurityTrackingID(args[0]) {
		return errors.New(cliText("cli.security.usage_update"))
	}
	trackingID := args[0]
	values := map[string]string{}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		key := args[i]
		if key != "--status" && key != "--comment" && key != "--disclosure-date" && key != "--severity" {
			return errors.New(cliText("cli.security.unknown_option", "option", key))
		}
		if i+1 == len(args) || seen[key] {
			return errors.New(cliText("cli.security.option_value_required", "option", key))
		}
		seen[key] = true
		values[key] = args[i+1]
		i++
	}
	status := values["--status"]
	if status != "" && !validMaintenanceSecurityStatus(status) {
		return errors.New(cliText("cli.security.invalid_status", "status", status))
	}
	severity := values["--severity"]
	if severity != "" && !validMaintenanceSecuritySeverity(severity) {
		return errors.New(cliText("cli.security.invalid_severity", "severity", severity))
	}
	oldStatus, report, err := db.UpdateSecurityReport(trackingID, status, values["--comment"], seen["--comment"], values["--disclosure-date"], severity)
	if err != nil {
		return err
	}
	if err := appendSecurityMaintenanceLog(logsDir, trackingID, oldStatus, report.Status); err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.security.updated_log_failed"), err)
	}
	if status != "" || values["--comment"] != "" || values["--disclosure-date"] != "" {
		if err := notifySecurityResearcher(cfg, report); err != nil {
			fmt.Fprintln(os.Stderr, cliText("cli.security.notification_warning", "error", err))
		}
	}
	printSecurityReport(report)
	return nil
}

func notifySecurityResearcher(cfg *config.ServerConfig, report db.SecurityReport) error {
	sender := buildSender(cfg)
	if !sender.CanSend() {
		return nil
	}
	ciphertext, err := db.SecurityReportContactCiphertext(report.TrackingID)
	if err != nil || len(ciphertext) == 0 {
		return nil
	}
	plaintext, err := server.DecryptSecurityReport(cfg.Server.Security.EncryptionKey, ciphertext)
	if err != nil {
		plaintext, _, err = db.DecryptSecurityReportDuringRotation(report.TrackingID, "contact_enc", ciphertext, cfg.Server.Security.EncryptionKey)
		if err != nil {
			return err
		}
	}
	var contact securityReportContact
	if err := json.Unmarshal(plaintext, &contact); err != nil {
		return fmt.Errorf("decode encrypted researcher contact: %w", err)
	}
	address, err := mail.ParseAddress(contact.Email)
	if err != nil {
		return fmt.Errorf("encrypted researcher address is invalid")
	}
	body := "The status of your security report " + report.TrackingID + " is now " + report.Status + "."
	if report.StatusComment != "" {
		body += "\n\nMaintainer comment: " + report.StatusComment
	}
	if report.DisclosureDate != "" {
		body += "\n\nExpected disclosure date: " + report.DisclosureDate
	}
	if strings.TrimSpace(contact.ResearcherGPG) != "" {
		publicKey := strings.TrimSpace(contact.ResearcherGPG)
		if strings.HasPrefix(publicKey, "https://") {
			publicKey, err = pgp.FetchPublicKey(context.Background(), publicKey)
			if err != nil {
				return fmt.Errorf("fetch researcher key for status notice: %w", err)
			}
		}
		ciphertext, err := pgp.Encrypt([]byte(body), publicKey)
		if err != nil {
			return fmt.Errorf("encrypt researcher status notice: %w", err)
		}
		body = string(ciphertext)
	}
	return sender.Send(address.Address, "Security report status: "+report.TrackingID, body)
}

func appendSecurityMaintenanceLog(logsDir, trackingID, oldStatus, newStatus string) error {
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return err
	}
	path := filepath.Join(logsDir, "security.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	line := fmt.Sprintf("%s security.report_updated tracking_id=%q old_status=%q new_status=%q\n", time.Now().UTC().Format(time.RFC3339), trackingID, oldStatus, newStatus)
	if _, err := file.WriteString(line); err != nil {
		return err
	}
	return file.Chmod(0600)
}

func printSecurityReport(report db.SecurityReport) {
	fmt.Println(cliText("cli.security.field.tracking_id", "value", report.TrackingID))
	fmt.Println(cliText("cli.security.field.status", "value", securityStatusLabel(report.Status)))
	fmt.Println(cliText("cli.security.field.severity", "value", securitySeverityLabel(report.Severity)))
	fmt.Println(cliText("cli.security.field.component", "value", report.Component))
	fmt.Println(cliText("cli.security.field.created", "value", report.CreatedAt))
	fmt.Println(cliText("cli.security.field.updated", "value", report.UpdatedAt))
	if report.StatusComment != "" {
		fmt.Println(cliText("cli.security.field.researcher_comment", "value", report.StatusComment))
	}
	if report.DisclosureDate != "" {
		fmt.Println(cliText("cli.security.field.disclosure_date", "value", report.DisclosureDate))
	}
}

func securityStatusLabel(status string) string {
	return cliText("cli.security.status." + status)
}

func securitySeverityLabel(severity string) string {
	return cliText("cli.security.severity." + severity)
}

func validSecurityTrackingID(value string) bool {
	if len(value) != 20 || !strings.HasPrefix(value, "sec_") {
		return false
	}
	_, err := hex.DecodeString(value[4:])
	return err == nil
}

func validMaintenanceSecurityStatus(value string) bool {
	switch value {
	case "received", "triaged", "confirmed", "patching", "disclosed", "wont_fix":
		return true
	default:
		return false
	}
}

func validMaintenanceSecuritySeverity(value string) bool {
	switch value {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}
