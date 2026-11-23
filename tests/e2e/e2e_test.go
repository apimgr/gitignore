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

	"github.com/chromedp/cdproto/cdp"
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
	configDir := filepath.Join(tempDir, "config", "yaml.d")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "create E2E config directory: %v\n", err)
		os.Exit(2)
	}
	configPath := filepath.Join(configDir, "security.yml")
	if err := os.WriteFile(configPath, []byte("server:\n  rate_limit:\n    enabled: true\n    requests: 10000\n    window: 60\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "create E2E config: %v\n", err)
		os.Exit(2)
	}

	logFile, err := os.OpenFile(filepath.Join(tempDir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create server log: %v\n", err)
		os.Exit(2)
	}

	serverArgs := []string{"--address", "127.0.0.1", "--port", port, "--config", filepath.Join(tempDir, "config"), "--data", filepath.Join(tempDir, "data"), "--log", filepath.Join(tempDir, "logs"), "--cache", filepath.Join(tempDir, "cache"), "--backup", filepath.Join(tempDir, "backup")}
	command := exec.Command("go", append([]string{"run", "./src"}, serverArgs...)...)
	if serverBinary := os.Getenv("PROJECT_E2E_SERVER_BINARY"); serverBinary != "" {
		command = exec.Command(serverBinary, serverArgs...)
	}
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
		{"/server", "About"},
		{"/server/about", "About"},
		{"/server/privacy", "Privacy"},
		{"/server/contact", "Contact"},
		{"/server/help", "Help"},
		{"/server/terms", "Terms"},
		{"/server/docs/swagger", "API Documentation"},
		{"/server/docs/graphql", "GraphQL"},
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

func TestGraphiQLRTLDoesNotOverflow(t *testing.T) {
	ctx := newBrowser(t, false)
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(320, 640),
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			_, err := cdp.Call(ctx, target, network.SetCookie, network.SetCookieParams{Name: "lang", Value: "ar", URL: baseURL})
			return err
		}),
		chromedp.Navigate(baseURL+"/server/docs/graphql"),
		chromedp.WaitVisible("#graphiql .graphiql-container"),
		chromedp.Sleep(time.Second),
	); err != nil {
		t.Fatal(err)
	}
	diagnostic, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const offenders = Array.from(document.querySelectorAll("body *"))
			.map(element => ({element, rect: element.getBoundingClientRect()}))
			.filter(({element, rect}) => rect.width > innerWidth + 1 || rect.right > innerWidth + 1 || rect.left < -1 || element.scrollWidth > element.clientWidth + 100)
			.sort((a, b) => Math.max(b.rect.width, b.element.scrollWidth) - Math.max(a.rect.width, a.element.scrollWidth))
			.slice(0, 12)
			.map(({element, rect}) => element.tagName.toLowerCase() + "." + String(element.className).replace(/\s+/g, ".") + " rect=" + Math.round(rect.left) + ":" + Math.round(rect.right) + " size=" + element.clientWidth + "/" + element.scrollWidth + " pos=" + getComputedStyle(element).position + " left=" + getComputedStyle(element).left + " dir=" + getComputedStyle(element).direction + " parent=" + element.parentElement.className + " display=" + getComputedStyle(element).display + " visibility=" + getComputedStyle(element).visibility);
		return "root=" + document.documentElement.clientWidth + "/" + document.documentElement.scrollWidth + " body=" + document.body.clientWidth + "/" + document.body.scrollWidth + "\n" + offenders.join("\n");
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(diagnostic, "root=") && strings.Contains(diagnostic, " body=") {
		var rootWidth, viewportWidth int
		if _, err := fmt.Sscanf(diagnostic, "root=%d/%d", &viewportWidth, &rootWidth); err == nil && rootWidth > viewportWidth {
			t.Errorf("GraphiQL RTL page overflows: %s", diagnostic)
		}
	}
}

