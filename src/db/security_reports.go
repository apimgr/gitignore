package db

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// PGPKeyMetadata stores public key lifecycle data without key material.
type PGPKeyMetadata struct {
	Fingerprint         string
	CreatedAt           string
	ExpiresAt           string
	LastRotatedAt       sql.NullString
	KeyserversPublished string
	Revoked             bool
}

// GetPGPKeyMetadata returns the current or last-known key lifecycle record.
func GetPGPKeyMetadata() (PGPKeyMetadata, error) {
	mu.RLock()
	defer mu.RUnlock()
	if conn == nil {
		return PGPKeyMetadata{}, fmt.Errorf("database is not initialized")
	}
	ctx, cancel := readCtx()
	defer cancel()
	var metadata PGPKeyMetadata
	err := readConn.QueryRowContext(ctx, `SELECT fingerprint, created_at, expires_at, last_rotated_at, keyservers_published, revoked FROM pgp_key_metadata WHERE id = 1`).Scan(&metadata.Fingerprint, &metadata.CreatedAt, &metadata.ExpiresAt, &metadata.LastRotatedAt, &metadata.KeyserversPublished, &metadata.Revoked)
	return metadata, err
}

// SavePGPKeyMetadata records a generated or imported keypair's public metadata.
func SavePGPKeyMetadata(metadata PGPKeyMetadata) error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO pgp_key_metadata(id, fingerprint, created_at, expires_at, last_rotated_at, keyservers_published, revoked)
VALUES(1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET fingerprint=excluded.fingerprint, created_at=excluded.created_at, expires_at=excluded.expires_at, last_rotated_at=excluded.last_rotated_at, keyservers_published=excluded.keyservers_published, revoked=excluded.revoked`, metadata.Fingerprint, metadata.CreatedAt, metadata.ExpiresAt, nullableStringValue(metadata.LastRotatedAt), metadata.KeyserversPublished, metadata.Revoked)
		return err
	})
}

// SetPGPKeyserversPublished updates the recorded keyserver publication state.
func SetPGPKeyserversPublished(state string) error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE pgp_key_metadata SET keyservers_published = ? WHERE id = 1", state)
		return err
	})
}

// RevokePGPKeyMetadata retains the fingerprint for the audit history on delete.
func RevokePGPKeyMetadata() error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE pgp_key_metadata SET revoked = TRUE WHERE id = 1")
		return err
	})
}

// RotatePGPReportBodies re-encrypts every PGP report and updates key metadata
// atomically while sharing the submission lock used by SaveSecurityReport.
func RotatePGPReportBodies(metadata PGPKeyMetadata, reencrypt func([]byte) ([]byte, error), switchKey func() (func(), error)) error {
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	ctx, cancel := writeCtx()
	defer cancel()
	type encryptedReport struct {
		id   string
		body []byte
	}
	var reports []encryptedReport
	err := Read(ctx, func(ctx context.Context, reader *sql.DB) error {
		rows, err := reader.QueryContext(ctx, "SELECT tracking_id, body_enc FROM security_reports WHERE body_alg = 'pgp'")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var report encryptedReport
			if err := rows.Scan(&report.id, &report.body); err != nil {
				return err
			}
			reports = append(reports, report)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	updated := make(map[string][]byte, len(reports))
	for _, report := range reports {
		ciphertext, err := reencrypt(report.body)
		if err != nil {
			return fmt.Errorf("reencrypt security report %s: %w", report.id, err)
		}
		updated[report.id] = ciphertext
	}
	rollbackFiles, err := switchKey()
	if err != nil {
		return err
	}
	err = Write(ctx, func(tx *sql.Tx) error {
		for id, ciphertext := range updated {
			if _, err := tx.ExecContext(ctx, "UPDATE security_reports SET body_enc = ? WHERE tracking_id = ? AND body_alg = 'pgp'", ciphertext, id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pgp_key_metadata(id, fingerprint, created_at, expires_at, last_rotated_at, keyservers_published, revoked)
VALUES(1, ?, ?, ?, ?, ?, FALSE)
ON CONFLICT(id) DO UPDATE SET fingerprint=excluded.fingerprint, created_at=excluded.created_at, expires_at=excluded.expires_at, last_rotated_at=excluded.last_rotated_at, keyservers_published=excluded.keyservers_published, revoked=FALSE`, metadata.Fingerprint, metadata.CreatedAt, metadata.ExpiresAt, nullableStringValue(metadata.LastRotatedAt), metadata.KeyserversPublished); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		rollbackFiles()
	}
	return err
}

