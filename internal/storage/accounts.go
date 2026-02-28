package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Account represents a stored cookie account.
type Account struct {
	ID                int    `json:"id"`
	Secure1PSID       string `json:"secure_1psid"`
	Secure1PSIDTS     string `json:"secure_1psidts"`
	Nickname          string `json:"nickname"`
	IsActive          bool   `json:"is_active"`
	UseCount          int    `json:"use_count"`
	ErrorCount        int    `json:"error_count"`
	ConsecutiveErrors int    `json:"consecutive_errors"`
	BanReason         string `json:"ban_reason,omitempty"`
	BannedAt          string `json:"banned_at,omitempty"`
	LastUsedAt        string `json:"last_used_at,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// ListAccounts returns all accounts.
func (db *DB) ListAccounts() ([]Account, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	rows, err := db.conn.Query(`SELECT id, secure_1psid, secure_1psidts, nickname, is_active,
		use_count, error_count, consecutive_errors, ban_reason, banned_at,
		last_used_at, last_error, created_at, updated_at
		FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccounts(rows)
}

// ListActiveAccounts returns only active, non-banned accounts.
func (db *DB) ListActiveAccounts() ([]Account, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	rows, err := db.conn.Query(`SELECT id, secure_1psid, secure_1psidts, nickname, is_active,
		use_count, error_count, consecutive_errors, ban_reason, banned_at,
		last_used_at, last_error, created_at, updated_at
		FROM accounts WHERE is_active = 1 AND (banned_at IS NULL OR banned_at = '')
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccounts(rows)
}

// GetAccount returns a single account by ID.
func (db *DB) GetAccount(id int) (*Account, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	a := &Account{}
	var banReason, bannedAt, lastUsedAt, lastError sql.NullString
	err := db.conn.QueryRow(`SELECT id, secure_1psid, secure_1psidts, nickname, is_active,
		use_count, error_count, consecutive_errors, ban_reason, banned_at,
		last_used_at, last_error, created_at, updated_at
		FROM accounts WHERE id = ?`, id).Scan(
		&a.ID, &a.Secure1PSID, &a.Secure1PSIDTS, &a.Nickname, &a.IsActive,
		&a.UseCount, &a.ErrorCount, &a.ConsecutiveErrors, &banReason, &bannedAt,
		&lastUsedAt, &lastError, &a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.BanReason = banReason.String
	a.BannedAt = bannedAt.String
	a.LastUsedAt = lastUsedAt.String
	a.LastError = lastError.String
	return a, nil
}

// AddAccount creates a new account. Returns the new ID.
func (db *DB) AddAccount(psid, psidts, nickname string) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	result, err := db.conn.Exec(
		`INSERT INTO accounts (secure_1psid, secure_1psidts, nickname) VALUES (?, ?, ?)`,
		psid, psidts, nickname,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpsertByPSID updates an existing account matching the psid prefix, or inserts a new one.
// Returns the account ID and whether it was an insert (true) or update (false).
func (db *DB) UpsertByPSID(psid, psidts string) (int64, bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Check if an account with same psid exists (match first 20 chars)
	prefix := psid
	if len(prefix) > 20 {
		prefix = prefix[:20]
	}

	var existingID int64
	err := db.conn.QueryRow(`SELECT id FROM accounts WHERE secure_1psid LIKE ?`, prefix+"%").Scan(&existingID)
	if err == nil {
		// Update existing
		_, err = db.conn.Exec(
			`UPDATE accounts SET secure_1psid = ?, secure_1psidts = ?,
			 consecutive_errors = 0, is_active = 1, ban_reason = NULL, banned_at = NULL,
			 updated_at = datetime('now')
			 WHERE id = ?`,
			psid, psidts, existingID,
		)
		return existingID, false, err
	}

	// Insert new
	result, err := db.conn.Exec(
		`INSERT INTO accounts (secure_1psid, secure_1psidts) VALUES (?, ?)`,
		psid, psidts,
	)
	if err != nil {
		return 0, false, err
	}
	id, err := result.LastInsertId()
	return id, true, err
}

// DeleteAccount removes an account by ID.
func (db *DB) DeleteAccount(id int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	return err
}

// EnableAccount sets an account to active and clears ban state.
func (db *DB) EnableAccount(id int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE accounts SET is_active = 1, consecutive_errors = 0,
		 ban_reason = NULL, banned_at = NULL, updated_at = datetime('now')
		 WHERE id = ?`, id)
	return err
}

// DisableAccount sets an account to inactive.
func (db *DB) DisableAccount(id int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE accounts SET is_active = 0, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// RecordUse increments use_count and sets last_used_at.
func (db *DB) RecordUse(id int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE accounts SET use_count = use_count + 1, last_used_at = datetime('now'),
		 updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// RecordError increments error counts and optionally bans at threshold.
func (db *DB) RecordError(id int, errMsg string, banThreshold int) (banned bool, err error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, err = db.conn.Exec(
		`UPDATE accounts SET error_count = error_count + 1,
		 consecutive_errors = consecutive_errors + 1,
		 last_error = ?, updated_at = datetime('now')
		 WHERE id = ?`, errMsg, id)
	if err != nil {
		return false, err
	}

	// Check if we should ban
	var consec int
	if err = db.conn.QueryRow(`SELECT consecutive_errors FROM accounts WHERE id = ?`, id).Scan(&consec); err != nil {
		return false, err
	}

	if banThreshold > 0 && consec >= banThreshold {
		_, err = db.conn.Exec(
			`UPDATE accounts SET ban_reason = ?, banned_at = datetime('now'),
			 updated_at = datetime('now') WHERE id = ?`,
			fmt.Sprintf("consecutive errors >= %d", banThreshold), id)
		return true, err
	}
	return false, nil
}

// RecordSuccess resets consecutive error count.
func (db *DB) RecordSuccess(id int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE accounts SET consecutive_errors = 0, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// UnbanExpiredAccounts unbans accounts that were banned longer than duration ago.
func (db *DB) UnbanExpiredAccounts(duration time.Duration) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	cutoff := time.Now().Add(-duration).UTC().Format("2006-01-02 15:04:05")
	result, err := db.conn.Exec(
		`UPDATE accounts SET ban_reason = NULL, banned_at = NULL, consecutive_errors = 0,
		 updated_at = datetime('now')
		 WHERE banned_at IS NOT NULL AND banned_at != '' AND banned_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// AccountCount returns total and active account counts.
func (db *DB) AccountCount() (total int, active int, err error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&total)
	if err != nil {
		return
	}
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM accounts WHERE is_active = 1 AND (banned_at IS NULL OR banned_at = '')`).Scan(&active)
	return
}

func scanAccounts(rows *sql.Rows) ([]Account, error) {
	var accounts []Account
	for rows.Next() {
		a := Account{}
		var banReason, bannedAt, lastUsedAt, lastError sql.NullString
		err := rows.Scan(
			&a.ID, &a.Secure1PSID, &a.Secure1PSIDTS, &a.Nickname, &a.IsActive,
			&a.UseCount, &a.ErrorCount, &a.ConsecutiveErrors, &banReason, &bannedAt,
			&lastUsedAt, &lastError, &a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		a.BanReason = banReason.String
		a.BannedAt = bannedAt.String
		a.LastUsedAt = lastUsedAt.String
		a.LastError = lastError.String
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
