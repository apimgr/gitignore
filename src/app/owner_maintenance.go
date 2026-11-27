package app

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/service"
	"golang.org/x/term"
)

type ownerExport struct {
	Format    int                 `json:"format"`
	Owner     ownerExportIdentity `json:"owner"`
	Tokens    []ownerExportToken  `json:"tokens"`
	Resources []json.RawMessage   `json:"resources"`
}

type ownerExportIdentity struct {
	ID         string `json:"id"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
}

type ownerExportToken struct {
	Prefix     string `json:"prefix"`
	Kind       string `json:"kind"`
	Name       string `json:"name,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
	ExpiresAt  *int64 `json:"expires_at"`
	Status     string `json:"status"`
}

func handleOwnerMaintenance(cmd string, cfg *config.ServerConfig, dataDir, logsDir string, args []string) error {
	if cfg == nil {
		return errors.New("configuration is unavailable")
	}
	if !service.IsElevated() && !authenticateSecurityMaintenance(cfg.Server.Token) {
		return errors.New("operator authorization required")
	}
	if err := db.Init(dataDir, databaseConnectionConfig(cfg)); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer func() { _ = db.Close() }()

	switch cmd {
	case "token":
		if len(args) == 0 {
			return errors.New("usage: --maintenance token list|revoke <prefix> [--yes]")
		}
		switch args[0] {
		case "list":
			if len(args) != 1 {
				return errors.New("usage: --maintenance token list")
			}
			return listOwnerTokens()
		case "revoke":
			return revokeOwnerToken(cfg, logsDir, args[1:])
		default:
			return errors.New("usage: --maintenance token list|revoke <prefix> [--yes]")
		}
	case "data":
		if len(args) == 0 {
			return errors.New("usage: --maintenance data export|delete --owner <prefix> [--output FILE] [--yes]")
		}
		return maintainOwnerData(cfg, logsDir, args)
	case "compliance":
		if len(args) != 1 || args[0] != "report" {
			return errors.New("usage: --maintenance compliance report")
		}
		return printComplianceReport(cfg, dataDir, logsDir)
	default:
		return errors.New("unknown owner maintenance command")
	}
}

func listOwnerTokens() error {
	items, err := db.ListAllOwnerTokens()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	fmt.Println("PREFIX\tKIND\tOWNER\tNAME\tCREATED\tLAST USED\tSTATUS")
	for _, item := range items {
		status := "active"
		if item.RevokedAt.Valid {
			status = "revoked"
		} else if item.ExpiresAt.Valid && item.ExpiresAt.Int64 <= now {
			status = "expired"
		}
		name := ""
		if item.Name.Valid {
			name = item.Name.String
		}
		lastUsed := "never"
		if item.LastUsedAt.Valid {
			lastUsed = time.Unix(item.LastUsedAt.Int64, 0).UTC().Format(time.RFC3339)
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.TokenPrefix, item.Kind, item.OwnerID, name,
			time.Unix(item.CreatedAt, 0).UTC().Format(time.RFC3339), lastUsed, status)
	}
	return nil
}

func revokeOwnerToken(cfg *config.ServerConfig, logsDir string, args []string) error {
	args, yes := stripYes(args)
	if len(args) != 1 {
		return errors.New("usage: --maintenance token revoke <prefix> [--yes]")
	}
	prefix := args[0]
	items, err := db.FindOwnerTokensByPrefix(prefix)
	if err != nil {
		return err
	}
	var target *db.OwnerToken
	for i := range items {
		if !items[i].RevokedAt.Valid && (!items[i].ExpiresAt.Valid || items[i].ExpiresAt.Int64 > time.Now().UTC().Unix()) {
			if target != nil {
				return errors.New("token prefix is ambiguous")
			}
			target = &items[i]
		}
	}
	if target == nil {
		return db.ErrOwnerTokenNotFound
	}
	if target.Kind == "primary" {
		all, err := db.ListOwnerTokens(target.OwnerID)
		if err != nil {
			return err
		}
		if err := confirmOwnerID(target.OwnerID, fmt.Sprintf("Revoke primary token and delete owner %s (%d tokens, 0 resources)", target.OwnerID, len(all)), yes); err != nil {
			return err
		}
		count, err := db.DeleteOwner(target.OwnerID)
		if err != nil {
			return err
		}
		return audit.Append(logsDir, audit.Entry{Event: "token.revoked", Category: "security", Severity: "info", Actor: audit.Actor{Type: "operator", ID: "operator"}, Target: &audit.Target{Type: "owner", ID: target.OwnerID}, Details: map[string]any{"scope": "owner", "tokens_deleted": count, "resources_deleted": 0, "source": "cli"}, Result: "success", Source: "cli"})
	}
	if err := confirmTypedValue(prefix, "Type token prefix to revoke: ", yes); err != nil {
		return err
	}
	count, err := db.RevokeOwnerTokenPrefix(prefix, time.Now().UTC())
	if err != nil {
		return err
	}
	return audit.Append(logsDir, audit.Entry{Event: "token.revoked", Category: "security", Severity: "info", Actor: audit.Actor{Type: "operator", ID: "operator"}, Target: &audit.Target{Type: "token", ID: prefix}, Details: map[string]any{"scope": "token", "tokens_deleted": 0, "tokens_revoked": count, "resources_deleted": 0, "source": "cli"}, Result: "success", Source: "cli"})
}

