//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	baseURL = "http://127.0.0.1:64099"
	port    = "64099"
)

var tempDir string

func TestMain(m *testing.M) {
	tempDir = os.Getenv("PROJECT_E2E_TMP")
	if tempDir == "" {
		fmt.Fprintln(os.Stderr, "PROJECT_E2E_TMP must point to an isolated temporary directory")
		os.Exit(2)
	}

	for _, dir := range []string{"config", "data", "logs", "cache", "backup", "home"} {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "create E2E directory: %v\n", err)
			os.Exit(2)
		}
	}

	logFile, err := os.OpenFile(filepath.Join(tempDir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create server log: %v\n", err)
		os.Exit(2)
	}

	command := exec.Command("go", "run", "./src", "--address", "127.0.0.1", "--port", port, "--config", filepath.Join(tempDir, "config"), "--data", filepath.Join(tempDir, "data"), "--log", filepath.Join(tempDir, "logs"), "--cache", filepath.Join(tempDir, "cache"), "--backup", filepath.Join(tempDir, "backup"))
	command.Dir = "../.."
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = append(os.Environ(), "HOME="+filepath.Join(tempDir, "home"), "CONFIG_DIR="+filepath.Join(tempDir, "config"), "DATA_DIR="+filepath.Join(tempDir, "data"), "LOG_DIR="+filepath.Join(tempDir, "logs"), "CACHE_DIR="+filepath.Join(tempDir, "cache"), "BACKUP_DIR="+filepath.Join(tempDir, "backup"))
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start E2E server: %v\n", err)
		os.Exit(2)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- command.Wait() }()

	ready := false
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + "/server/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		select {
		case err := <-serverDone:
			logFile.Close()
			fmt.Fprintf(os.Stderr, "E2E server exited before becoming ready: %v\n", err)
			os.Exit(2)
		default:
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !ready {
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-serverDone
		logFile.Close()
		fmt.Fprintf(os.Stderr, "E2E server did not become ready; inspect %s\n", filepath.Join(tempDir, "server.log"))
		os.Exit(2)
	}

	result := m.Run()
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil && !strings.Contains(err.Error(), "no such process") {
		fmt.Fprintf(os.Stderr, "stop E2E server: %v\n", err)
	}
	select {
	case <-serverDone:
	case <-time.After(10 * time.Second):
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-serverDone
	}
	logFile.Close()
	os.Exit(result)
}

func TestSSRFeaturePages(t *testing.T) {
	pages := []struct {
		path    string
		content string
	}{
		{"/", "GitIgnore Template Server"},
		{"/search?q=python", "Search"},
		{"/template/Go", "Go"},
		{"/combine?templates=Go,Node", "Combine"},
		{"/categories", "Categories"},
		{"/list", "All Templates"},
		{"/stats", "Statistics"},
		{"/docs", "API Documentation"},
		{"/cli", "CLI"},
		{"/server", "Server"},
		{"/server/about", "About"},
		{"/server/privacy", "Privacy"},
		{"/server/contact", "Contact"},
		{"/server/help", "Help"},
		{"/server/terms", "Terms"},
		{"/server/docs/swagger", "Swagger"},
		{"/server/docs/graphql", "GraphiQL"},
	}

	for _, page := range pages {
		t.Run(page.path, func(t *testing.T) {
			response, err := http.Get(baseURL + page.path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s returned %d: %s", page.path, response.StatusCode, body)
			}
			if !strings.Contains(response.Header.Get("Content-Type"), "text/html") {
				t.Errorf("GET %s Content-Type = %q", page.path, response.Header.Get("Content-Type"))
			}
			for _, marker := range []string{"<title>", "<html", "charset", "name=\"viewport\"", page.content} {
				if !strings.Contains(string(body), marker) {
					t.Errorf("GET %s HTML missing %q", page.path, marker)
				}
			}
		})
	}

	response, err := http.Get(baseURL + "/template/not-a-real-template")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("unknown template returned %d; want 404", response.StatusCode)
	}
}

func TestSSRTemplateHasRealContent(t *testing.T) {
	response, err := http.Get(baseURL + "/api/v1/templates/Go")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Go") {
		t.Fatalf("Go template response status %d lacks template content", response.StatusCode)
	}
}