func nullableStringValue(value sql.NullString) interface{} {
	if !value.Valid {
		return nil
	}
	return value.String
}

// SecurityReport contains operator-visible report metadata. It deliberately
// excludes encrypted report and contact bodies.
type SecurityReport struct {
	TrackingID       string
	Status           string
	Severity         string
	Component        string
	CreatedAt        string
	UpdatedAt        string
	StatusComment    string
	DisclosureDate   string
	CreditPreference string
	CreditName       string
}

// SecurityReportContactCiphertext returns only the stored encrypted contact
// blob so a trusted maintenance action can send a status notice.
func SecurityReportContactCiphertext(trackingID string) ([]byte, error) {
	mu.RLock()
	defer mu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	ctx, cancel := readCtx()
	defer cancel()
	var encrypted []byte
	if err := readConn.QueryRowContext(ctx, "SELECT contact_enc FROM security_reports WHERE tracking_id = ?", trackingID).Scan(&encrypted); err != nil {
		return nil, err
	}
	return encrypted, nil
}

// CleanupClosedSecurityReports removes only closed reports whose retention
// deadline has passed. Public acknowledgments are retained independently.
func CleanupClosedSecurityReports(before time.Time) (int64, error) {
	ctx, cancel := writeCtx()
	defer cancel()
	var total int64
	for {
		var count int64
		err := Write(ctx, func(tx *sql.Tx) error {
			result, err := tx.ExecContext(ctx, `DELETE FROM security_reports WHERE tracking_id IN (SELECT tracking_id FROM security_reports WHERE closed_at IS NOT NULL AND datetime(closed_at) < datetime(?) LIMIT 500)`, before.UTC().Format(time.RFC3339))
			if err != nil {
				return fmt.Errorf("delete expired security reports: %w", err)
			}
			count, err = result.RowsAffected()
			if err != nil {
				return fmt.Errorf("count deleted security reports: %w", err)
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += count
		if count < 500 {
			return total, nil
		}
		runtime.Gosched()
	}
}

// ListSecurityReports returns report metadata, optionally filtered by status.
func ListSecurityReports(status string) ([]SecurityReport, error) {
	mu.RLock()
	defer mu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	ctx, cancel := readCtx()
	defer cancel()
	query := `SELECT tracking_id, status, severity, component, created_at, updated_at, researcher_comment, disclosure_date, credit_pref, COALESCE(credit_name, '')
		FROM security_reports`
	args := []interface{}{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC, tracking_id"
	rows, err := readConn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list security reports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	reports := make([]SecurityReport, 0)
	for rows.Next() {
		var report SecurityReport
		if err := rows.Scan(&report.TrackingID, &report.Status, &report.Severity, &report.Component, &report.CreatedAt, &report.UpdatedAt, &report.StatusComment, &report.DisclosureDate, &report.CreditPreference, &report.CreditName); err != nil {
			return nil, fmt.Errorf("scan security report: %w", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read security reports: %w", err)
	}
	return reports, nil
}

// GetSecurityReport returns operator-visible metadata without decrypted data.
func GetSecurityReport(trackingID string) (SecurityReport, error) {
	mu.RLock()
	defer mu.RUnlock()
	if conn == nil {
		return SecurityReport{}, fmt.Errorf("database is not initialized")
	}
	ctx, cancel := readCtx()
	defer cancel()
	var report SecurityReport
	err := readConn.QueryRowContext(ctx, `SELECT tracking_id, status, severity, component, created_at, updated_at, researcher_comment, disclosure_date, credit_pref, COALESCE(credit_name, '')
		FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&report.TrackingID, &report.Status, &report.Severity, &report.Component, &report.CreatedAt, &report.UpdatedAt, &report.StatusComment, &report.DisclosureDate, &report.CreditPreference, &report.CreditName)
	if err != nil {
		return SecurityReport{}, err
	}
	return report, nil
}

// UpdateSecurityReport applies monotonic workflow updates and publishes an
// opted-in acknowledgment atomically when a report is disclosed.
func UpdateSecurityReport(trackingID, status, comment string, setComment bool, disclosureDate, severity string) (oldStatus string, report SecurityReport, err error) {
	if len([]rune(comment)) > 2000 {
		return "", report, fmt.Errorf("comment exceeds 2000 characters")
	}
	if disclosureDate != "" {
		parsed, parseErr := time.Parse("2006-01-02", disclosureDate)
		if parseErr != nil || parsed.Format("2006-01-02") != disclosureDate {
			return "", report, fmt.Errorf("disclosure date must use YYYY-MM-DD")
		}
	}
	if severity != "" && !validReportSeverity(severity) {
		return "", report, fmt.Errorf("invalid severity")
	}
	if status != "" && !validReportStatus(status) {
		return "", report, fmt.Errorf("invalid status")
	}
	ctx, cancel := writeCtx()
	defer cancel()
	err = Write(ctx, func(tx *sql.Tx) error {
		oldStatus, report, err = updateSecurityReportTx(ctx, tx, trackingID, status, comment, setComment, disclosureDate, severity)
		return err
	})
	return oldStatus, report, err
}

func updateSecurityReportTx(ctx context.Context, tx *sql.Tx, trackingID, status, comment string, setComment bool, disclosureDate, severity string) (oldStatus string, report SecurityReport, err error) {
	var closedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT status, credit_pref, COALESCE(credit_name, ''), severity, closed_at FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&oldStatus, &report.CreditPreference, &report.CreditName, &report.Severity, &closedAt); err != nil {
		return "", report, err
	}
	if closedAt.Valid && status != "" && status != oldStatus {
		return "", report, fmt.Errorf("closed report status cannot change")
	}
	if status != "" && !allowedReportTransition(oldStatus, status) {
		return "", report, fmt.Errorf("status transition %s -> %s is not allowed", oldStatus, status)
	}
	var set []string
	var values []interface{}
	if status != "" {
		set = append(set, "status = ?")
		values = append(values, status)
		if status == "disclosed" || status == "wont_fix" {
			set = append(set, "closed_at = COALESCE(closed_at, CURRENT_TIMESTAMP)")
		}
	}
	if setComment {
		set = append(set, "researcher_comment = ?")
		values = append(values, comment)
	}
	if disclosureDate != "" {
		set = append(set, "disclosure_date = ?")
		values = append(values, disclosureDate)
	}
	if severity != "" {
		set = append(set, "severity = ?")
		values = append(values, severity)
	}
	if len(set) == 0 {
		return "", report, fmt.Errorf("at least one update field is required")
	}
	set = append(set, "updated_at = CURRENT_TIMESTAMP")
	values = append(values, trackingID)
	if _, err := tx.ExecContext(ctx, "UPDATE security_reports SET "+strings.Join(set, ", ")+" WHERE tracking_id = ?", values...); err != nil {
		return "", report, err
	}
	if status == "disclosed" && report.CreditPreference != "no" {
		disclosedSeverity := report.Severity
		if severity != "" {
			disclosedSeverity = severity
		}
		credit := ""
		switch report.CreditPreference {
		case "name", "handle":
			credit = strings.TrimSpace(report.CreditName)
		case "anon":
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_thanks WHERE credit LIKE 'Anonymous Researcher #%'`).Scan(&count); err != nil {
				return "", report, err
			}
			credit = fmt.Sprintf("Anonymous Researcher #%d", count+1)
		}
		if credit != "" {
			disclosedOn := disclosureDate
			if disclosedOn == "" {
				disclosedOn = time.Now().UTC().Format("2006-01-02")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO security_thanks (tracking_id, credit, severity, disclosed_on) VALUES (?, ?, ?, ?) ON CONFLICT(tracking_id) DO NOTHING`, trackingID, credit, disclosedSeverity, disclosedOn); err != nil {
				return "", report, err
			}
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT tracking_id, status, severity, component, created_at, updated_at, researcher_comment, disclosure_date, credit_pref, COALESCE(credit_name, '') FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&report.TrackingID, &report.Status, &report.Severity, &report.Component, &report.CreatedAt, &report.UpdatedAt, &report.StatusComment, &report.DisclosureDate, &report.CreditPreference, &report.CreditName); err != nil {
		return "", report, err
	}
	return oldStatus, report, nil
}

func validReportStatus(status string) bool {
	switch status {
	case "received", "triaged", "confirmed", "patching", "disclosed", "wont_fix":
		return true
	default:
		return false
	}
}

func validReportSeverity(severity string) bool {
	switch severity {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}

func allowedReportTransition(from, to string) bool {
	if from == to {
		return true
	}
	if from == "disclosed" || from == "wont_fix" {
		return false
	}
	if to == "wont_fix" {
		return true
	}
	order := map[string]int{"received": 0, "triaged": 1, "confirmed": 2, "patching": 3, "disclosed": 4}
	return order[to] > order[from]
}
