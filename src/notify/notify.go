package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

var retrySchedule = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

// Event is the stable, allow-listed payload sent to notification transports.
type Event struct {
	Role           string    `json:"role"`
	Event          string    `json:"event"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	Severity       string    `json:"severity"`
	Timestamp      time.Time `json:"timestamp"`
	ProjectName    string    `json:"project_name"`
	ProjectVersion string    `json:"project_version"`
	AppURL         string    `json:"app_url"`
	TrackingID     string    `json:"tracking_id,omitempty"`
}

// Target is one role's configured webhook endpoint. Secrets must be generated
// and persisted by the config integration before dispatch.
type Target struct {
	Name   string
	URL    string
	Secret string
}

// Adapter converts a notification into the request format for a transport.
// It must not make network requests or retain the URL or secret.
type Adapter func(Event, string) (requestURL string, contentType string, body []byte, err error)

// AuditFunc records an event name and non-secret fields. It must not include a
// target URL, webhook secret, or raw transport error.
type AuditFunc func(event string, fields map[string]string)

// Options configures dispatch behavior. A nil HTTP client gets a safe default
// with redirects disabled so credentials cannot be forwarded elsewhere.
type Options struct {
	HTTPClient  *http.Client
	UserAgent   string
	Now         func() time.Time
	Audit       AuditFunc
	RetryAfter  []time.Duration
	ResolveHost func(context.Context, string) ([]net.IP, error)
}

// Dispatcher sends signed webhook notifications with bounded retries.
type Dispatcher struct {
	client        *http.Client
	userAgent     string
	now           func() time.Time
	audit         AuditFunc
	retries       []time.Duration
	resolveHost   func(context.Context, string) ([]net.IP, error)
	pinResolvedIP bool

	mu       sync.Mutex
	adapters map[string]Adapter
	logged   map[string]time.Time
}

// NewDispatcher creates a dispatcher with Telegram, Discord, Slack,
// Mattermost, Pushover, Gotify, and generic JSON adapters.
func NewDispatcher(options Options) *Dispatcher {
	client := options.HTTPClient
	pinResolvedIP := client == nil
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	retries := options.RetryAfter
	if retries == nil {
		retries = append([]time.Duration(nil), retrySchedule...)
	}
	resolver := options.ResolveHost
	if resolver == nil {
		resolver = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	d := &Dispatcher{
		client: client, userAgent: options.UserAgent, now: now,
		audit: options.Audit, retries: append([]time.Duration(nil), retries...),
		resolveHost: resolver, pinResolvedIP: pinResolvedIP,
		adapters: make(map[string]Adapter), logged: make(map[string]time.Time),
	}
	d.adapters["generic"] = genericAdapter
	d.adapters["discord"] = discordAdapter
	d.adapters["slack"] = slackAdapter
	d.adapters["mattermost"] = slackAdapter
	d.adapters["telegram"] = telegramAdapter
	d.adapters["pushover"] = pushoverAdapter
	d.adapters["gotify"] = gotifyAdapter
	return d
}

// RegisterAdapter adds or replaces an adapter for a configured transport.
func (d *Dispatcher) RegisterAdapter(name string, adapter Adapter) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || adapter == nil {
		return errors.New("transport name and adapter are required")
	}
	d.mu.Lock()
	d.adapters[name] = adapter
	d.mu.Unlock()
	return nil
}

// GenerateSecret returns a new 32-byte webhook signing secret encoded as
// hexadecimal. The caller persists it in secrets.yml and reveals it once.
func GenerateSecret() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return hex.EncodeToString(secret), nil
}

// Dispatch sends an event to all supplied endpoints. It should run outside an
// HTTP request handler when retries may take longer than the request lifetime.
// Each endpoint receives its own UUIDv7 ID, reused across its retries.
func (d *Dispatcher) Dispatch(ctx context.Context, event Event, targets []Target) []error {
	if ctx == nil {
		ctx = context.Background()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = d.now().UTC()
	}
	results := make([]error, 0)
	for _, target := range targets {
		if strings.TrimSpace(target.URL) == "" {
			continue
		}
		if err := d.dispatchTarget(ctx, event, target); err != nil {
			results = append(results, err)
		}
	}
	return results
}

func (d *Dispatcher) dispatchTarget(ctx context.Context, event Event, target Target) error {
	d.mu.Lock()
	adapter := d.adapters[strings.ToLower(target.Name)]
	d.mu.Unlock()
	if adapter == nil {
		return fmt.Errorf("unsupported webhook transport %q", target.Name)
	}
	requestURL, contentType, body, err := adapter(event, target.URL)
	if err != nil {
		return fmt.Errorf("prepare webhook request: %w", err)
	}
	parsed, resolvedIPs, err := validateURL(ctx, requestURL, d.resolveHost)
	if err != nil {
		return err
	}
	if strings.TrimSpace(target.Secret) == "" {
		return errors.New("webhook signing secret is required")
	}
	webhookID, err := uuidV7(d.now())
	if err != nil {
		return fmt.Errorf("generate webhook ID: %w", err)
	}
	for attempt := 0; ; attempt++ {
		if err := d.post(ctx, requestURL, contentType, body, target.Secret, webhookID, event, resolvedIPs); err == nil {
			return nil
		} else if attempt >= len(d.retries) {
			d.auditFailure(event, target.Name, parsed.Hostname())
			return fmt.Errorf("webhook delivery failed after %d attempts", attempt+1)
		}
		timer := time.NewTimer(d.retries[attempt])
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (d *Dispatcher) post(ctx context.Context, requestURL, contentType string, body []byte, secret, id string, event Event, resolvedIPs []net.IP) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid webhook request")
	}
	stamp := strconv.FormatInt(d.now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Webhook-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-Webhook-Timestamp", stamp)
	req.Header.Set("X-Webhook-ID", id)
	req.Header.Set("X-Webhook-Event", event.Event)
	userAgent := d.userAgent
	if userAgent == "" && event.ProjectName != "" {
		userAgent = event.ProjectName + "/" + event.ProjectVersion
		if event.AppURL != "" {
			userAgent += " (+" + event.AppURL + ")"
		}
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	client := d.client
	if d.pinResolvedIP {
		clientCopy := *d.client
		transport := http.DefaultTransport.(*http.Transport).Clone()
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("invalid webhook destination")
			}
			var dialErr error
			for _, ip := range resolvedIPs {
				connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return connection, nil
				}
				dialErr = err
			}
			if dialErr != nil {
				return nil, dialErr
			}
			return nil, errors.New("webhook destination has no public addresses")
		}
		clientCopy.Transport = transport
		client = &clientCopy
		defer transport.CloseIdleConnections()
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("webhook transport failure")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (d *Dispatcher) auditFailure(event Event, transport, hostname string) {
	if d.audit == nil {
		return
	}
	now := d.now()
	keys := []string{hostname}
	if addresses, err := net.LookupIP(hostname); err == nil && len(addresses) > 0 {
		keys = make([]string, 0, len(addresses))
		for _, address := range addresses {
			keys = append(keys, address.String())
		}
	}
	d.mu.Lock()
	for _, key := range keys {
		last := d.logged[key]
		if !last.IsZero() && now.Sub(last) < time.Minute {
			d.mu.Unlock()
			return
		}
	}
	for _, key := range keys {
		d.logged[key] = now
	}
	d.mu.Unlock()
	d.audit("notify.webhook_failed", map[string]string{
		"event": event.Event, "role": event.Role, "transport": transport,
	})
}

func validateURL(ctx context.Context, raw string, resolve func(context.Context, string) ([]net.IP, error)) (*url.URL, []net.IP, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return nil, nil, errors.New("webhook URL is invalid")
	}
	if parsed.Scheme != "https" {
		return nil, nil, errors.New("webhook URL must use HTTPS")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return nil, nil, errors.New("webhook URL cannot target a local or internal host")
	}
	var addresses []net.IP
	if ip := net.ParseIP(host); ip != nil {
		addresses = []net.IP{ip}
	} else {
		if resolve == nil {
			return nil, nil, errors.New("webhook hostname cannot be resolved")
		}
		addresses, err = resolve(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, nil, errors.New("webhook hostname cannot be resolved")
		}
	}
	for _, address := range addresses {
		if !publicWebhookIP(address) {
			return nil, nil, errors.New("webhook URL cannot target a private or reserved address")
		}
	}
	return parsed, addresses, nil
}

func publicWebhookIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	blocked := []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func genericAdapter(event Event, rawURL string) (string, string, []byte, error) {
	body, err := json.Marshal(event)
	return rawURL, "application/json", body, err
}

func discordAdapter(event Event, rawURL string) (string, string, []byte, error) {
	body, err := json.Marshal(struct {
		Content  string `json:"content"`
		Username string `json:"username"`
	}{Content: formatMessage(event), Username: event.ProjectName})
	return rawURL, "application/json", body, err
}

func slackAdapter(event Event, rawURL string) (string, string, []byte, error) {
	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: formatMessage(event)})
	return rawURL, "application/json", body, err
}

func telegramAdapter(event Event, rawURL string) (string, string, []byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", nil, errors.New("invalid Telegram webhook URL")
	}
	query := parsed.Query()
	query.Set("text", formatMessage(event))
	parsed.RawQuery = query.Encode()
	body, err := json.Marshal(event)
	return parsed.String(), "application/json", body, err
}

func pushoverAdapter(event Event, rawURL string) (string, string, []byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", nil, errors.New("invalid Pushover webhook URL")
	}
	form := parsed.Query()
	form.Set("message", formatMessage(event))
	form.Set("title", event.Subject)
	parsed.RawQuery = ""
	body := []byte(form.Encode())
	return parsed.String(), "application/x-www-form-urlencoded", body, nil
}

func gotifyAdapter(event Event, rawURL string) (string, string, []byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", nil, errors.New("invalid gotify webhook URL")
	}
	query := parsed.Query()
	if query.Get("token") == "" {
		return "", "", nil, errors.New("gotify webhook URL must include its token")
	}
	parsed.Path = path.Join(parsed.Path, "message")
	parsed.RawQuery = query.Encode()
	body, err := json.Marshal(struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Priority int    `json:"priority"`
	}{Title: event.Subject, Message: formatMessage(event), Priority: severityPriority(event.Severity)})
	return parsed.String(), "application/json", body, err
}

func formatMessage(event Event) string {
	return event.Subject + "\n\n" + event.Body
}

func severityPriority(severity string) int {
	switch strings.ToLower(severity) {
	case "critical", "fatal":
		return 10
	case "error", "high":
		return 8
	case "warning", "medium":
		return 5
	default:
		return 0
	}
}

func uuidV7(now time.Time) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	ms := uint64(now.UnixMilli())
	value[0] = byte(ms >> 40)
	value[1] = byte(ms >> 32)
	value[2] = byte(ms >> 24)
	value[3] = byte(ms >> 16)
	value[4] = byte(ms >> 8)
	value[5] = byte(ms)
	value[6] = value[6]&0x0f | 0x70
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
