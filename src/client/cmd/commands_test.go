package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/output"
)

func newTestClientAndPrinter(t *testing.T, handler http.HandlerFunc) *api.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return api.New(srv.URL)
}

func TestCmdListSuccessAndError(t *testing.T) {
	p := output.New(false)

	t.Run("success", func(t *testing.T) {
		c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": []string{"Node", "Go"}})
		})
		if got := CmdList(c, p, "text"); got != output.ExitSuccess {
			t.Errorf("CmdList() = %d, want ExitSuccess", got)
		}
	})

	t.Run("server error maps to general exit", func(t *testing.T) {
		c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "boom"})
		})
		if got := CmdList(c, p, "text"); got != output.ExitGeneral {
			t.Errorf("CmdList() with 500 = %d, want ExitGeneral", got)
		}
	})
}

func TestCmdSearchEmptyQueryReturnsUsage(t *testing.T) {
	p := output.New(false)
	c := api.New("http://unused.invalid")
	if got := CmdSearch(c, p, "text", "   "); got != output.ExitUsage {
		t.Errorf("CmdSearch(empty query) = %d, want ExitUsage", got)
	}
}

func TestCmdSearchSuccess(t *testing.T) {
	p := output.New(false)
	c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": []string{"Go"}})
	})
	if got := CmdSearch(c, p, "json", "go"); got != output.ExitSuccess {
		t.Errorf("CmdSearch() = %d, want ExitSuccess", got)
	}
}

func TestCmdCategoriesSuccess(t *testing.T) {
	p := output.New(false)
	c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": []string{"Root", "Global"}})
	})
	if got := CmdCategories(c, p, "table"); got != output.ExitSuccess {
		t.Errorf("CmdCategories() = %d, want ExitSuccess", got)
	}
}

func TestCmdCategoryEmptyNameReturnsUsage(t *testing.T) {
	p := output.New(false)
	c := api.New("http://unused.invalid")
	if got := CmdCategory(c, p, "text", ""); got != output.ExitUsage {
		t.Errorf("CmdCategory(empty) = %d, want ExitUsage", got)
	}
}

func TestCmdCategoryNotFound(t *testing.T) {
	p := output.New(false)
	c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "no such category"})
	})
	if got := CmdCategory(c, p, "text", "Bogus"); got != output.ExitNotFound {
		t.Errorf("CmdCategory(not found) = %d, want ExitNotFound", got)
	}
}

func TestCmdStatsTextAndJSON(t *testing.T) {
	c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   true,
			"data": map[string]interface{}{"total_templates": float64(10)},
		})
	})
	p := output.New(false)
	if got := CmdStats(c, p, "text"); got != output.ExitSuccess {
		t.Errorf("CmdStats(text) = %d, want ExitSuccess", got)
	}
	if got := CmdStats(c, p, "json"); got != output.ExitSuccess {
		t.Errorf("CmdStats(json) = %d, want ExitSuccess", got)
	}
}

func TestCmdGetTemplateEmptyNameReturnsUsage(t *testing.T) {
	p := output.New(false)
	c := api.New("http://unused.invalid")
	if got := CmdGetTemplate(c, p, "text", ""); got != output.ExitUsage {
		t.Errorf("CmdGetTemplate(empty) = %d, want ExitUsage", got)
	}
}

func TestCmdGetTemplateSuccess(t *testing.T) {
	c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   true,
			"data": api.Template{Name: "Go", Content: "*.o"},
		})
	})
	p := output.New(false)
	if got := CmdGetTemplate(c, p, "text", "Go"); got != output.ExitSuccess {
		t.Errorf("CmdGetTemplate() = %d, want ExitSuccess", got)
	}
	if got := CmdGetTemplate(c, p, "json", "Go"); got != output.ExitSuccess {
		t.Errorf("CmdGetTemplate(json) = %d, want ExitSuccess", got)
	}
}

func TestCmdCombineEmptyNamesReturnsUsage(t *testing.T) {
	p := output.New(false)
	c := api.New("http://unused.invalid")
	if got := CmdCombine(c, p, "text", nil); got != output.ExitUsage {
		t.Errorf("CmdCombine(empty) = %d, want ExitUsage", got)
	}
}

func TestCmdCombineSuccess(t *testing.T) {
	c := newTestClientAndPrinter(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": "*.o\n"})
	})
	p := output.New(false)
	if got := CmdCombine(c, p, "text", []string{"Go"}); got != output.ExitSuccess {
		t.Errorf("CmdCombine() = %d, want ExitSuccess", got)
	}
	if got := CmdCombine(c, p, "json", []string{"Go"}); got != output.ExitSuccess {
		t.Errorf("CmdCombine(json) = %d, want ExitSuccess", got)
	}
}

func TestHandleAPIErrorMapsStatusCodes(t *testing.T) {
	p := output.New(false)
	cases := []struct {
		err  error
		want int
	}{
		{&api.APIError{Status: 404, Message: "nf"}, output.ExitNotFound},
		{&api.APIError{Status: 401, Message: "auth"}, output.ExitAuth},
		{&api.APIError{Status: 403, Message: "forbidden"}, output.ExitAuth},
		{&api.APIError{Status: 500, Message: "boom"}, output.ExitGeneral},
	}
	for _, tc := range cases {
		if got := handleAPIError(tc.err, p); got != tc.want {
			t.Errorf("handleAPIError(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestHandleAPIErrorConnectionFailure(t *testing.T) {
	p := output.New(false)
	c := api.New("http://127.0.0.1:1")
	_, err := c.List()
	if err == nil {
		t.Fatal("expected connection error")
	}
	if got := handleAPIError(err, p); got != output.ExitConnection {
		t.Errorf("handleAPIError(connection failure) = %d, want ExitConnection", got)
	}
}

func TestPrintHelpAndVersion(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	PrintHelp("1.2.3")
	PrintVersion("1.2.3", "abc123", "today", nil)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	var output bytes.Buffer
	if _, err := io.Copy(&output, r); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gitignore-cli 1.2.3", "Commands:", "--server URL", "gitignore-cli 1.2.3 (abc123) built today"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("help/version output missing %q", want)
		}
	}
}
