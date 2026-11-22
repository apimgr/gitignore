package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
)

func TestSecurityReportSubmissionStoresEncryptedReportAndReturnsTrackingID(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	s := New(&Config{Cfg: cfg, Version: "1.2.3", Commit: "abc1234"})
	form := url.Values{
		"severity": {"high"}, "component": {"server"}, "summary": {"Unsafe disclosure"},
		"endpoint": {"/api/v1/example"}, "reproduction": {"Send a crafted request."},
		"impact": {"Private data could be exposed."}, "suggested_fix": {"Validate input."},
		"credit_preference": {"no"}, "coordinated_disclosure": {"yes"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/contact", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "researcher-test")
	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	s.handleSecurityReportSubmit(recorder, request, "Researcher", "researcher@example.test", "security-id")
	if recorder.Code != http.StatusOK {
		t.Fatalf("submission response = %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data struct {
			TrackingID string `json:"tracking_id"`
			Message    string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(response.Data.TrackingID, "sec_") || len(response.Data.TrackingID) != 20 || strings.Contains(response.Data.Message, "email") {
		t.Fatalf("submission response exposes unexpected tracking/email state: %+v", response.Data)
	}
	report, err := db.GetSecurityReport(response.Data.TrackingID)
	if err != nil || report.Status != "received" {
		t.Fatalf("stored report = %+v, err=%v", report, err)
	}
	if _, _, _, err := db.ClaimSecurityReportStatus(response.Data.TrackingID, strings.Repeat("0", sha256.Size*2), "2026-11-22"); err == nil {
		t.Fatal("status page accepted an unrelated token hash")
	}
}

func TestAPIContactDescriptionIncludesConditionalSecurityFields(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Server.Pages.Contact.Enabled = true
	s := New(&Config{Cfg: cfg})
	securityToken, err := securityID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/contact?security_id="+securityToken, nil)
	recorder := httptest.NewRecorder()
	s.handleAPIContact(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("contact description response = %d %s", recorder.Code, recorder.Body.String())
	}
	for _, expected := range []string{`"security"`, `"form_token"`, `"captcha"`, `"researcher_gpg"`, `"credit_handle"`, `"required_when"`} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Errorf("contact description missing %s: %s", expected, recorder.Body.String())
		}
	}
}