func TestSwaggerUILayoutControls(t *testing.T) {
	ctx := newBrowser(t, false)
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(320, 640),
		chromedp.Navigate(baseURL+"/server/docs/swagger"),
		chromedp.WaitVisible("#swagger-ui .info"),
	); err != nil {
		t.Fatal(err)
	}
	diagnostic, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const headings = document.querySelectorAll("main h1");
		const smallTargets = Array.from(document.querySelectorAll("#swagger-ui a[href], #swagger-ui button, #swagger-ui [role=button]"))
			.filter(target => {
				const box = target.getBoundingClientRect();
				return getComputedStyle(target).display !== "none" && (box.width < 44 || box.height < 44);
			})
			.slice(0, 10)
			.map(target => target.tagName.toLowerCase() + " " + target.textContent.trim().slice(0, 24) + " " + Math.round(target.getBoundingClientRect().width) + "x" + Math.round(target.getBoundingClientRect().height));
		return "h1=" + headings.length + " small_targets=" + smallTargets.join(", ");
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(diagnostic, "h1=1 ") || strings.TrimPrefix(diagnostic, "h1=1 small_targets=") != "" {
		t.Errorf("Swagger UI layout is not accessible at 320px: %s", diagnostic)
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

	response, err = http.Post(baseURL+"/api/graphql", "application/json", strings.NewReader(`{"query":"{ __schema { types { name fields { name } } } }"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	schema, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(schema), `"name":"Query"`) || !strings.Contains(string(schema), `"name":"totalSizeBytes"`) {
		t.Fatalf("GraphQL introspection status %d is incomplete: %s", response.StatusCode, schema)
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
	return newBrowserWithTimeout(t, noJavaScript, 30*time.Second)
}

func newBrowserWithTimeout(t *testing.T, noJavaScript bool, timeout time.Duration) context.Context {
	t.Helper()
	chromiumPath := "/usr/bin/chromium-browser"
	if _, err := os.Stat(chromiumPath); err != nil {
		chromiumPath = "/usr/bin/chromium"
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.ExecPath(chromiumPath), chromedp.Flag("no-sandbox", true))
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	ctx, cancelTimeout := context.WithTimeout(browser, timeout)
	var errorMu sync.Mutex
	var eventCollectors sync.WaitGroup
	var browserErrors []string
	eventContext, cancelEvents := context.WithCancel(ctx)
	t.Cleanup(func() {
		cancelEvents()
		eventCollectors.Wait()
		errorMu.Lock()
		if len(browserErrors) > 0 {
			t.Errorf("browser reported errors: %s", strings.Join(browserErrors, ", "))
		}
		errorMu.Unlock()
		if t.Failed() {
			artifacts := filepath.Join(tempDir, "artifacts")
			if err := os.MkdirAll(artifacts, 0o700); err == nil {
				name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())
				if html, err := chromedp.Run(ctx, chromedp.OuterHTML("html")); err == nil {
					os.WriteFile(filepath.Join(artifacts, name+".html"), []byte(html), 0o600)
				}
				if screenshot, err := chromedp.Run(ctx, chromedp.FullScreenshot(90)); err == nil {
					os.WriteFile(filepath.Join(artifacts, name+".png"), screenshot, 0o600)
				}
			}
		}
		cancelTimeout()
		cancelBrowser()
		cancelAllocator()
	})
	if err := chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
		if _, err := cdp.Call(ctx, target, cdpruntime.Enable, cdp.Empty{}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, target, network.Enable, network.EnableParams{})
		return err
	})); err != nil {
		t.Fatal(err)
	}
	eventCollectors.Add(2)
	go func() {
		defer eventCollectors.Done()
		for message, err := range chromedp.Console(eventContext) {
			if err != nil {
				return
			}
			if message.Type == chromedp.ConsoleError || message.IsException() {
				text := message.String()
				if strings.Contains(text, "Failed to load resource: the server responded with a status of 404") || strings.Contains(text, "Loading the font 'data:font/") {
					continue
				}
				errorMu.Lock()
				browserErrors = append(browserErrors, "browser console: "+text)
				errorMu.Unlock()
			}
		}
	}()
	go func() {
		defer eventCollectors.Done()
		for event, err := range chromedp.Events(eventContext, network.LoadingFailed) {
			if err != nil {
				return
			}
			errorMu.Lock()
			if event.ErrorText != "" && event.ErrorText != "net::ERR_BLOCKED_BY_CLIENT" {
				browserErrors = append(browserErrors, "failed browser request: "+event.ErrorText)
			}
			errorMu.Unlock()
		}
	}()
	if noJavaScript {
		if err := chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			_, err := cdp.Call(ctx, target, emulation.SetScriptExecutionDisabled, emulation.SetScriptExecutionDisabledParams{Value: true})
			return err
		})); err != nil {
			t.Fatal(err)
		}
	}
	return ctx
}

func TestNoJSBrowserProgressiveEnhancement(t *testing.T) {
	ctx := newBrowser(t, true)
	if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+"/")); err != nil {
		t.Fatal(err)
	}
	title, err := chromedp.Run(ctx, chromedp.Title())
	if err != nil {
		t.Fatal(err)
	}
	text, err := chromedp.Run(ctx, chromedp.Text("body"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "GitIgnore") || !strings.Contains(text, "templates") {
		t.Fatalf("home page without JavaScript did not render its content: title=%q", title)
	}

	err = chromedp.Do(ctx,
		chromedp.Navigate(baseURL+"/search"),
		chromedp.SendKeys(`input[name="q"]`, "python"),
		chromedp.Submit(`input[name="q"]`),
		chromedp.WaitVisible("ul.templates li"),
	)
	if err != nil {
		t.Fatal(err)
	}
	title, err = chromedp.Run(ctx, chromedp.Location())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "/search?q=python") {
		t.Errorf("search form did not submit without JavaScript: current URL %q", title)
	}

	if err := chromedp.Do(ctx,
		chromedp.Navigate(baseURL+"/combine"),
		chromedp.SendKeys(`input[name="templates"]`, "Go,Node"),
		chromedp.Submit(`input[name="templates"]`),
		chromedp.WaitVisible("main pre"),
	); err != nil {
		t.Fatal(err)
	}
	text, err = chromedp.Run(ctx, chromedp.Text("pre"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Go") || !strings.Contains(text, "Node") {
		t.Errorf("template composition form failed without JavaScript: %s", text)
	}

	if err := chromedp.Do(ctx,
		chromedp.Navigate(baseURL+"/"),
		chromedp.Click(".theme-button"),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Navigate(baseURL+"/"),
		chromedp.WaitVisible("html.theme-light"),
	); err != nil {
		t.Fatal(err)
	}
	title, err = chromedp.Run(ctx, chromedp.Evaluate[string](`document.documentElement.className`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "theme-light") {
		t.Errorf("theme form did not select light mode without JavaScript: %q", title)
	}

	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(320, 640),
		chromedp.Navigate(baseURL+"/"),
		chromedp.Click(".nav-toggle"),
	); err != nil {
		t.Fatalf("open mobile navigation without JavaScript: %v", err)
	}
	checked, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector(".nav-checkbox").checked`))
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("mobile navigation toggle did not open the drawer without JavaScript")
	}
	if err := chromedp.Do(ctx,
		chromedp.Click(".nav-overlay"),
	); err != nil {
		t.Fatalf("close mobile navigation with scrim without JavaScript: %v", err)
	}
	checked, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector(".nav-checkbox").checked`))
	if err != nil {
		t.Fatal(err)
	}
	if checked {
		t.Fatal("scrim did not close the drawer without JavaScript")
	}
	if err := chromedp.Do(ctx,
		chromedp.Click(".nav-toggle"),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Click(".nav-close"),
	); err != nil {
		t.Fatalf("close mobile navigation with close control without JavaScript: %v", err)
	}
	checked, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector(".nav-checkbox").checked`))
	if err != nil {
		t.Fatal(err)
	}
	if checked {
		t.Fatal("close control did not close the drawer without JavaScript")
	}
}

