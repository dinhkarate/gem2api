package browser

import (
	"context"
	"fmt"
	"log"
	"time"

	"gem2api/internal/storage"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// RefreshCookies launches a headless Chrome with the saved profile, navigates
// to gemini.google.com, and extracts fresh cookies.
func (bm *BrowserManager) RefreshCookies(profile *storage.BrowserProfile) (*CookieResult, error) {
	dir := bm.profileDir(profile.ProfileName)

	// Launch headless Chrome with saved profile
	opts := bm.chromeOpts(dir, true)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Set timeout for the entire refresh operation
	ctx, timeoutCancel := context.WithTimeout(ctx, 60*time.Second)
	defer timeoutCancel()

	// Anti-detection: remove webdriver flag
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := page.Enable().Do(ctx); err != nil {
				return err
			}
			_, err := page.AddScriptToEvaluateOnNewDocument(`
				Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
				delete navigator.__proto__.webdriver;
				window.chrome = {runtime: {}};
			`).Do(ctx)
			return err
		}),
	); err != nil {
		log.Printf("Refresh profile %q: anti-detection setup failed: %v", profile.ProfileName, err)
		// Continue anyway - not critical
	}

	// Navigate to Gemini to trigger cookie refresh
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://gemini.google.com/"),
		chromedp.WaitReady("body"),
	); err != nil {
		return nil, fmt.Errorf("navigate to gemini: %w", err)
	}

	// Small delay to let cookies settle
	time.Sleep(2 * time.Second)

	// Extract cookies
	cookies, err := extractGoogleCookies(ctx)
	if err != nil {
		return nil, fmt.Errorf("extract cookies: %w", err)
	}

	return cookies, nil
}
