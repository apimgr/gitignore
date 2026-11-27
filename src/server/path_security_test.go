package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apppath "github.com/apimgr/gitignore/src/path"
)

func TestPathSecurityMiddlewareRejectsRawAndEncodedTraversal(t *testing.T) {
	for _, target := range []string{"/static/../server/admin", "/static/%2e%2e/server/admin"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		pathSecurityMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Errorf("handler reached for traversal path %q", target)
		})).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("path %q status = %d, want %d", target, recorder.Code, http.StatusBadRequest)
		}
	}
}

func TestURLNormalizeMiddlewareCollapsesDuplicateSlashes(t *testing.T) {
	var got string
	handler := urlNormalizeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "//api///v1//items", nil))
	if got != "/api/v1/items" {
		t.Fatalf("normalized path = %q, want %q", got, "/api/v1/items")
	}
}

func TestPathSecurityMiddlewareRejectsRepeatedDotSegments(t *testing.T) {
	recorder := httptest.NewRecorder()
	pathSecurityMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler reached for repeated-dot route segment")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/server/admin/....//secret", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got, err := apppath.SafeResourcePath("reports/Report..pdf"); err != nil || got != "reports/Report..pdf" {
		t.Fatalf("resource filename with internal dots = %q, %v", got, err)
	}
}
