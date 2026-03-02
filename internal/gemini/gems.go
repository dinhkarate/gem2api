package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

const (
	batchExecutePath = "/_/BardChatUi/data/batchexecute"
	fetchGemsRPCID   = "CNgdBe"
)

// Gem represents a Gemini Gem (custom persona).
type Gem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	IsPredefined bool   `json:"is_predefined"`
}

// FetchGems retrieves available gems using fallback cookies.
func (c *Client) FetchGems(ctx context.Context) ([]Gem, error) {
	return c.FetchGemsAs(ctx, c.secure1PSID, c.secure1PSIDTS)
}

// FetchGemsAs retrieves available gems using specific cookies.
func (c *Client) FetchGemsAs(ctx context.Context, psid, psidts string) ([]Gem, error) {
	// Get cached session or bootstrap
	c.cacheMu.RLock()
	session := c.sessionCache[cacheKey(psid)]
	c.cacheMu.RUnlock()

	if session == nil {
		var err error
		session, err = c.doBootstrap(ctx, psid, psidts)
		if err != nil {
			return nil, fmt.Errorf("auto-bootstrap failed: %w", err)
		}
		c.cacheMu.Lock()
		c.sessionCache[cacheKey(psid)] = session
		c.cacheMu.Unlock()
	}

	var allGems []Gem

	// Fetch user-created gems (type 2) and predefined gems (type 3)
	for _, gemType := range []struct {
		code         int
		isPredefined bool
	}{
		{2, false}, // user-created
		{3, true},  // predefined visible
	} {
		gems, err := c.fetchGemsByType(ctx, psid, psidts, session, gemType.code, gemType.isPredefined)
		if err != nil {
			log.Printf("Failed to fetch gems type %d: %v", gemType.code, err)
			continue
		}
		allGems = append(allGems, gems...)
	}

	return allGems, nil
}

func (c *Client) fetchGemsByType(ctx context.Context, psid, psidts string, session *SessionData, gemTypeCode int, isPredefined bool) ([]Gem, error) {
	// Build batchexecute payload
	// Inner payload: [code, ['en'], 0]
	innerPayload := fmt.Sprintf(`[%d,["en"],0]`, gemTypeCode)

	// Outer f.req: [[["CNgdBe","[2,[\"en\"],0]",null,"generic"]]]
	fReqInner := []interface{}{
		[]interface{}{fetchGemsRPCID, innerPayload, nil, "generic"},
	}
	fReqOuter := []interface{}{fReqInner}
	fReqJSON, err := json.Marshal(fReqOuter)
	if err != nil {
		return nil, fmt.Errorf("marshal f.req: %w", err)
	}

	body := fmt.Sprintf("at=%s&f.req=%s",
		url.QueryEscape(session.SNlM0e),
		url.QueryEscape(string(fReqJSON)),
	)

	reqURL := fmt.Sprintf("%s%s?rpcids=%s&bl=%s&f.sid=%s&rt=c",
		baseURL, batchExecutePath,
		fetchGemsRPCID,
		url.QueryEscape(session.Cfb2h),
		url.QueryEscape(session.FdrFJe),
	)

	req, err := newRequestWithContext(ctx, "POST", reqURL, body)
	if err != nil {
		return nil, err
	}
	setHeaders(req)
	setCookies(req, psid, psidts)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 200)]))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseGemsResponse(string(respBody), isPredefined)
}

func newRequestWithContext(ctx context.Context, method, url, body string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	return req, nil
}

func parseGemsResponse(raw string, isPredefined bool) ([]Gem, error) {
	// Response format: )]}'  then length-prefixed frames
	// Find the first frame containing gem data
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, ")]}'") {
		raw = raw[4:]
	}
	raw = strings.TrimSpace(raw)

	var gems []Gem

	// Parse length-prefixed frames (same format as StreamGenerate)
	frames := strings.Split(raw, "\n")
	for i := 0; i < len(frames); i++ {
		line := strings.TrimSpace(frames[i])
		if line == "" {
			continue
		}

		// Try to parse as JSON array (skip length lines)
		if line[0] != '[' {
			continue
		}

		// This should be a batchexecute response frame
		var outerArr []json.RawMessage
		if err := json.Unmarshal([]byte(line), &outerArr); err != nil {
			continue
		}

		// Each element: ["wrb.fr","CNgdBe","<inner json>",null,...]
		for _, entry := range outerArr {
			var entryArr []json.RawMessage
			if err := json.Unmarshal(entry, &entryArr); err != nil {
				continue
			}
			if len(entryArr) < 3 {
				continue
			}

			// Check it's our RPC
			var rpcID string
			json.Unmarshal(entryArr[1], &rpcID)
			if rpcID != fetchGemsRPCID {
				continue
			}

			// Parse inner content
			var innerStr string
			if err := json.Unmarshal(entryArr[2], &innerStr); err != nil {
				continue
			}

			parsed := parseGemsList(innerStr, isPredefined)
			gems = append(gems, parsed...)
		}
	}

	return gems, nil
}

func parseGemsList(jsonStr string, isPredefined bool) []Gem {
	var gems []Gem

	var content []json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &content); err != nil {
		return gems
	}

	// Gems list is at content[2]
	if len(content) < 3 {
		return gems
	}

	var gemsList []json.RawMessage
	if err := json.Unmarshal(content[2], &gemsList); err != nil {
		return gems
	}

	for _, gemRaw := range gemsList {
		var gemArr []json.RawMessage
		if err := json.Unmarshal(gemRaw, &gemArr); err != nil {
			continue
		}
		if len(gemArr) < 2 {
			continue
		}

		gem := Gem{IsPredefined: isPredefined}

		// ID at [0]
		json.Unmarshal(gemArr[0], &gem.ID)

		// Info at [1]: [name, description, ...]
		if len(gemArr) > 1 {
			var info []json.RawMessage
			if err := json.Unmarshal(gemArr[1], &info); err == nil {
				if len(info) > 0 {
					json.Unmarshal(info[0], &gem.Name)
				}
				if len(info) > 1 {
					json.Unmarshal(info[1], &gem.Description)
				}
			}
		}

		// Prompt at [2]: [prompt, ...]
		if len(gemArr) > 2 {
			var promptArr []json.RawMessage
			if err := json.Unmarshal(gemArr[2], &promptArr); err == nil && len(promptArr) > 0 {
				json.Unmarshal(promptArr[0], &gem.Prompt)
			}
		}

		if gem.ID != "" {
			gems = append(gems, gem)
		}
	}

	return gems
}