func maintainOwnerData(cfg *config.ServerConfig, logsDir string, args []string) error {
	command := args[0]
	ownerPrefix, output := "", ""
	args = args[1:]
	args, yes := stripYes(args)
	for i := 0; i < len(args); i++ {
		if (args[i] == "--owner" || args[i] == "--output") && i+1 < len(args) {
			if args[i] == "--owner" {
				if ownerPrefix != "" {
					return errors.New("--owner may be specified once")
				}
				ownerPrefix = args[i+1]
			} else {
				if output != "" {
					return errors.New("--output may be specified once")
				}
				output = args[i+1]
			}
			i++
			continue
		}
		return fmt.Errorf("unknown data option %q", args[i])
	}
	if ownerPrefix == "" || command != "export" && command != "delete" || command == "delete" && output != "" {
		return errors.New("usage: --maintenance data export|delete --owner <prefix> [--output FILE] [--yes]")
	}
	owner, tokens, err := ownerForPrefix(ownerPrefix)
	if err != nil {
		return err
	}
	requestID := fmt.Sprintf("data_%d", time.Now().UTC().UnixNano())
	if err := appendComplianceAudit(logsDir, "compliance.data_"+map[string]string{"export": "export", "delete": "deletion"}[command]+"_requested", owner.ID, map[string]any{"request_id": requestID}); err != nil {
		return err
	}
	if command == "export" {
		return writeOwnerExport(owner, tokens, output, logsDir, requestID)
	}
	if err := confirmOwnerID(owner.ID, fmt.Sprintf("Delete owner %s (%d tokens, 0 resources)", owner.ID, len(tokens)), yes); err != nil {
		return err
	}
	deletedTokens, err := db.DeleteOwner(owner.ID)
	if err != nil {
		return err
	}
	if err := appendComplianceAudit(logsDir, "compliance.data_deletion_completed", owner.ID, map[string]any{"request_id": requestID, "deleted_count": deletedTokens + 1, "anonymized_count": 0}); err != nil {
		return err
	}
	fmt.Printf("Deleted owner and %d tokens; anonymized 0 resources.\n", deletedTokens)
	return nil
}

func ownerForPrefix(prefix string) (db.OwnerRecord, []db.OwnerToken, error) {
	items, err := db.FindOwnerTokensByPrefix(prefix)
	if err != nil {
		return db.OwnerRecord{}, nil, err
	}
	ownerID := ""
	for _, item := range items {
		if item.OwnerID != "" {
			if ownerID != "" && ownerID != item.OwnerID {
				return db.OwnerRecord{}, nil, errors.New("token prefix is ambiguous")
			}
			ownerID = item.OwnerID
		}
	}
	if ownerID == "" {
		return db.OwnerRecord{}, nil, db.ErrOwnerTokenNotFound
	}
	owner, err := db.GetOwner(ownerID)
	if err != nil {
		return db.OwnerRecord{}, nil, err
	}
	tokens, err := db.ListOwnerTokens(ownerID)
	return owner, tokens, err
}