func TestGraphQLQueryAndSchema(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/graphql", strings.NewReader(`{"query":"{ template(name: \"Go\") { name category } }"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"Go"`) {
		t.Fatalf("GraphQL query status %d did not return the Go template: %s", response.StatusCode, body)
	}

	response, err = http.Get(baseURL + "/graphql")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	schema, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(schema), "type Query") || !strings.Contains(string(schema), "totalSizeBytes") {
		t.Fatalf("GraphQL SDL status %d is incomplete: %s", response.StatusCode, schema)
	}
}

func TestSSRStaticAssetsLoad(t *testing.T) {
	response, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	attribute := regexp.MustCompile(`(?:href|src)="(/[^"#]+)"`)
	for _, match := range attribute.FindAllSubmatch(body, -1) {
		path := string(match[1])
		asset, err := http.Get(baseURL + path)
		if err != nil {
			t.Errorf("fetch referenced asset %s: %v", path, err)
			continue
		}
		asset.Body.Close()
		if asset.StatusCode != http.StatusOK {
			t.Errorf("referenced asset %s returned %d", path, asset.StatusCode)
		}
	}
}

func newBrowser(t *testing.T, noJavaScript bool) context.Context {
	t.Helper()
	chromiumPath := "/usr/bin/chromium-browser"
	if _, err := os.Stat(chromiumPath); err != nil {
		chromiumPath = "/usr/bin/chromium"
	}
	options := append(append([]chromedp.ExecAllocatorOption(nil), chromedp.DefaultExecAllocatorOptions...), chromedp.ExecPath(chromiumPath), chromedp.Flag("no-sandbox", true))
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	ctx, cancelTimeout := context.WithTimeout(browser, 30*time.Second)
	var errorMu sync.Mutex
	var browserErrors []string
	chromedp.ListenTarget(ctx, func(event interface{}) {
		errorMu.Lock()
		defer errorMu.Unlock()
		switch event := event.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			if event.Type == cdpruntime.ConsoleAPICalledTypeError {
				browserErrors = append(browserErrors, "console.error")
			}
		case *cdpruntime.EventExceptionThrown:
			browserErrors = append(browserErrors, "uncaught JavaScript exception")
		case *network.EventLoadingFailed:
			browserErrors = append(browserErrors, "failed browser request: "+event.ErrorText)
		}
	})
	t.Cleanup(func() {
		errorMu.Lock()
		if len(browserErrors) > 0 {
			t.Errorf("browser reported errors: %s", strings.Join(browserErrors, ", "))
		}
		errorMu.Unlock()
		if t.Failed() {
			artifacts := filepath.Join(tempDir, "artifacts")
			if err := os.MkdirAll(artifacts, 0o700); err == nil {
				name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())
				var html string
				if chromedp.Run(ctx, chromedp.OuterHTML("html", &html)) == nil {
					os.WriteFile(filepath.Join(artifacts, name+".html"), []byte(html), 0o600)
				}
				var screenshot []byte
				if chromedp.Run(ctx, chromedp.FullScreenshot(&screenshot, 90)) == nil {
					os.WriteFile(filepath.Join(artifacts, name+".png"), screenshot, 0o600)
				}
			}
		}
		cancelTimeout()
		cancelBrowser()
		cancelAllocator()
	})
	if err := chromedp.Run(ctx, cdpruntime.Enable(), network.Enable()); err != nil {
		t.Fatal(err)
	}
	if noJavaScript {
		if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
			t.Fatal(err)
		}
	}
	return ctx
}

func TestNoJSBrowserProgressiveEnhancement(t *testing.T) {
	ctx := newBrowser(t, true)
	var title, text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/"),
		chromedp.Title(&title),
		chromedp.Text("body", &text),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "GitIgnore") || !strings.Contains(text, "templates") {
		t.Fatalf("home page without JavaScript did not render its content: title=%q", title)
	}

	err = chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/search"),
		chromedp.SendKeys(`input[name="q"]`, "python\n"),
		chromedp.WaitVisible("ul.templates li"),
		chromedp.Location(&title),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "/search?q=python") {
		t.Errorf("search form did not submit without JavaScript: current URL %q", title)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/combine"),
		chromedp.SendKeys(`input[name="templates"]`, "Go,Node\n"),
		chromedp.WaitVisible("pre"),
		chromedp.Text("pre", &text),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Go") || !strings.Contains(text, "Node") {
		t.Errorf("template composition form failed without JavaScript: %s", text)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/"),
		chromedp.Click(`form.theme-form button[value="light"]`),
		chromedp.WaitVisible("html.theme-light"),
		chromedp.Evaluate(`document.documentElement.className`, &title),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "theme-light") {
		t.Errorf("theme form did not select light mode without JavaScript: %q", title)
	}
}

func TestFullBrowserTemplateSearchAndComposition(t *testing.T) {
	ctx := newBrowser(t, false)
	var text string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/search?q=python"),
		chromedp.Text("body", &text),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(text), "python") {
		t.Errorf("search page did not render Python results: %s", text)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/combine?templates=Go,Node"),
		chromedp.Text("body", &text),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Go") || !strings.Contains(text, "Node") {
		t.Errorf("composer did not render both selected templates: %s", text)
	}
}

func TestFullBrowserThemesAndMobileLayout(t *testing.T) {
	ctx := newBrowser(t, false)
	var theme, darkColor, lightColor string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/"),
		chromedp.Evaluate(`document.documentElement.className`, &theme),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue("--bg")`, &darkColor),
		chromedp.Click(".theme-button"),
		chromedp.Evaluate(`document.documentElement.className`, &theme),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue("--bg")`, &lightColor),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(theme, "theme-light") || darkColor == lightColor {
		t.Errorf("JavaScript theme toggle did not change theme styles: theme=%q dark=%q light=%q", theme, darkColor, lightColor)
	}

	var overflow, navigationVisible bool
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(375, 812, 1, false),
		chromedp.Evaluate(`document.documentElement.scrollWidth > window.innerWidth`, &overflow),
		chromedp.Visible(".nav", &navigationVisible),
	); err != nil {
		t.Fatal(err)
	}
	if overflow || !navigationVisible {
		t.Errorf("mobile layout is unusable: horizontal overflow=%t navigation visible=%t", overflow, navigationVisible)
	}
}
