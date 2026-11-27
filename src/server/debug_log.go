package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/mode"
)

var debugSQLLiteralPattern = regexp.MustCompile(`'(?:''|[^'])*'`)

func sanitizeDebugSQL(query string) string {
	return debugSQLLiteralPattern.ReplaceAllString(query, "?")
}

type debugResponseWriter struct {
	http.ResponseWriter
	status int
	size   int
	limit  int
	body   []byte
}

func (w *debugResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *debugResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.size += n
	if w.limit > len(w.body) {
		remaining := w.limit - len(w.body)
		if remaining > len(body) {
			remaining = len(body)
		}
		w.body = append(w.body, body[:remaining]...)
	}
	return n, err
}

func (w *debugResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *debugResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		if w.status == 0 {
			w.WriteHeader(http.StatusOK)
		}
		f.Flush()
	}
}

func (w *debugResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (w *debugResponseWriter) Push(target string, options *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, options)
}

func (w *debugResponseWriter) ReadFrom(source io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := io.Copy(w.ResponseWriter, source)
	w.size += int(n)
	return n, err
}

func (s *Server) debugMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mode.IsDebugEnabled() {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		bodyPreview := ""
		bodyLimit := 0
		if cfg := s.currentConfig(); cfg != nil && cfg.Server.Debug.LogBodies {
			bodyPreview = captureDebugRequestBody(r, cfg.Server.Debug.MaxBodyLogSize)
			bodyLimit = parseDebugBodyLimit(cfg.Server.Debug.MaxBodyLogSize)
		}
		capture := &debugResponseWriter{ResponseWriter: w, limit: bodyLimit}
		next.ServeHTTP(capture, r)
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		responsePreview := ""
		if bodyLimit > 0 && len(capture.body) > 0 {
			responsePreview = redactDebugBody(w.Header().Get("Content-Type"), capture.body)
			if capture.size > len(capture.body) {
				responsePreview += " [truncated]"
			}
		}
		slog.Debug("http request",
			"method", r.Method, "path", r.URL.Path, "query", safeDebugQuery(r.URL.Query()),
			"status", status, "duration_ms", time.Since(start).Milliseconds(),
			"request_size", r.ContentLength, "response_size", capture.size,
			"remote_addr", r.RemoteAddr, "user_agent", r.UserAgent(),
			"request_id", r.Header.Get("X-Request-ID"),
			"headers_request", safeDebugHeaders(r.Header), "headers_response", safeDebugHeaders(w.Header()),
			"request_body", bodyPreview, "response_body", responsePreview)
	})
}

func safeDebugHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for name, values := range headers {
		if sensitiveDebugField(name) || strings.EqualFold(name, "set-cookie") {
			result[name] = []string{"[REDACTED]"}
			continue
		}
		result[name] = append([]string(nil), values...)
	}
	return result
}

func captureDebugRequestBody(r *http.Request, configuredLimit string) string {
	if r.Body == nil {
		return ""
	}
	limit := parseDebugBodyLimit(configuredLimit)
	if limit <= 0 {
		return "[body logging disabled: invalid size limit]"
	}
	readLimit := limit
	if limit < int(^uint(0)>>1) {
		readLimit++
	}
	captured, err := io.ReadAll(io.LimitReader(r.Body, int64(readLimit)))
	if err != nil {
		return "[body unavailable]"
	}
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(captured), r.Body))
	truncated := len(captured) > limit
	data := captured
	if truncated {
		data = data[:limit]
	}
	preview := redactDebugBody(r.Header.Get("Content-Type"), data)
	if truncated {
		preview += " [truncated]"
	}
	return preview
}

func parseDebugBodyLimit(value string) int {
	value = strings.ToUpper(strings.TrimSpace(value))
	units := []struct {
		suffix     string
		multiplier uint64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30}, {"TIB", 1 << 40},
		{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30}, {"TB", 1 << 40},
		{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"T", 1 << 40}, {"B", 1},
	}
	var multiplier uint64 = 1
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			value = strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
			multiplier = unit.multiplier
			break
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 || n > uint64(^uint(0)>>1)/multiplier {
		return 0
	}
	return int(n * multiplier)
}

func redactDebugBody(contentType string, data []byte) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "[body omitted: unsupported content type]"
	}
	switch mediaType {
	case "application/json", "application/ld+json":
		var value any
		if json.Unmarshal(data, &value) != nil {
			return "[body omitted: invalid JSON]"
		}
		redactDebugJSON(value)
		encoded, err := json.Marshal(value)
		if err != nil {
			return "[body omitted]"
		}
		return string(encoded)
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(data))
		if err != nil {
			return "[body omitted: invalid form]"
		}
		for key := range values {
			if sensitiveDebugField(key) {
				values[key] = []string{"[REDACTED]"}
			}
		}
		return values.Encode()
	default:
		return "[body omitted: unsupported content type]"
	}
}

func redactDebugJSON(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if sensitiveDebugField(key) {
				typed[key] = "[REDACTED]"
			} else {
				redactDebugJSON(item)
			}
		}
	case []any:
		for _, item := range typed {
			redactDebugJSON(item)
		}
	}
}

func sensitiveDebugField(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	for _, marker := range []string{"password", "passwd", "token", "secret", "credential", "authorization", "api_key", "private_key", "cookie", "key"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func safeDebugQuery(values url.Values) string {
	copy := make(url.Values, len(values))
	for key, items := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "key") || strings.Contains(lower, "auth") {
			copy[key] = []string{"[REDACTED]"}
			continue
		}
		copy[key] = items
	}
	return copy.Encode()
}
