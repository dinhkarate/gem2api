package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	baseURL          = "https://gemini.google.com"
	generatePath     = "/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate"
	rotateCookiesURL = "https://accounts.google.com/RotateCookies"
	modelHeaderKey   = "x-goog-ext-525001261-jspb"
	userAgent        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	snlm0eRe = regexp.MustCompile(`"SNlM0e":"([^"]+)"`)
	cfb2hRe  = regexp.MustCompile(`"cfb2h":"([^"]+)"`)
	fdrfjeRe = regexp.MustCompile(`"FdrFJe":"([^"]+)"`)
)

// Client interacts with the Gemini web interface (gemini.google.com).
type Client struct {
	httpClient *http.Client

	// Fallback cookies (from env vars, for backward compatibility)
	secure1PSID   string
	secure1PSIDTS string

	// Per-account session cache (key = first 20 chars of PSID)
	sessionCache map[string]*SessionData
	cacheMu      sync.RWMutex

	mu           sync.RWMutex
	stopRotation chan struct{}
	bootstrapMu  sync.Mutex // prevents concurrent bootstrap for same fallback account
}

// NewClient creates a Gemini web client.
// psid/psidts are optional fallback cookies (for env var backward compat).
func NewClient(secure1PSID, secure1PSIDTS, proxyURL string) (*Client, error) {
	transport := &http.Transport{}

	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}

	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   120 * time.Second,
		},
		secure1PSID:   secure1PSID,
		secure1PSIDTS: secure1PSIDTS,
		sessionCache:  make(map[string]*SessionData),
		stopRotation:  make(chan struct{}),
	}, nil
}

// cacheKey returns a short key for session cache lookup.
func cacheKey(psid string) string {
	if len(psid) > 20 {
		return psid[:20]
	}
	return psid
}

// Bootstrap extracts session tokens using the fallback cookies.
func (c *Client) Bootstrap(ctx context.Context) error {
	c.bootstrapMu.Lock()
	defer c.bootstrapMu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			log.Printf("Bootstrap attempt %d/3 — rotating cookies first...", attempt)
			if rotErr := c.rotateCookies(c.secure1PSID, c.secure1PSIDTS); rotErr != nil {
				log.Printf("Cookie rotation failed: %v (continuing anyway)", rotErr)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		session, err := c.doBootstrap(ctx, c.secure1PSID, c.secure1PSIDTS)
		if err == nil {
			c.cacheMu.Lock()
			c.sessionCache[cacheKey(c.secure1PSID)] = session
			c.cacheMu.Unlock()
			return nil
		}
		lastErr = err
		log.Printf("Bootstrap attempt %d failed: %v", attempt, lastErr)
	}
	return fmt.Errorf("bootstrap failed after 3 attempts: %w", lastErr)
}

// BootstrapFor bootstraps a session for specific cookies (pool account).
func (c *Client) BootstrapFor(ctx context.Context, psid, psidts string) error {
	session, err := c.doBootstrap(ctx, psid, psidts)
	if err != nil {
		return err
	}
	c.cacheMu.Lock()
	c.sessionCache[cacheKey(psid)] = session
	c.cacheMu.Unlock()
	return nil
}

// doBootstrap performs a single bootstrap attempt with given cookies.
func (c *Client) doBootstrap(ctx context.Context, psid, psidts string) (*SessionData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/app", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setHeaders(req)
	setCookies(req, psid, psidts)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, preview)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	html := string(body)

	session := &SessionData{}
	if m := snlm0eRe.FindStringSubmatch(html); len(m) > 1 {
		session.SNlM0e = m[1]
	} else {
		return nil, fmt.Errorf("SNlM0e not found (cookies expired or Google blocked access)")
	}
	if m := cfb2hRe.FindStringSubmatch(html); len(m) > 1 {
		session.Cfb2h = m[1]
	} else {
		return nil, fmt.Errorf("cfb2h not found")
	}
	if m := fdrfjeRe.FindStringSubmatch(html); len(m) > 1 {
		session.FdrFJe = m[1]
	} else {
		return nil, fmt.Errorf("FdrFJe not found")
	}

	log.Printf("Session bootstrapped (SNlM0e=%.8s... cfb2h=%s)", session.SNlM0e, session.Cfb2h)
	return session, nil
}

// Generate sends a prompt using the fallback cookies (backward compat).
func (c *Client) Generate(ctx context.Context, prompt, modelKey, gemID string) (io.ReadCloser, error) {
	return c.GenerateAs(ctx, c.secure1PSID, c.secure1PSIDTS, prompt, modelKey, gemID)
}

