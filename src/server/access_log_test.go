package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogSuppressesSuccessfulHealthAndRecordsFailures(t *testing.T) {
	var entries []AccessLogEntry
	failed := false
	s := &Server{config: &Config{AccessLog: func(entry AccessLogEntry) { entries = append(entries, entry) }}}
	handler := s.accessLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !failed {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
	}))

	for _, path := range []string{"/server/healthz", "/api/v1/server/healthz"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if len(entries) != 0 {
		t.Fatalf("successful health probes generated %d access log entries", len(entries))
	}
	failed = true
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/server/healthz", nil))
	if len(entries) != 1 || entries[0].Status != http.StatusServiceUnavailable || entries[0].Bytes != int64(len("unavailable")) {
		t.Fatalf("failed health probe access log = %+v", entries)
	}
}

func TestAccessLogRedactsSensitiveQueryParameters(t *testing.T) {
	var entry AccessLogEntry
	s := &Server{config: &Config{AccessLog: func(value AccessLogEntry) { entry = value }}}
	request := httptest.NewRequest(http.MethodGet, "/search?term=hello&TOKEN=secret&session=s1&code=c1&key=k1&password=p1&secret=s2&auth=a1&pwd=p2&api_key=k2&apikey=k3&access_token=t1&refresh_token=t2&authToken=t3&security_id=sid&review_token=review&csrf_token=csrf", nil)
	request.Header.Set("Referer", "https://example.test/page?session=ref-secret&keep=shown")
	s.accessLogMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), request)
	for _, secret := range []string{"s1", "c1", "k1", "p1", "s2", "a1", "p2", "k2", "k3", "t1", "t2", "t3"} {
		if strings.Contains(entry.Query, secret) {
			t.Errorf("access query contains secret %q: %s", secret, entry.Query)
		}
	}
	for _, expected := range []string{"TOKEN=xxxxx", "session=xxxxx", "code=xxxxx", "key=xxxxx", "password=xxxxx", "secret=xxxxx", "auth=xxxxx", "pwd=xxxxx", "api_key=xxxxx", "apikey=xxxxx", "access_token=xxxxx", "refresh_token=xxxxx", "authToken=xxxxx", "security_id=xxxxx", "review_token=xxxxx", "csrf_token=xxxxx", "term=hello"} {
		if !strings.Contains(entry.Query, expected) {
			t.Errorf("access query %q does not contain %q", entry.Query, expected)
		}
	}
	if strings.Contains(entry.Referer, "ref-secret") || !strings.Contains(entry.Referer, "session=xxxxx") || !strings.Contains(entry.Referer, "keep=shown") {
		t.Errorf("access referer was not safely redacted: %s", entry.Referer)
	}
}