func TestFullBrowserTemplateSearchAndComposition(t *testing.T) {
	ctx := newBrowser(t, false)
	if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+"/search?q=python")); err != nil {
		t.Fatal(err)
	}
	text, err := chromedp.Run(ctx, chromedp.Text("body"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(text), "python") {
		t.Errorf("search page did not render Python results: %s", text)
	}

	if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+"/combine?templates=Go,Node")); err != nil {
		t.Fatal(err)
	}
	text, err = chromedp.Run(ctx, chromedp.Text("body"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Go") || !strings.Contains(text, "Node") {
		t.Errorf("composer did not render both selected templates: %s", text)
	}
}

func TestFullBrowserThemesAndMobileLayout(t *testing.T) {
	ctx := newBrowser(t, false)
	if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+"/")); err != nil {
		t.Fatal(err)
	}
	theme, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.documentElement.className`))
	if err != nil {
		t.Fatal(err)
	}
	darkColor, err := chromedp.Run(ctx, chromedp.Evaluate[string](`getComputedStyle(document.documentElement).getPropertyValue("--bg")`))
	if err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Do(ctx, chromedp.Click(".theme-button")); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Do(ctx, chromedp.Sleep(250*time.Millisecond), chromedp.Navigate(baseURL+"/")); err != nil {
		t.Fatal(err)
	}
	theme, err = chromedp.Run(ctx, chromedp.Evaluate[string](`document.documentElement.className`))
	if err != nil {
		t.Fatal(err)
	}
	lightColor, err := chromedp.Run(ctx, chromedp.Evaluate[string](`getComputedStyle(document.documentElement).getPropertyValue("--bg")`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(theme, "theme-light") || darkColor == lightColor {
		t.Errorf("JavaScript theme toggle did not change theme styles: theme=%q dark=%q light=%q", theme, darkColor, lightColor)
	}

	if err := chromedp.Do(ctx, chromedp.EmulateViewport(375, 812)); err != nil {
		t.Fatal(err)
	}
	overflow, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.documentElement.scrollWidth > window.innerWidth`))
	if err != nil {
		t.Fatal(err)
	}
	navigationVisible, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector(".nav-panel").getClientRects().length > 0`))
	if err != nil {
		t.Fatal(err)
	}
	if overflow || !navigationVisible {
		t.Errorf("mobile layout is unusable: horizontal overflow=%t navigation visible=%t", overflow, navigationVisible)
	}
}

func TestLayoutReviewMatrixAllPages(t *testing.T) {
	ctx := newBrowserWithTimeout(t, false, 5*time.Minute)
	viewports := []struct {
		width  int
		height int
	}{
		{320, 640}, {375, 812}, {390, 844}, {768, 1024}, {820, 1180},
		{1024, 768}, {1280, 800}, {1440, 900}, {1920, 1080},
	}
	themes := []string{"dark", "light", "auto"}
	languages := []string{"en", "ar"}
	pages := []string{
		"/", "/search", "/search?q=python", "/template/Go", "/combine?templates=Go,Node",
		"/categories", "/list", "/stats", "/docs", "/cli", "/server", "/server/about",
		"/server/privacy", "/server/contact", "/server/contact?type=abuse", "/server/contact?type=abuse&sent=1", "/server/help",
		"/server/help/mcp", "/server/preferences", "/server/preferences/sync",
		"/server/preferences/tokens", "/server/terms", "/server/dpo", "/server/security",
		"/server/security/policy", "/server/security/thanks", "/server/security/report/invalid",
		"/server/docs/swagger", "/server/docs/graphql", "/server/healthz",
	}
	layoutCheck := `(() => {
		const failures = [];
		const root = document.documentElement;
		if (root.scrollWidth > window.innerWidth) {
			const offenders = Array.from(document.body.querySelectorAll("*"))
				.map(element => ({element, rect: element.getBoundingClientRect()}))
				.filter(({rect}) => rect.width > window.innerWidth + 1 || rect.right > window.innerWidth + 1 || rect.left < -1)
				.sort((a, b) => b.rect.right - a.rect.right)
				.slice(0, 3)
				.map(({element, rect}) => element.tagName.toLowerCase() + "." + String(element.className).replace(/\s+/g, ".") + ":" + Math.round(rect.left) + "-" + Math.round(rect.right));
			failures.push("horizontal overflow " + root.scrollWidth + "/" + window.innerWidth + " " + offenders.join(","));
		}
		if (root.getAttribute("dir") !== __DIR__) failures.push("wrong text direction");
		if (new URLSearchParams(location.search).get("sent") === "1") {
			const flash = document.querySelector('.alert-success[role="status"]');
			if (!flash || !flash.textContent.trim()) failures.push("success flash is missing or empty");
		}
		const visible = element => {
			if (element.matches(".skip-link") && document.activeElement !== element) return false;
			if (element.closest('[aria-hidden="true"]')) return false;
			const style = getComputedStyle(element);
			return style.display !== "none" && style.visibility !== "hidden" && element.getClientRects().length > 0;
		};
		const clippedByAncestor = (element, box) => {
			for (let ancestor = element.parentElement; ancestor && ancestor !== document.body; ancestor = ancestor.parentElement) {
				const style = getComputedStyle(ancestor);
				if (!/(auto|scroll|hidden|clip)/.test(style.overflowX)) continue;
				const clip = ancestor.getBoundingClientRect();
				if (box.left < clip.left || box.right > clip.right) return true;
			}
			return false;
		};
		for (const element of document.body.querySelectorAll("*")) {
			if (!visible(element)) continue;
			const box = element.getBoundingClientRect();
			if ((box.left < -1 || box.right > window.innerWidth + 1) && !clippedByAncestor(element, box)) {
				failures.push("visible element exceeds viewport: " + element.tagName.toLowerCase() + "." + String(element.className).replace(/\s+/g, ".") + " " + Math.round(box.left) + "-" + Math.round(box.right));
			}
		}
		for (const select of document.querySelectorAll("select")) {
			if (select.options.length === 0) failures.push("select without options");
		}
		for (const control of document.querySelectorAll("input:not([type=hidden]), select, textarea")) {
			if (!control.labels || control.labels.length === 0) {
				if (!control.hasAttribute("aria-label") && !control.hasAttribute("aria-labelledby")) failures.push("unlabeled form control");
			}
		}
		const headings = document.querySelectorAll("main h1");
		if (headings.length !== 1) failures.push("page must have exactly one h1");
		const targets = document.querySelectorAll("a[href], button, input:not([type=hidden]), select, textarea:not(.CodeMirror textarea), summary, [role=button]");
		for (const target of targets) {
			if (window.innerWidth > 820) continue;
			if (!visible(target)) continue;
			if (target.matches("a[href]") && target.closest("p, td, th, .cookie-message")) continue;
			let box = target.getBoundingClientRect();
			if (target.matches("input[type=checkbox], input[type=radio]") && target.labels && target.labels.length) box = target.labels[0].getBoundingClientRect();
			if (box.width < 44 || box.height < 44) failures.push("small touch target: " + target.tagName.toLowerCase() + " " + target.textContent.trim().slice(0, 24) + " " + Math.round(box.width) + "x" + Math.round(box.height));
		}
		return failures.slice(0, 20).join("; ");
	})()`
	for _, viewport := range viewports {
		if err := chromedp.Do(ctx, chromedp.EmulateViewport(int64(viewport.width), int64(viewport.height))); err != nil {
			t.Fatalf("set viewport %dx%d: %v", viewport.width, viewport.height, err)
		}
		for _, theme := range themes {
			if err := chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
				_, err := cdp.Call(ctx, target, network.SetCookie, network.SetCookieParams{Name: "theme", Value: theme, URL: baseURL})
				return err
			})); err != nil {
				t.Fatalf("set theme %q: %v", theme, err)
			}
			for _, language := range languages {
				direction := "ltr"
				if language == "ar" {
					direction = "rtl"
				}
				if err := chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
					_, err := cdp.Call(ctx, target, network.SetCookie, network.SetCookieParams{Name: "lang", Value: language, URL: baseURL})
					return err
				})); err != nil {
					t.Fatalf("set language %q: %v", language, err)
				}
				for _, page := range pages {
					var failures string
					script := strings.ReplaceAll(layoutCheck, "__DIR__", `"`+direction+`"`)
					if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+page)); err != nil {
						t.Fatalf("layout matrix page %s at %dx%d theme=%s lang=%s: %v", page, viewport.width, viewport.height, theme, language, err)
					}
					failures, err := chromedp.Run(ctx, chromedp.Evaluate[string](script))
					if err != nil {
						t.Fatalf("layout matrix page %s at %dx%d theme=%s lang=%s: %v", page, viewport.width, viewport.height, theme, language, err)
					}
					if failures != "" {
						t.Errorf("layout matrix page %s at %dx%d theme=%s lang=%s: %s", page, viewport.width, viewport.height, theme, language, failures)
					}
				}
			}
		}
	}

	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(320, 640),
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			_, err := cdp.Call(ctx, target, network.SetCookie, network.SetCookieParams{Name: "lang", Value: "ar", URL: baseURL})
			return err
		}),
		chromedp.Navigate(baseURL+"/"),
		chromedp.Click(".nav-toggle"),
	); err != nil {
		t.Fatalf("open Arabic mobile navigation: %v", err)
	}
	drawerFailures, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const failures = [];
		const panel = document.querySelector(".nav-panel");
		const scrim = document.querySelector(".nav-overlay");
		const panelBox = panel.getBoundingClientRect();
		const scrimBox = scrim.getBoundingClientRect();
		const desktopLinks = Array.from(document.querySelectorAll(".nav-links a"), link => link.getAttribute("href"));
		const drawerLinks = Array.from(panel.querySelectorAll("a"), link => link.getAttribute("href"));
		if (panelBox.height < window.innerHeight - 1 || panelBox.top > 1 || panelBox.bottom < window.innerHeight - 1) failures.push("drawer does not cover the viewport height");
		if (scrimBox.width < window.innerWidth - 1 || scrimBox.height < window.innerHeight - 1 || scrimBox.left > 1 || scrimBox.top > 1) failures.push("scrim does not cover the viewport");
		if (JSON.stringify(desktopLinks) !== JSON.stringify(drawerLinks)) failures.push("drawer links differ from desktop navigation order");
		if (panel.querySelectorAll("a").length === 0 || Array.from(panel.querySelectorAll("a")).some(link => !link.getClientRects().length)) failures.push("drawer navigation links are not visible");
		if (getComputedStyle(document.body).overflow !== "hidden") failures.push("background page is still scrollable");
		if (getComputedStyle(panel).backgroundColor === "rgba(0, 0, 0, 0)") failures.push("drawer surface is transparent");
		if (Number(getComputedStyle(panel).zIndex) <= Number(getComputedStyle(scrim).zIndex)) failures.push("drawer is not above its scrim");
		if (!panel.contains(document.activeElement)) failures.push("opening the drawer did not move focus inside");
		return failures.join("; ");
	})()`))
	if err != nil {
		t.Fatalf("inspect open mobile drawer: %v", err)
	}
	if drawerFailures != "" {
		t.Errorf("open mobile drawer: %s", drawerFailures)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.dispatchEvent(new KeyboardEvent("keydown", {key:"Escape", bubbles:true}))`)); err != nil {
		t.Fatalf("close drawer with Escape: %v", err)
	}
	closedByEscape, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!document.querySelector(".nav-checkbox").checked && document.activeElement === document.querySelector(".nav-toggle")`))
	if err != nil {
		t.Fatalf("check Escape close: %v", err)
	}
	if !closedByEscape {
		t.Error("Escape did not close the drawer and return focus to its toggle")
	}
	if err := chromedp.Do(ctx,
		chromedp.Click(".nav-toggle"),
		chromedp.Click(".nav-overlay"),
	); err != nil {
		t.Fatalf("close drawer with scrim: %v", err)
	}
	closedByEscape, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`!document.querySelector(".nav-checkbox").checked`))
	if err != nil {
		t.Fatalf("check scrim close: %v", err)
	}
	if !closedByEscape {
		t.Error("scrim click did not close the drawer")
	}
}

