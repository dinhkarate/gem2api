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
	httpClient    *http.Client
	secure1PSID   string
	secure1PSIDTS string
	session       *SessionData
	mu            sync.RWMutex
	stopRotation  chan struct{}
}

// NewClient creates a Gemini web client with browser cookies.
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
		stopRotation:  make(chan struct{}),
	}, nil
}

// Bootstrap extracts session tokens (SNlM0e, cfb2h, FdrFJe) from gemini.google.com/app.
func (c *Client) Bootstrap(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/app", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)
	c.setCookies(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d (cookies may be invalid)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	html := string(body)

	session := &SessionData{}

	if m := snlm0eRe.FindStringSubmatch(html); len(m) > 1 {
		session.SNlM0e = m[1]
	} else {
		return fmt.Errorf("SNlM0e not found (cookies may be expired)")
	}

	if m := cfb2hRe.FindStringSubmatch(html); len(m) > 1 {
		session.Cfb2h = m[1]
	} else {
		return fmt.Errorf("cfb2h not found")
	}

	if m := fdrfjeRe.FindStringSubmatch(html); len(m) > 1 {
		session.FdrFJe = m[1]
	} else {
		return fmt.Errorf("FdrFJe not found")
	}

	c.mu.Lock()
	c.session = session
	c.mu.Unlock()

	log.Printf("Session bootstrapped (SNlM0e=%.8s... cfb2h=%s)", session.SNlM0e, session.Cfb2h)
	return nil
}

// Generate sends a prompt to Gemini web and returns the raw response body for parsing.
func (c *Client) Generate(ctx context.Context, prompt, modelKey string) (io.ReadCloser, error) {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return nil, fmt.Errorf("session not bootstrapped")
	}

	body, err := buildRequestBody(prompt, session)
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
	c.setHeaders(req)
	c.setCookies(req)

	// Set model header if needed
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

// buildRequestBody constructs the form-encoded body for StreamGenerate.
func buildRequestBody(prompt string, session *SessionData) (string, error) {
	// Build inner 69-element array
	inner := make([]interface{}, 69)
	// [0]: message content — [prompt, 0, null, null, null, null, 0]
	inner[0] = []interface{}{prompt, 0, nil, nil, nil, nil, 0}
	// [2]: conversation context (new conversation)
	inner[2] = []interface{}{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	// [7]: enable snapshot streaming
	inner[7] = 1

	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return "", fmt.Errorf("marshal inner: %w", err)
	}

	// f.req = [null, "<inner_json_string>"]
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

// StartCookieRotation starts a background goroutine to rotate __Secure-1PSIDTS every ~9 minutes.
func (c *Client) StartCookieRotation() {
	go func() {
		ticker := time.NewTicker(9 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.rotateCookies(); err != nil {
					log.Printf("Cookie rotation failed: %v", err)
				}
			case <-c.stopRotation:
				return
			}
		}
	}()
}

func (c *Client) rotateCookies() error {
	req, err := http.NewRequest("POST", rotateCookiesURL,
		strings.NewReader(`[000,"-0000000000000000000"]`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setCookies(req)

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

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", "https://gemini.google.com/")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Same-Domain", "1")
}

func (c *Client) setCookies(req *http.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	req.AddCookie(&http.Cookie{Name: "__Secure-1PSID", Value: c.secure1PSID})
	if c.secure1PSIDTS != "" {
		req.AddCookie(&http.Cookie{Name: "__Secure-1PSIDTS", Value: c.secure1PSIDTS})
	}
}
