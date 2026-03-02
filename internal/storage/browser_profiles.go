package storage

import (
	"database/sql"
	"fmt"
)

// BrowserProfile represents a Chrome browser profile for Google login.
type BrowserProfile struct {
	ID          int     `json:"id"`
	AccountID   *int    `json:"account_id,omitempty"`
	ProfileName string  `json:"profile_name"`
	ProfileDir  string  `json:"profile_dir"`
	Status      string  `json:"status"` // pending, logging_in, active, error
	LastRefresh *string `json:"last_refresh,omitempty"`
	LastError   *string `json:"last_error,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// CreateBrowserProfile creates a new browser profile record.
func (db *DB) CreateBrowserProfile(name, dir string) (*BrowserProfile, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	res, err := db.conn.Exec(
		`INSERT INTO browser_profiles (profile_name, profile_dir) VALUES (?, ?)`,
		name, dir,
	)
	if err != nil {
		return nil, fmt.Errorf("create browser profile: %w", err)
	}
	id, _ := res.LastInsertId()
	return db.getBrowserProfileLocked(int(id))
}

// GetBrowserProfile retrieves a browser profile by ID.
func (db *DB) GetBrowserProfile(id int) (*BrowserProfile, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.getBrowserProfileLocked(id)
}

func (db *DB) getBrowserProfileLocked(id int) (*BrowserProfile, error) {
	p := &BrowserProfile{}
	err := db.conn.QueryRow(
		`SELECT id, account_id, profile_name, profile_dir, status, last_refresh, last_error, created_at, updated_at
		 FROM browser_profiles WHERE id = ?`, id,
	).Scan(&p.ID, &p.AccountID, &p.ProfileName, &p.ProfileDir, &p.Status, &p.LastRefresh, &p.LastError, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("browser profile %d not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get browser profile: %w", err)
	}
	return p, nil
}

// ListBrowserProfiles returns all browser profiles.
func (db *DB) ListBrowserProfiles() ([]*BrowserProfile, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	rows, err := db.conn.Query(
		`SELECT id, account_id, profile_name, profile_dir, status, last_refresh, last_error, created_at, updated_at
		 FROM browser_profiles ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list browser profiles: %w", err)
	}
	defer rows.Close()

	var profiles []*BrowserProfile
	for rows.Next() {
		p := &BrowserProfile{}
		if err := rows.Scan(&p.ID, &p.AccountID, &p.ProfileName, &p.ProfileDir, &p.Status, &p.LastRefresh, &p.LastError, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan browser profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// UpdateBrowserProfileStatus sets the status of a browser profile.
func (db *DB) UpdateBrowserProfileStatus(id int, status string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE browser_profiles SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		status, id,
	)
	return err
}

// LinkBrowserProfileAccount links a browser profile to an account.
func (db *DB) LinkBrowserProfileAccount(profileID, accountID int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE browser_profiles SET account_id = ?, updated_at = datetime('now') WHERE id = ?`,
		accountID, profileID,
	)
	return err
}

// UpdateBrowserProfileRefresh marks a successful cookie refresh.
func (db *DB) UpdateBrowserProfileRefresh(id int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE browser_profiles SET last_refresh = datetime('now'), last_error = NULL, status = 'active', updated_at = datetime('now') WHERE id = ?`,
		id,
	)
	return err
}

// UpdateBrowserProfileError records a refresh error.
func (db *DB) UpdateBrowserProfileError(id int, errMsg string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE browser_profiles SET last_error = ?, status = 'error', updated_at = datetime('now') WHERE id = ?`,
		errMsg, id,
	)
	return err
}

// DeleteBrowserProfile removes a browser profile record.
func (db *DB) DeleteBrowserProfile(id int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(`DELETE FROM browser_profiles WHERE id = ?`, id)
	return err
}

// ListActiveBrowserProfiles returns profiles that are active (for auto-refresh).
func (db *DB) ListActiveBrowserProfiles() ([]*BrowserProfile, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	rows, err := db.conn.Query(
		`SELECT id, account_id, profile_name, profile_dir, status, last_refresh, last_error, created_at, updated_at
		 FROM browser_profiles WHERE status = 'active' ORDER BY last_refresh ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list active browser profiles: %w", err)
	}
	defer rows.Close()

	var profiles []*BrowserProfile
	for rows.Next() {
		p := &BrowserProfile{}
		if err := rows.Scan(&p.ID, &p.AccountID, &p.ProfileName, &p.ProfileDir, &p.Status, &p.LastRefresh, &p.LastError, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan browser profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// UpdateAccountCookies updates the cookies for an existing account by ID.
func (db *DB) UpdateAccountCookies(id int, psid, psidts string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE accounts SET secure_1psid = ?, secure_1psidts = ?, consecutive_errors = 0, ban_reason = NULL, banned_at = NULL, updated_at = datetime('now') WHERE id = ?`,
		psid, psidts, id,
	)
	return err
}