// GenerateAs sends a prompt using specific cookies (for pool accounts).
// It auto-bootstraps on first use and re-bootstraps on session errors.
func (c *Client) GenerateAs(ctx context.Context, psid, psidts, prompt, modelKey, gemID string) (io.ReadCloser, error) {
	body, err := c.doGenerate(ctx, psid, psidts, prompt, modelKey, gemID)
	if err != nil && isSessionError(err) {
		log.Printf("Generate failed with session error: %v — re-bootstrapping...", err)
		if bErr := c.BootstrapFor(ctx, psid, psidts); bErr != nil {
			return nil, fmt.Errorf("re-bootstrap failed: %w (original: %v)", bErr, err)
		}
		return c.doGenerate(ctx, psid, psidts, prompt, modelKey, gemID)
	}
	return body, err
}

func isSessionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "status 403") ||
		strings.Contains(msg, "session not bootstrapped")
}

func (c *Client) doGenerate(ctx context.Context, psid, psidts, prompt, modelKey, gemID string) (io.ReadCloser, error) {
	// Get cached session or bootstrap
	c.cacheMu.RLock()
	session := c.sessionCache[cacheKey(psid)]
	c.cacheMu.RUnlock()

	if session == nil {
		// Auto-bootstrap for this account
		var err error
		session, err = c.doBootstrap(ctx, psid, psidts)
		if err != nil {
			return nil, fmt.Errorf("auto-bootstrap failed: %w", err)
		}
		c.cacheMu.Lock()
		c.sessionCache[cacheKey(psid)] = session
		c.cacheMu.Unlock()
	}

	body, err := buildRequestBody(prompt, session, gemID)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	reqURL := fmt.Sprintf("%s%s?_reqid=%d&rt=c&bl=%s&f.sid=%s",
		baseURL, generatePath,
		rand.Intn(90000)+10000,
		url.QueryEscape(session.Cfb2h),
		url.QueryEscape(session.FdrFJe),
	)

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	setHeaders(req)
	setCookies(req, psid, psidts)

	if model, ok := KnownModels[modelKey]; ok && model.HexID != "" {
		req.Header.Set(modelHeaderKey, fmt.Sprintf(
			`[1,null,null,null,"%s",null,null,0,[4],null,null,1]`, model.HexID))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(errBody[:min(len(errBody), 500)]))
	}

	return resp.Body, nil
}

func buildRequestBody(prompt string, session *SessionData, gemID string) (string, error) {
	inner := make([]interface{}, 69)
	inner[0] = []interface{}{prompt, 0, nil, nil, nil, nil, 0}
	inner[2] = []interface{}{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	inner[7] = 1

	// Set Gem ID at index 19 if provided
	if gemID != "" {
		inner[19] = gemID
	}

	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return "", fmt.Errorf("marshal inner: %w", err)
	}

	fReq := []interface{}{nil, string(innerJSON)}
	fReqJSON, err := json.Marshal(fReq)
	if err != nil {
		return "", fmt.Errorf("marshal f.req: %w", err)
	}

	return fmt.Sprintf("at=%s&f.req=%s",
		url.QueryEscape(session.SNlM0e),
		url.QueryEscape(string(fReqJSON)),
	), nil
}

// StartCookieRotation starts background cookie rotation for fallback cookies.
func (c *Client) StartCookieRotation() {
	go func() {
		ticker := time.NewTicker(9 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.rotateCookies(c.secure1PSID, c.secure1PSIDTS); err != nil {
					log.Printf("Cookie rotation failed: %v", err)
				}
			case <-c.stopRotation:
				return
			}
		}
	}()
}

func (c *Client) rotateCookies(psid, psidts string) error {
	req, err := http.NewRequest("POST", rotateCookiesURL,
		strings.NewReader(`[000,"-0000000000000000000"]`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setCookies(req, psid, psidts)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "__Secure-1PSIDTS" {
			c.mu.Lock()
			c.secure1PSIDTS = cookie.Value
			c.mu.Unlock()
			log.Println("Cookie __Secure-1PSIDTS rotated")
			return nil
		}
	}
	return nil
}

// Close stops cookie rotation.
func (c *Client) Close() {
	close(c.stopRotation)
}

// setHeaders sets common browser-like headers.
func setHeaders(req *http.Request) {
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", "https://gemini.google.com/")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Same-Domain", "1")
}

// setCookies sets authentication cookies on a request.
func setCookies(req *http.Request, psid, psidts string) {
	req.AddCookie(&http.Cookie{Name: "__Secure-1PSID", Value: psid})
	if psidts != "" {
		req.AddCookie(&http.Cookie{Name: "__Secure-1PSIDTS", Value: psidts})
	}
}
