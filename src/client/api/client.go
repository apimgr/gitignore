// Package api implements the HTTP client used by gitignore-cli to talk to
// this project's own /api/v1/* endpoints. All endpoints are public and
// unauthenticated (see IDEA.md: "No user accounts, registration, or login
// of any kind") so this client carries no token/auth machinery.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/urlutil"
)

// ProjectName is compiled in via -ldflags and used only to build the
// User-Agent header — it must stay fixed regardless of what the binary is
// renamed to on disk (see AI.md PART 32 "HTTP Client Identity").
var ProjectName = "gitignore"

// Version is compiled in via -ldflags.
var Version = "dev"

// UserAgent returns the fixed User-Agent header for all API requests.
func UserAgent() string {
	return fmt.Sprintf("%s-cli/%s", ProjectName, Version)
}

// Client talks to the gitignore API server.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New creates a Client for baseURL (trailing slash trimmed).
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Template mirrors the JSON shape of src/template.Template as served by
// the API. Kept as a small client-side copy so this package has no
// compile-time dependency on the server's internal packages.
type Template struct {
	Name        string   `json:"name"`
	FileName    string   `json:"file_name"`
	Category    string   `json:"category"`
	Content     string   `json:"content,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Size        int      `json:"size"`
}

// envelope mirrors the {"success": true, "data": ...} JSON contract
// implemented by src/template/handlers.go for every JSON-negotiated
// response.
type envelope struct {
	Success   bool            `json:"success"`
	Data      json.RawMessage `json:"data"`
	Count     int             `json:"count,omitempty"`
	Query     string          `json:"query,omitempty"`
	Category  string          `json:"category,omitempty"`
	Templates []string        `json:"templates,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// APIError is returned for non-2xx HTTP responses; Status carries the HTTP
// status code so callers can map it to a CLI exit code.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("server returned %d: %s", e.Status, e.Message)
}

func (c *Client) get(path string, pathParams, queryParams map[string]string) (*envelope, error) {
	apiURL := urlutil.BuildAPIURL(c.BaseURL, path, pathParams, queryParams)
	if apiURL == "" {
		return nil, fmt.Errorf("invalid server URL: %s", c.BaseURL)
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var env envelope
	if len(body) > 0 {
		_ = json.Unmarshal(body, &env)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := env.Error
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, &APIError{Status: resp.StatusCode, Message: msg}
	}

	return &env, nil
}

// List returns all template names.
func (c *Client) List() ([]string, error) {
	env, err := c.get("/api/v1/list", nil, nil)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(env.Data, &names); err != nil {
		return nil, fmt.Errorf("decoding list response: %w", err)
	}
	return names, nil
}

// Search returns template names matching q.
func (c *Client) Search(q string) ([]string, error) {
	env, err := c.get("/api/v1/search", nil, map[string]string{"q": q})
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(env.Data, &names); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}
	return names, nil
}

// Categories returns all category names.
func (c *Client) Categories() ([]string, error) {
	env, err := c.get("/api/v1/categories", nil, nil)
	if err != nil {
		return nil, err
	}
	var cats []string
	if err := json.Unmarshal(env.Data, &cats); err != nil {
		return nil, fmt.Errorf("decoding categories response: %w", err)
	}
	return cats, nil
}

// CategoryTemplates returns template names in the given category.
func (c *Client) CategoryTemplates(name string) ([]string, error) {
	env, err := c.get("/api/v1/category/{name}", map[string]string{"name": name}, nil)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(env.Data, &names); err != nil {
		return nil, fmt.Errorf("decoding category response: %w", err)
	}
	return names, nil
}

// GetTemplate fetches a single named template.
func (c *Client) GetTemplate(name string) (*Template, error) {
	env, err := c.get("/api/v1/template/{name}", map[string]string{"name": name}, nil)
	if err != nil {
		return nil, err
	}
	var tmpl Template
	if err := json.Unmarshal(env.Data, &tmpl); err != nil {
		return nil, fmt.Errorf("decoding template response: %w", err)
	}
	return &tmpl, nil
}

// Combine merges the named templates into one output, in request order.
func (c *Client) Combine(names []string) (string, error) {
	env, err := c.get("/api/v1/combine", nil, map[string]string{"templates": strings.Join(names, ",")})
	if err != nil {
		return "", err
	}
	var content string
	if err := json.Unmarshal(env.Data, &content); err != nil {
		return "", fmt.Errorf("decoding combine response: %w", err)
	}
	return content, nil
}

// Stats returns server-reported template statistics.
func (c *Client) Stats() (map[string]interface{}, error) {
	env, err := c.get("/api/v1/stats", nil, nil)
	if err != nil {
		return nil, err
	}
	var stats map[string]interface{}
	if err := json.Unmarshal(env.Data, &stats); err != nil {
		return nil, fmt.Errorf("decoding stats response: %w", err)
	}
	return stats, nil
}

// Healthz checks server reachability; used by --status.
func (c *Client) Healthz() error {
	_, err := c.get("/api/v1/healthz", nil, nil)
	return err
}

// StatsCount is a small helper for formatting numeric stats fields that may
// arrive as json.Number/float64.
func StatsCount(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