func TestLayoutStressValuesAndUIStates(t *testing.T) {
	ctx := newBrowserWithTimeout(t, false, 60*time.Second)
	if err := chromedp.Do(ctx, chromedp.EmulateViewport(320, 640), chromedp.Navigate(baseURL+"/search?q=__e2e_empty_search_9f8c__")); err != nil {
		t.Fatal(err)
	}
	emptyCount, err := chromedp.Run(ctx, chromedp.Evaluate[int](`document.querySelectorAll("ul.templates li").length`))
	if err != nil {
		t.Fatal(err)
	}
	if emptyCount != 0 {
		t.Errorf("empty search rendered %d results", emptyCount)
	}
	if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+"/template/not-a-real-template")); err != nil {
		t.Fatal(err)
	}
	errorState, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector("h1")?.textContent.trim() === "not-a-real-template" && document.querySelector("main")?.textContent.includes("Template not found.")`))
	if err != nil {
		t.Fatal(err)
	}
	if !errorState {
		t.Error("unknown template did not render the error state")
	}
	if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+"/server/preferences")); err != nil {
		t.Fatal(err)
	}
	disabledState, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`Boolean(document.querySelector("input:disabled"))`))
	if err != nil {
		t.Fatal(err)
	}
	if !disabledState {
		t.Error("preferences page did not expose its disabled essential-cookie control")
	}
	if err := chromedp.Do(ctx, chromedp.Navigate(baseURL+"/list")); err != nil {
		t.Fatal(err)
	}
	stressState, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
			const list = document.querySelector("ul.templates");
			if (!list) return "missing list";
			const onion = "a".repeat(56) + ".onion";
			const token = "t".repeat(64);
			const name = "n".repeat(64);
			const longTranslation = "واجهة المستخدم متعددة اللغات ".repeat(12);
			for (const count of [0, 1, 25]) {
				list.replaceChildren();
				for (let i = 0; i < count; i++) {
					const item = document.createElement("li");
					const link = document.createElement("a");
					link.href = "#stress-" + i;
					link.textContent = [onion, token, name, longTranslation, String(i)].join(" ");
					item.appendChild(link);
					list.appendChild(item);
				}
				if (list.children.length !== count) return "wrong list count: " + count;
				document.documentElement.style.fontSize = "200%";
				if (document.documentElement.scrollWidth > window.innerWidth) {
					const overflow = Array.from(document.querySelectorAll("body *"))
						.map(element => ({element, box: element.getBoundingClientRect()}))
						.filter(item => !item.element.closest(".nav-panel") && (item.box.right > window.innerWidth + 1 || item.element.scrollWidth > item.element.clientWidth + 1))
						.sort((left, right) => right.element.scrollWidth - right.element.clientWidth - (left.element.scrollWidth - left.element.clientWidth))
						.slice(0, 12)
						.map(element => {
							const node = element.element;
							const box = element.box;
							return node.tagName.toLowerCase() + (typeof node.className === "string" && node.className ? "." + node.className : "") + ":" + Math.round(box.left) + "-" + Math.round(box.right) + " width=" + Math.round(box.width) + " scroll=" + node.clientWidth + "/" + node.scrollWidth + " overflow=" + getComputedStyle(node).overflow;
						});
					return "horizontal overflow with " + count + " items at 200% zoom (" + document.documentElement.scrollWidth + "/" + window.innerWidth + ", html=" + document.documentElement.clientWidth + ", body=" + document.body.clientWidth + "): " + overflow.join(", ");
				}
			}
			return "";
		})()`))
	if err != nil {
		t.Fatal(err)
	}
	if stressState != "" {
		t.Errorf("stress content layout: %s", stressState)
	}
	loaded, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.readyState === "complete" && document.querySelectorAll('[aria-busy="true"]').length === 0`))
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.Error("page remained in a loading state after navigation completed")
	}
}
