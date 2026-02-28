package pool

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"gem2api/internal/storage"
)

// Pool manages a pool of cookie accounts with random load balancing and health tracking.
type Pool struct {
	db             *storage.DB
	errorThreshold int
	autoUnbanAfter time.Duration
	mu             sync.Mutex
	stopUnban      chan struct{}
}

// CookiePair holds a selected account's cookies and ID for request use.
type CookiePair struct {
	AccountID     int
	Secure1PSID   string
	Secure1PSIDTS string
}

// NewPool creates a new cookie pool.
func NewPool(db *storage.DB, errorThreshold int, autoUnbanAfter time.Duration) *Pool {
	return &Pool{
		db:             db,
		errorThreshold: errorThreshold,
		autoUnbanAfter: autoUnbanAfter,
		stopUnban:      make(chan struct{}),
	}
}

// Pick selects a random active, non-banned account from the pool.
// Returns nil if no accounts are available.
func (p *Pool) Pick() (*CookiePair, error) {
	accounts, err := p.db.ListActiveAccounts()
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, nil
	}

	// Random selection
	idx := rand.Intn(len(accounts))
	a := accounts[idx]

	// Record usage
	if err := p.db.RecordUse(a.ID); err != nil {
		log.Printf("Warning: failed to record use for account %d: %v", a.ID, err)
	}

	return &CookiePair{
		AccountID:     a.ID,
		Secure1PSID:   a.Secure1PSID,
		Secure1PSIDTS: a.Secure1PSIDTS,
	}, nil
}

// RecordSuccess records a successful request for an account.
func (p *Pool) RecordSuccess(accountID int) {
	if err := p.db.RecordSuccess(accountID); err != nil {
		log.Printf("Warning: failed to record success for account %d: %v", accountID, err)
	}
}

// RecordError records a failed request and potentially bans the account.
func (p *Pool) RecordError(accountID int, errMsg string) bool {
	banned, err := p.db.RecordError(accountID, errMsg, p.errorThreshold)
	if err != nil {
		log.Printf("Warning: failed to record error for account %d: %v", accountID, err)
	}
	if banned {
		log.Printf("Account %d auto-banned: consecutive errors >= %d", accountID, p.errorThreshold)
	}
	return banned
}

// StartAutoUnban runs a background goroutine to unban expired accounts.
func (p *Pool) StartAutoUnban() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				count, err := p.db.UnbanExpiredAccounts(p.autoUnbanAfter)
				if err != nil {
					log.Printf("Error unbanning accounts: %v", err)
				} else if count > 0 {
					log.Printf("Auto-unbanned %d accounts", count)
				}
			case <-p.stopUnban:
				return
			}
		}
	}()
}

// Stop stops the auto-unban goroutine.
func (p *Pool) Stop() {
	close(p.stopUnban)
}

// HasAccounts returns true if the pool has any accounts (active or not).
func (p *Pool) HasAccounts() bool {
	total, _, err := p.db.AccountCount()
	if err != nil {
		return false
	}
	return total > 0
}

// Stats returns pool statistics.
func (p *Pool) Stats() (total int, active int, err error) {
	return p.db.AccountCount()
}
