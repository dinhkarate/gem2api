package browser

import (
	"context"
	"fmt"
	"log"
	"sync"

	"gem2api/internal/storage"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// LoginSession represents an active browser session for Google login.
type LoginSession struct {
	ProfileID int
	ctx       context.Context
	cancel    context.CancelFunc

	// Screencast channels
	FrameCh chan *ScreencastFrame // JPEG frames for the admin viewer
	InputCh chan InputEvent       // Input events from the admin viewer

	status string
	mu     sync.RWMutex
	closed bool
}

// ScreencastFrame holds a single screencast frame from CDP.
type ScreencastFrame struct {
	Data      string                        `json:"data"`      // base64-encoded image
	SessionID int64                         `json:"sessionId"` // for ack
	Metadata  *page.ScreencastFrameMetadata `json:"metadata"`
}

// Status returns the current login session status.
func (ls *LoginSession) Status() string {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.status
}

func (ls *LoginSession) setStatus(status string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.status = status
}

// Close cancels the browser context and cleans up.
func (ls *LoginSession) Close() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if !ls.closed {
		ls.closed = true
		ls.cancel()
		close(ls.FrameCh)
		close(ls.InputCh)
	}
}

// StartLogin launches a Chrome browser for interactive Google login.
// The browser is visible via screencast frames streamed to FrameCh.
func (bm *BrowserManager) StartLogin(profileID int) error {
	bm.mu.Lock()
	if _, exists := bm.sessions[profileID]; exists {
		bm.mu.Unlock()
		return fmt.Errorf("login session already active for profile %d", profileID)
	}
	bm.mu.Unlock()

	// Get profile from DB
	profile, err := bm.db.GetBrowserProfile(profileID)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Ensure profile directory exists
	dir, err := bm.ensureProfileDir(profile.ProfileName)
	if err != nil {
		return err
	}

	// Launch Chrome with screencast (headless - we stream frames via CDP)
	opts := bm.chromeOpts(dir, true)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	session := &LoginSession{
		ProfileID: profileID,
		ctx:       ctx,
		cancel: func() {
			cancel()
			allocCancel()
		},
		FrameCh: make(chan *ScreencastFrame, 5),
		InputCh: make(chan InputEvent, 20),
		status:  "starting",
	}

	bm.mu.Lock()
	bm.sessions[profileID] = session
	bm.mu.Unlock()

	// Navigate to Google login and start screencast
	go bm.runLoginSession(session, profile)

	return nil
}

// runLoginSession manages the Chrome browser lifecycle for login.
func (bm *BrowserManager) runLoginSession(session *LoginSession, profile *storage.BrowserProfile) {
	defer bm.removeSession(session.ProfileID)

	// Remove navigator.webdriver flag (anti-detection)
	if err := chromedp.Run(session.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Enable page events for screencast
			if err := page.Enable().Do(ctx); err != nil {
				return err
			}

			// Remove webdriver flag via CDP
			_, err := page.AddScriptToEvaluateOnNewDocument(`
				Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
				// Remove Chrome automation indicators
				delete navigator.__proto__.webdriver;
				window.chrome = {runtime: {}};
			`).Do(ctx)
			return err
		}),
	); err != nil {
		log.Printf("Login session %d: anti-detection setup failed: %v", session.ProfileID, err)
	}

	// Start screencast
	if err := chromedp.Run(session.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return page.StartScreencast().
				WithFormat(page.ScreencastFormatJpeg).
				WithQuality(75).
				WithMaxWidth(1280).
				WithMaxHeight(720).
				Do(ctx)
		}),
	); err != nil {
		log.Printf("Login session %d: screencast start failed: %v", session.ProfileID, err)
		session.setStatus("error")
		bm.db.UpdateBrowserProfileStatus(profile.ID, "error")
		return
	}

	// Listen for screencast frames
	chromedp.ListenTarget(session.ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *page.EventScreencastFrame:
			frame := &ScreencastFrame{
				Data:      ev.Data,
				SessionID: ev.SessionID,
				Metadata:  ev.Metadata,
			}
			// Non-blocking send
			select {
			case session.FrameCh <- frame:
			default:
				// Drop frame if channel is full
			}
			// Acknowledge frame to continue receiving
			go func() {
				if err := chromedp.Run(session.ctx,
					chromedp.ActionFunc(func(ctx context.Context) error {
						return page.ScreencastFrameAck(ev.SessionID).Do(ctx)
					}),
				); err != nil {
					// Ignore ack errors (session may be closed)
				}
			}()
		}
	})

	// Navigate to Google account login
	session.setStatus("logging_in")
	bm.db.UpdateBrowserProfileStatus(profile.ID, "logging_in")

	if err := chromedp.Run(session.ctx,
		chromedp.Navigate("https://accounts.google.com/"),
	); err != nil {
		log.Printf("Login session %d: navigation failed: %v", session.ProfileID, err)
		session.setStatus("error")
		bm.db.UpdateBrowserProfileStatus(profile.ID, "error")
		return
	}

	// Process input events from the admin viewer
	bm.processInputEvents(session)
}

