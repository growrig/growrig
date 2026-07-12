package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// ErrUsernameTaken is returned when creating a user whose username already exists.
var ErrUsernameTaken = errors.New("username already taken")

// --- Users ---

// CountUsers returns the number of accounts, used to detect first-run setup.
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a new account. The caller supplies an already-hashed
// password. Returns ErrUsernameTaken on a unique-constraint violation.
func (s *Store) CreateUser(u domain.User) error {
	if u.Created.IsZero() {
		u.Created = time.Now()
	}
	if u.Role == "" {
		u.Role = domain.UserRoleUser
	}
	_, err := s.db.Exec(
		`INSERT INTO users (id, username, password_hash, password_salt, role, disabled, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.PasswordHash, u.PasswordSalt, string(u.Role), boolToInt(u.Disabled), u.Created.UnixMilli())
	if err != nil && isUniqueViolation(err) {
		return ErrUsernameTaken
	}
	return err
}

func (s *Store) scanUser(row interface{ Scan(...any) error }) (domain.User, error) {
	var u domain.User
	var role string
	var disabled int
	var created int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.PasswordSalt, &role, &disabled, &created); err != nil {
		return domain.User{}, err
	}
	u.Role = domain.UserRole(role)
	u.Disabled = disabled != 0
	u.Created = time.UnixMilli(created)
	return u, nil
}

const userCols = `id, username, password_hash, password_salt, role, disabled, created`

// UserByUsername looks up an account by (case-insensitive) username.
func (s *Store) UserByUsername(username string) (domain.User, bool, error) {
	u, err := s.scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username=? COLLATE NOCASE`, username))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, err
	}
	return u, true, nil
}

// UserByID looks up an account by id.
func (s *Store) UserByID(id string) (domain.User, bool, error) {
	u, err := s.scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, err
	}
	return u, true, nil
}

// Users returns all accounts, oldest first.
func (s *Store) Users() ([]domain.User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins returns the number of enabled administrators, used to prevent
// removing or demoting the last admin (self-lockout).
func (s *Store) CountAdmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role=? AND disabled=0`, string(domain.UserRoleAdmin)).Scan(&n)
	return n, err
}

func (s *Store) SetUserRole(id string, role domain.UserRole) error {
	_, err := s.db.Exec(`UPDATE users SET role=? WHERE id=?`, string(role), id)
	return err
}

func (s *Store) SetUserDisabled(id string, disabled bool) error {
	_, err := s.db.Exec(`UPDATE users SET disabled=? WHERE id=?`, boolToInt(disabled), id)
	return err
}

// SetUserPassword updates the stored hash/salt and invalidates existing sessions.
func (s *Store) SetUserPassword(id, hash, salt string) error {
	if _, err := s.db.Exec(`UPDATE users SET password_hash=?, password_salt=? WHERE id=?`, hash, salt, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	return err
}

// DeleteUser removes an account and cascades its grants, sessions and passkeys.
func (s *Store) DeleteUser(id string) error {
	_, _ = s.db.Exec(`DELETE FROM env_access WHERE user_id=?`, id)
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	_, _ = s.db.Exec(`DELETE FROM webauthn_credentials WHERE user_id=?`, id)
	res, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %q not found", id)
	}
	return nil
}

// --- Per-environment access ---

// AccessForUser returns the user's grants as a map of environment id to level.
func (s *Store) AccessForUser(userID string) (map[string]domain.AccessLevel, error) {
	rows, err := s.db.Query(`SELECT environment_id, access FROM env_access WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.AccessLevel{}
	for rows.Next() {
		var env, access string
		if err := rows.Scan(&env, &access); err != nil {
			return nil, err
		}
		out[env] = domain.AccessLevel(access)
	}
	return out, rows.Err()
}

// SetUserAccess replaces all of a user's grants with the provided set in one
// transaction. Grants referencing unknown environments are the caller's concern.
func (s *Store) SetUserAccess(userID string, grants []domain.EnvAccess) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM env_access WHERE user_id=?`, userID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO env_access (user_id, environment_id, access) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, g := range grants {
		access := g.Access
		if access != domain.AccessRead && access != domain.AccessWrite {
			access = domain.AccessRead
		}
		if _, err := stmt.Exec(userID, g.EnvironmentID, string(access)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- WebAuthn credentials (passkeys) ---

// SaveCredential inserts or updates a stored passkey.
func (s *Store) SaveCredential(c domain.StoredCredential) error {
	if c.Created.IsZero() {
		c.Created = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT INTO webauthn_credentials (id, user_id, name, created, data) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, data=excluded.data`,
		c.ID, c.UserID, c.Name, c.Created.UnixMilli(), string(c.Data))
	return err
}

// CredentialsForUser returns a user's stored passkeys, oldest first.
func (s *Store) CredentialsForUser(userID string) ([]domain.StoredCredential, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, name, created, data FROM webauthn_credentials WHERE user_id=? ORDER BY created`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.StoredCredential
	for rows.Next() {
		var c domain.StoredCredential
		var created int64
		var data string
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &created, &data); err != nil {
			return nil, err
		}
		c.Created = time.UnixMilli(created)
		c.Data = []byte(data)
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCredentialData rewrites a credential's stored record (e.g. after a
// successful login bumps its signature counter).
func (s *Store) UpdateCredentialData(id string, data []byte) error {
	_, err := s.db.Exec(`UPDATE webauthn_credentials SET data=? WHERE id=?`, string(data), id)
	return err
}

// DeleteCredential removes one of a user's passkeys.
func (s *Store) DeleteCredential(userID, id string) error {
	res, err := s.db.Exec(`DELETE FROM webauthn_credentials WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("passkey not found")
	}
	return nil
}

// --- Sessions ---

// CreateSession stores a session keyed by the token's hash, expiring after ttl.
func (s *Store) CreateSession(tokenHash, userID string, ttl time.Duration) error {
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, created, expires) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, now.UnixMilli(), now.Add(ttl).UnixMilli())
	return err
}

// SessionUserID returns the user id for a live (non-expired) session token hash.
// Expired sessions are treated as absent and opportunistically deleted.
func (s *Store) SessionUserID(tokenHash string) (string, bool, error) {
	var userID string
	var expires int64
	err := s.db.QueryRow(`SELECT user_id, expires FROM sessions WHERE token_hash=?`, tokenHash).Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if time.Now().UnixMilli() > expires {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash)
		return "", false, nil
	}
	return userID, true, nil
}

// DeleteSession removes a single session (logout).
func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

// PurgeExpiredSessions removes all sessions past their expiry.
func (s *Store) PurgeExpiredSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires < ?`, time.Now().UnixMilli())
	return err
}

// --- Settings (key/value) ---

// GetSetting returns a setting value, or def when the key is absent.
func (s *Store) GetSetting(key, def string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
