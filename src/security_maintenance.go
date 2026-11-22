package main

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
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load operator credentials: %w", err)
	}
	if !service.IsElevated() && !authenticateSecurityMaintenance(cfg.Server.Token) {
		return errors.New("security report maintenance requires the operator token or root privileges")
	}
	if err := db.Init(dataDir); err != nil {
		return fmt.Errorf("initialize report database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if len(args) == 0 {
		return errors.New("usage: --maintenance security {list|show|update}")
	}
	switch args[0] {
	case "list":
		return listSecurityReports(args[1:])
	case "show":
		if len(args) != 2 || !validSecurityTrackingID(args[1]) {
			return errors.New("usage: --maintenance security show TRACKING_ID")
		}
		report, err := db.GetSecurityReport(args[1])
		if err != nil {
			return fmt.Errorf("load report metadata: %w", err)
		}
		printSecurityReport(report)
		return nil
	case "update":
		return updateSecurityReport(cfg, logsDir, args[1:])
	default:
		return errors.New("usage: --maintenance security {list|show|update}")
	}
}

func authenticateSecurityMaintenance(operatorToken string) bool {
	if operatorToken == "" || !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprint(os.Stderr, "Operator token: ")
	supplied, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return err == nil && subtle.ConstantTimeCompare(supplied, []byte(operatorToken)) == 1
}

func listSecurityReports(args []string) error {
	status := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--status" || status != "" || i+1 == len(args) {
			return errors.New("usage: --maintenance security list [--status STATUS]")
		}
		status = args[i+1]
		i++
	}
	if status != "" && !validMaintenanceSecurityStatus(status) {
		return fmt.Errorf("invalid status %q", status)
	}
	reports, err := db.ListSecurityReports(status)
	if err != nil {
		return err
	}
	for _, report := range reports {
		fmt.Printf("%s\t%s\t%s\t%q\t%s\t%s\n", report.TrackingID, report.Status, report.Severity, report.Component, report.CreatedAt, report.UpdatedAt)
	}
	return nil
}

func updateSecurityReport(cfg *config.Config, logsDir string, args []string) error {
	if len(args) == 0 || !validSecurityTrackingID(args[0]) {
		return errors.New("usage: --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]")
	}
	trackingID := args[0]
	values := map[string]string{}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		key := args[i]
		if key != "--status" && key != "--comment" && key != "--disclosure-date" && key != "--severity" {
			return fmt.Errorf("unknown update option %q", key)
		}
		if i+1 == len(args) || seen[key] {
			return fmt.Errorf("option %s requires one value and may be supplied once", key)
		}
		seen[key] = true
		values[key] = args[i+1]
		i++
	}
	status := values["--status"]
	if status != "" && !validMaintenanceSecurityStatus(status) {
		return fmt.Errorf("invalid status %q", status)
	}
	severity := values["--severity"]
	if severity != "" && !validMaintenanceSecuritySeverity(severity) {
		return fmt.Errorf("invalid severity %q", severity)
	}
	oldStatus, report, err := db.UpdateSecurityReport(trackingID, status, values["--comment"], seen["--comment"], values["--disclosure-date"], severity)
	if err != nil {
		return err
	}
	if err := appendSecurityMaintenanceLog(logsDir, trackingID, oldStatus, report.Status); err != nil {
		return fmt.Errorf("report updated but security event could not be logged: %w", err)
	}
	if status != "" || values["--comment"] != "" || values["--disclosure-date"] != "" {
		if err := notifySecurityResearcher(cfg, report); err != nil {
			fmt.Fprintf(os.Stderr, "warning: report updated; researcher notification was not sent: %v\n", err)
		}
	}
	printSecurityReport(report)
	return nil
}

func notifySecurityResearcher(cfg *config.Config, report db.SecurityReport) error {
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
		return err
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
	fmt.Printf("Tracking ID: %s\nStatus: %s\nSeverity: %s\nComponent: %q\nCreated: %s\nUpdated: %s\n", report.TrackingID, report.Status, report.Severity, report.Component, report.CreatedAt, report.UpdatedAt)
	if report.StatusComment != "" {
		fmt.Printf("Researcher comment: %q\n", report.StatusComment)
	}
	if report.DisclosureDate != "" {
		fmt.Printf("Disclosure date: %s\n", report.DisclosureDate)
	}
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