// FinishLogin extracts cookies from an active login session and saves them.
func (bm *BrowserManager) FinishLogin(profileID int) (*CookieResult, error) {
	session := bm.GetSession(profileID)
	if session == nil {
		return nil, fmt.Errorf("no active login session for profile %d", profileID)
	}

	profile, err := bm.db.GetBrowserProfile(profileID)
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}

	// Navigate to Gemini first to ensure cookies are set
	if err := chromedp.Run(session.ctx,
		chromedp.Navigate("https://gemini.google.com/"),
		chromedp.WaitReady("body"),
	); err != nil {
		return nil, fmt.Errorf("navigate to gemini: %w", err)
	}

	// Extract cookies
	cookies, err := extractGoogleCookies(session.ctx)
	if err != nil {
		return nil, err
	}

	// Save to database: create or update account
	accountID, _, err := bm.db.UpsertByPSID(cookies.Secure1PSID, cookies.Secure1PSIDTS)
	if err != nil {
		return nil, fmt.Errorf("save account: %w", err)
	}

	// Link profile to account
	if err := bm.db.LinkBrowserProfileAccount(profile.ID, int(accountID)); err != nil {
		return nil, fmt.Errorf("link profile to account: %w", err)
	}

	// Mark profile as active
	bm.db.UpdateBrowserProfileStatus(profile.ID, "active")
	bm.db.UpdateBrowserProfileRefresh(profile.ID)

	// Close the login session
	session.Close()

	log.Printf("Login session %d: cookies extracted, account %d linked", profileID, accountID)
	return cookies, nil
}

// CancelLogin cancels an active login session.
func (bm *BrowserManager) CancelLogin(profileID int) {
	session := bm.GetSession(profileID)
	if session != nil {
		session.Close()
		bm.removeSession(profileID)
	}
	bm.db.UpdateBrowserProfileStatus(profileID, "pending")
}

// extractGoogleCookies extracts __Secure-1PSID and __Secure-1PSIDTS from the browser.
func extractGoogleCookies(ctx context.Context) (*CookieResult, error) {
	var cookies []*network.Cookie

	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().
				WithURLs([]string{
					"https://gemini.google.com",
					"https://accounts.google.com",
					"https://.google.com",
				}).Do(ctx)
			return err
		}),
	); err != nil {
		return nil, fmt.Errorf("get cookies: %w", err)
	}

	result := &CookieResult{}
	for _, c := range cookies {
		switch c.Name {
		case "__Secure-1PSID":
			result.Secure1PSID = c.Value
		case "__Secure-1PSIDTS":
			result.Secure1PSIDTS = c.Value
		}
	}

	if result.Secure1PSID == "" {
		return nil, fmt.Errorf("__Secure-1PSID cookie not found — login may not be complete")
	}

	return result, nil
}