func writeOwnerExport(owner db.OwnerRecord, tokens []db.OwnerToken, output, logsDir, requestID string) error {
	result := ownerExport{Format: 1, Owner: ownerExportIdentity{ID: owner.ID, CreatedAt: owner.CreatedAt, LastSeenAt: owner.LastSeenAt}, Tokens: make([]ownerExportToken, 0, len(tokens)), Resources: []json.RawMessage{}}
	now := time.Now().UTC().Unix()
	for _, item := range tokens {
		status := "active"
		if item.RevokedAt.Valid {
			status = "revoked"
		} else if item.ExpiresAt.Valid && item.ExpiresAt.Int64 <= now {
			status = "expired"
		}
		name := ""
		if item.Name.Valid {
			name = item.Name.String
		}
		result.Tokens = append(result.Tokens, ownerExportToken{Prefix: item.TokenPrefix, Kind: item.Kind, Name: name, CreatedAt: item.CreatedAt, LastUsedAt: nullableInt64Pointer(item.LastUsedAt), ExpiresAt: nullableInt64Pointer(item.ExpiresAt), Status: status})
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if output == "-" {
		if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
			return err
		}
	} else {
		if output == "" {
			output = fmt.Sprintf("gitignore-export-%s-%s.json", owner.ID, time.Now().UTC().Format("20060102"))
		}
		file, err := os.OpenFile(filepath.Clean(output), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err = file.Write(append(encoded, '\n')); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Chmod(output, 0600); err != nil {
			return err
		}
	}
	if err := appendComplianceAudit(logsDir, "compliance.data_export_completed", owner.ID, map[string]any{"request_id": requestID, "file_size": len(encoded)}); err != nil {
		return err
	}
	return nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if value.Valid {
		result := value.Int64
		return &result
	}
	return nil
}

func appendComplianceAudit(logsDir, event, ownerID string, details map[string]any) error {
	return audit.Append(logsDir, audit.Entry{Event: event, Category: "compliance", Severity: "info", Actor: audit.Actor{Type: "operator", ID: "operator"}, Target: &audit.Target{Type: "owner", ID: ownerID}, Details: details, Result: "success", Source: "cli"})
}

func printComplianceReport(cfg *config.ServerConfig, dataDir, logsDir string) error {
	owners, activeTokens, err := db.OwnerAndActiveTokenCounts(time.Now().UTC())
	if err != nil {
		return err
	}
	standards := []struct {
		name    string
		enabled bool
	}{{"GDPR", cfg.Server.Compliance.GDPR}, {"CCPA", cfg.Server.Compliance.CCPA}, {"HIPAA", cfg.Server.Compliance.HIPAA}, {"SOC2", cfg.Server.Compliance.SOC2}, {"PCI DSS", cfg.Server.Compliance.PCIDSS}, {"ISO 27001", cfg.Server.Compliance.ISO27001}, {"FedRAMP", cfg.Server.Compliance.FedRAMP}, {"LGPD", cfg.Server.Compliance.LGPD}, {"PIPEDA", cfg.Server.Compliance.PIPEDA}, {"APPI", cfg.Server.Compliance.APPI}, {"PDPA", cfg.Server.Compliance.PDPA}}
	fmt.Printf("Compliance enabled: %t\n", cfg.Server.Compliance.Enabled)
	fmt.Printf("Standards: ")
	for _, standard := range standards {
		fmt.Printf("%s=%t ", standard.name, standard.enabled)
	}
	fmt.Printf("\nRetention: %s\nOwners: %d\nResources: 0\nActive tokens: %d\nOpen security reports: ", cfg.Server.Privacy.Retention.Period, owners, activeTokens)
	reports, err := db.ListSecurityReports("")
	if err != nil {
		return err
	}
	openReports := 0
	for _, report := range reports {
		if report.Status != "disclosed" && report.Status != "wont_fix" {
			openReports++
		}
	}
	fmt.Printf("%d\n", openReports)
	latest, err := latestBackupFile(apppath.GetBackupDir())
	if err != nil {
		latest = "none"
	} else {
		latest = filepath.Base(latest)
	}
	fmt.Printf("Last backup: %s\n", latest)
	entries, integrity := audit.Verify(logsDir)
	if integrity != nil {
		fmt.Printf("Audit integrity: broken at %s\n", integrity)
	} else {
		fmt.Printf("Audit integrity: intact (%d entries)\n", entries)
	}
	events, err := audit.CountEventsSince(logsDir, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil {
		return err
	}
	fmt.Println("Compliance events (last 30 days):")
	if len(events) == 0 {
		fmt.Println("  none")
	}
	for _, name := range []string{"compliance.standard_enabled", "compliance.standard_disabled", "compliance.data_export_requested", "compliance.data_export_completed", "compliance.data_deletion_requested", "compliance.data_deletion_completed", "compliance.consent_updated", "compliance.report_generated"} {
		fmt.Printf("  %s: %d\n", name, events[name])
	}
	if err := appendComplianceAudit(logsDir, "compliance.report_generated", "", map[string]any{"report_type": "operator"}); err != nil {
		return err
	}
	_ = dataDir
	return nil
}

func stripYes(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	yes := false
	for _, arg := range args {
		if arg == "--yes" || arg == "--force" || arg == "-y" {
			yes = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	return filtered, yes
}

func confirmOwnerID(ownerID, message string, yes bool) error {
	if yes {
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s\nType owner id to confirm: ", message)
	return confirmTypedValue(ownerID, "", false)
}

func confirmTypedValue(expected, prompt string, yes bool) error {
	if yes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("confirmation requires an interactive terminal; use --yes for scripts")
	}
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil || subtle.ConstantTimeCompare(value, []byte(expected)) != 1 {
		return errors.New("confirmation did not match")
	}
	return nil
}
