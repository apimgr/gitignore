package server

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/common/i18n"
)

func TestMapAPIErrorCodeToHTTPStatus(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{"BAD_REQUEST", http.StatusBadRequest},
		{"UNAUTHORIZED", http.StatusUnauthorized},
		{"FORBIDDEN", http.StatusForbidden},
		{"NOT_FOUND", http.StatusNotFound},
		{"RATE_LIMITED", http.StatusTooManyRequests},
		{"MAINTENANCE", http.StatusServiceUnavailable},
		{"SOME_UNKNOWN_CODE", http.StatusInternalServerError},
		{"", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			if got := mapAPIErrorCodeToHTTPStatus(tc.code); got != tc.want {
				t.Errorf("mapAPIErrorCodeToHTTPStatus(%q) = %d, want %d", tc.code, got, tc.want)
			}
		})
	}
}

func TestEncodeJSONUsesTwoSpaceIndentAndFinalNewline(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := encodeJSON(recorder, APIResponse{OK: true, Data: map[string]string{"status": "ok"}}); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "\n  \"ok\": true,") || !strings.Contains(body, "\n    \"status\": \"ok\"") {
		t.Fatalf("JSON response was not indented by two spaces: %q", body)
	}
	if !strings.HasSuffix(body, "\n") {
		t.Fatalf("JSON response has no trailing newline: %q", body)
	}
}

func TestSendAPIResponseOK(t *testing.T) {
	rec := httptest.NewRecorder()
	sendAPIResponseOK(rec, map[string]string{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected default 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !resp.OK {
		t.Error("expected OK=true")
	}
	if resp.Error != "" {
		t.Errorf("expected empty Error, got %q", resp.Error)
	}
}

func TestSendAPIResponseError(t *testing.T) {
	rec := httptest.NewRecorder()
	sendAPIResponseError(rec, "NOT_FOUND", "template not found")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.OK {
		t.Error("expected OK=false")
	}
	if resp.Error != "NOT_FOUND" {
		t.Errorf("Error = %q, want NOT_FOUND", resp.Error)
	}
	if resp.Message != "template not found" {
		t.Errorf("Message = %q, want %q", resp.Message, "template not found")
	}
}

func TestSendAPIResponseErrorLocalizedCode(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Error codes select a translation key rather than using raw text.
	sendAPIResponseErrorLocalized(rec, req, "TOKEN_EXPIRED")

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Message != "Token has expired" {
		t.Errorf("Message = %q, want translated token-expired message", resp.Message)
	}
}

func TestSendAPIResponseErrorLocalizedTranslated(t *testing.T) {
	for _, tc := range []struct {
		code, language, want string
		status               int
	}{
		{"NOT_FOUND", "es", "No se encontró el recurso", http.StatusNotFound},
		{"CSRF_FAILED", "es", "La validación del token CSRF falló", http.StatusForbidden},
		{"VALIDATION_FAILED", "es", "La validación falló", http.StatusBadRequest},
	} {
		t.Run(tc.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/?lang="+tc.language, nil)
			i18n.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sendAPIResponseErrorLocalized(w, r, tc.code)
			})).ServeHTTP(rec, req)
			var resp APIResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if rec.Code != tc.status || resp.Error != tc.code || resp.Message != tc.want {
				t.Errorf("response = status %d, code %q, message %q; want %d, %q, %q", rec.Code, resp.Error, resp.Message, tc.status, tc.code, tc.want)
			}
		})
	}
}

func TestProductionHTTPErrorMessagesAreNotStringLiterals(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate response_test.go")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(currentFile), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Error" {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "http" {
				return true
			}
			if _, translated := call.Args[1].(*ast.CallExpr); !translated {
				t.Errorf("%s: http.Error message must come from a translation call", fset.Position(call.Pos()))
			}
			return true
		})
	}
}

func TestSetCacheHeaders(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"static", "no-cache"},
		{"api", "public, max-age=60"},
		{"authenticated", "private, no-store"},
		{"html", "no-store"},
		{"error", "no-store"},
		{"unknown-kind", "no-store"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			rec := httptest.NewRecorder()
			setCacheHeaders(rec, tc.kind)
			if got := rec.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("setCacheHeaders(%q) Cache-Control = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}
