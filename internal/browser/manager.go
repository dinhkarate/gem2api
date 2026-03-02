package browser

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gem2api/internal/config"
	"gem2api/internal/storage"

	"github.com/chromedp/chromedp"
)

// BrowserManager manages Chrome browser instances for Google account authentication.
type BrowserManager struct {
	db     *storage.DB
	config *config.Config

	sessions map[int]*LoginSession // profileID -> active login session
	mu       sync.Mutex

	stopRefresh chan struct{}
}

// CookieResult holds extracted Google authentication cookies.
type CookieResult struct {
	Secure1PSID   string
	Secure1PSIDTS string
}

// InputEvent represents a user input event from the admin panel viewer.
type InputEvent struct {
	Type       string  `json:"type"`                 // click, mousedown, mouseup, mousemove, type, keydown, keyup, scroll, navigate
	X          float64 `json:"x,omitempty"`          // Mouse X coordinate
	Y          float64 `json:"y,omitempty"`          // Mouse Y coordinate
	Button     string  `json:"button,omitempty"`     // Mouse button: left, right, middle
	ClickCount int     `json:"clickCount,omitempty"` // Click count (1=single, 2=double)
	Text       string  `json:"text,omitempty"`       // Text to type
	Key        string  `json:"key,omitempty"`        // Key name (Enter, Tab, etc.)
	Code       string  `json:"code,omitempty"`       // Physical key code
	DeltaX     float64 `json:"deltaX,omitempty"`     // Scroll delta X
	DeltaY     float64 `json:"deltaY,omitempty"`     // Scroll delta Y
	URL        string  `json:"url,omitempty"`        // Navigation URL
	Modifiers  int     `json:"modifiers,omitempty"`  // Bitmask: Alt=1, Ctrl=2, Meta=4, Shift=8
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

// NewBrowserManager creates a new browser manager.
func NewBrowserManager(db *storage.DB, cfg *config.Config) *BrowserManager {
	return &BrowserManager{
		db:          db,
		config:      cfg,
		sessions:    make(map[int]*LoginSession),
		stopRefresh: make(chan struct{}),
	}
}

// chromeOpts returns chromedp allocator options with anti-detection measures.
func (bm *BrowserManager) chromeOpts(profileDir string, headless bool) []chromedp.ExecAllocatorOption {
	ua := userAgents[rand.Intn(len(userAgents))]

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(profileDir),
		chromedp.UserAgent(ua),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-features", "TranslateUI"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.WindowSize(1280, 720),
	)

	if bm.config.ChromePath != "" {
		opts = append(opts, chromedp.ExecPath(bm.config.ChromePath))
	}

	if headless {
		opts = append(opts, chromedp.Headless)
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}

	return opts
}

// profileDir returns the filesystem path for a profile's Chrome user data.
func (bm *BrowserManager) profileDir(profileName string) string {
	return filepath.Join(bm.config.BrowserDataDir, profileName)
}

// ensureProfileDir creates the profile directory if it doesn't exist.
func (bm *BrowserManager) ensureProfileDir(profileName string) (string, error) {
	dir := bm.profileDir(profileName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create profile directory: %w", err)
	}
	return dir, nil
}

// GetSession returns the active login session for a profile, if any.
func (bm *BrowserManager) GetSession(profileID int) *LoginSession {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.sessions[profileID]
}

// DB returns the database instance (for admin handlers).
func (bm *BrowserManager) DB() *storage.DB {
	return bm.db
}

// ProfileDirFor returns the filesystem path for a named profile.
func (bm *BrowserManager) ProfileDirFor(profileName string) string {
	return bm.profileDir(profileName)
}

// removeSession removes a login session from the map.
func (bm *BrowserManager) removeSession(profileID int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	delete(bm.sessions, profileID)
}

// StartAutoRefresh begins periodic cookie refresh for all active profiles.
func (bm *BrowserManager) StartAutoRefresh() {
	interval := bm.config.BrowserRefreshInterval
	if interval <= 0 {
		interval = 30 * time.Minute
	}

	go func() {
		// Initial delay to let the server start up
		time.Sleep(10 * time.Second)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				bm.refreshAllProfiles()
			case <-bm.stopRefresh:
				return
			}
		}
	}()

	log.Printf("Browser auto-refresh started (interval: %v)", interval)
}

// refreshAllProfiles refreshes cookies for all active browser profiles.
func (bm *BrowserManager) refreshAllProfiles() {
	profiles, err := bm.db.ListActiveBrowserProfiles()
	if err != nil {
		log.Printf("Browser refresh: failed to list profiles: %v", err)
		return
	}

	for _, p := range profiles {
		if p.AccountID == nil {
			continue
		}

		cookies, err := bm.RefreshCookies(p)
		if err != nil {
			log.Printf("Browser refresh: profile %q failed: %v", p.ProfileName, err)
			if dbErr := bm.db.UpdateBrowserProfileError(p.ID, err.Error()); dbErr != nil {
				log.Printf("Browser refresh: failed to record error for profile %d: %v", p.ID, dbErr)
			}
			continue
		}

		// Update account cookies
		if err := bm.db.UpdateAccountCookies(*p.AccountID, cookies.Secure1PSID, cookies.Secure1PSIDTS); err != nil {
			log.Printf("Browser refresh: failed to update cookies for account %d: %v", *p.AccountID, err)
			continue
		}

		if err := bm.db.UpdateBrowserProfileRefresh(p.ID); err != nil {
			log.Printf("Browser refresh: failed to update refresh time for profile %d: %v", p.ID, err)
		}

		log.Printf("Browser refresh: profile %q cookies updated", p.ProfileName)
	}
}

// Close stops background tasks and cleans up all active sessions.
func (bm *BrowserManager) Close() {
	close(bm.stopRefresh)

	bm.mu.Lock()
	defer bm.mu.Unlock()

	for id, session := range bm.sessions {
		session.Close()
		delete(bm.sessions, id)
	}
}
