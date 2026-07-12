package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"github.com/growrig/growrig-platform/growcore/internal/store"
)

const (
	// sessionTTL is how long a login stays valid.
	sessionTTL = 30 * 24 * time.Hour
	// signupSettingKey toggles public self-registration; defaults off.
	signupSettingKey = "signup_enabled"
	authTokenBytes   = 32
)

type ctxKey int

const userCtxKey ctxKey = iota

// --- token helpers ---

func newToken() (string, error) {
	b := make([]byte, authTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken is what we persist; the raw token is only ever held by the client.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	// Fall back to a query token for the WebSocket, where browsers cannot set
	// request headers.
	return r.URL.Query().Get("token")
}

// userFromToken resolves the account behind a raw token, or nil if the token is
// empty, unknown, expired, or belongs to a disabled account.
func (s *Server) userFromToken(token string) *domain.User {
	if token == "" {
		return nil
	}
	userID, ok, err := s.store.SessionUserID(hashToken(token))
	if err != nil || !ok {
		return nil
	}
	u, ok, err := s.store.UserByID(userID)
	if err != nil || !ok || u.Disabled {
		return nil
	}
	return &u
}

// --- middleware & guards ---

// withAuth resolves the current user (if any) into the request context without
// rejecting: public routes still work, protected routes are gated by the
// require* wrappers below.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := s.userFromToken(bearerToken(r)); u != nil {
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) (*domain.User, bool) {
	u, ok := r.Context().Value(userCtxKey).(*domain.User)
	return u, ok
}

func (s *Server) requireAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentUser(r); !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("authentication required"))
			return
		}
		fn(w, r)
	}
}

func (s *Server) requireAdmin(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := currentUser(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("authentication required"))
			return
		}
		if u.Role != domain.UserRoleAdmin {
			writeJSON(w, http.StatusForbidden, errBody("administrator access required"))
			return
		}
		fn(w, r)
	}
}

// requireEnvAccess builds a guard checking the caller has at least the given
// level on the environment named by the {id} path value. Admins always pass.
func (s *Server) requireEnvAccess(level domain.AccessLevel, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := currentUser(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("authentication required"))
			return
		}
		if u.Role == domain.UserRoleAdmin {
			fn(w, r)
			return
		}
		grants, err := s.store.AccessForUser(u.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		have, granted := grants[r.PathValue("id")]
		if !granted || (level == domain.AccessWrite && !have.AllowsWrite()) {
			writeJSON(w, http.StatusForbidden, errBody("you do not have access to this environment"))
			return
		}
		fn(w, r)
	}
}

func (s *Server) requireEnvRead(fn http.HandlerFunc) http.HandlerFunc {
	return s.requireEnvAccess(domain.AccessRead, fn)
}

func (s *Server) requireEnvWrite(fn http.HandlerFunc) http.HandlerFunc {
	return s.requireEnvAccess(domain.AccessWrite, fn)
}

// requireEnvWriteForBinding guards a route keyed by a binding {id}: it resolves
// the binding's environment and requires write access there. Admins pass.
func (s *Server) requireEnvWriteForBinding(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := currentUser(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("authentication required"))
			return
		}
		if u.Role == domain.UserRoleAdmin {
			fn(w, r)
			return
		}
		bindings, err := s.store.Bindings()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		envID := ""
		for _, b := range bindings {
			if b.ID == r.PathValue("id") {
				envID = b.EnvironmentID
				break
			}
		}
		if envID == "" {
			writeJSON(w, http.StatusNotFound, errBody("binding not found"))
			return
		}
		grants, err := s.store.AccessForUser(u.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if have, ok := grants[envID]; !ok || !have.AllowsWrite() {
			writeJSON(w, http.StatusForbidden, errBody("you do not have access to this environment"))
			return
		}
		fn(w, r)
	}
}

// --- snapshot / list filtering ---

// accessibleEnvIDs returns the set of environment ids a user may view. The
// boolean all reports admin (unrestricted) access, in which case the set is nil.
func (s *Server) accessibleEnvIDs(u *domain.User) (set map[string]bool, all bool) {
	if u == nil {
		return map[string]bool{}, false
	}
	if u.Role == domain.UserRoleAdmin {
		return nil, true
	}
	grants, err := s.store.AccessForUser(u.ID)
	if err != nil {
		return map[string]bool{}, false
	}
	set = make(map[string]bool, len(grants))
	for envID := range grants {
		set[envID] = true
	}
	return set, false
}

// filterSnapshot returns a snapshot limited to the given environment ids.
func filterSnapshot(snap domain.Snapshot, allowed map[string]bool, all bool) domain.Snapshot {
	if all {
		return snap
	}
	envs := make([]domain.EnvironmentView, 0, len(snap.Environments))
	for _, e := range snap.Environments {
		if allowed[e.ID] {
			envs = append(envs, e)
		}
	}
	snap.Environments = envs
	return snap
}

// --- account creation shared by bootstrap / register / admin ---

func (s *Server) createAccount(username, password string, role domain.UserRole) (domain.User, error) {
	username = strings.TrimSpace(username)
	if err := validateCredentials(username, password); err != nil {
		return domain.User{}, err
	}
	hash, salt, err := domain.HashPassword(password)
	if err != nil {
		return domain.User{}, err
	}
	u := domain.User{
		ID:           id(username, "user"),
		Username:     username,
		Role:         role,
		PasswordHash: hash,
		PasswordSalt: salt,
		Created:      time.Now(),
	}
	if err := s.store.CreateUser(u); err != nil {
		return domain.User{}, err
	}
	return u, nil
}

func validateCredentials(username, password string) error {
	if len(username) < 3 {
		return fmt.Errorf("username must be at least 3 characters")
	}
	if len(username) > 40 {
		return fmt.Errorf("username must be at most 40 characters")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	return nil
}

// userView assembles the API view of an account, including its env grants.
func (s *Server) userView(u domain.User) (domain.UserView, error) {
	grants, err := s.store.AccessForUser(u.ID)
	if err != nil {
		return domain.UserView{}, err
	}
	access := make([]domain.EnvAccess, 0, len(grants))
	for envID, level := range grants {
		access = append(access, domain.EnvAccess{EnvironmentID: envID, Access: level})
	}
	sort.Slice(access, func(i, j int) bool { return access[i].EnvironmentID < access[j].EnvironmentID })
	return domain.UserView{User: u, Access: access}, nil
}

// issueSession creates a session for a user and returns the raw token.
func (s *Server) issueSession(userID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	if err := s.store.CreateSession(hashToken(token), userID, sessionTTL); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Server) signupEnabled() bool {
	v, _ := s.store.GetSetting(signupSettingKey, "false")
	return v == "true"
}

// --- handlers ---

type authResult struct {
	Token string          `json:"token"`
	User  domain.UserView `json:"user"`
}

func (s *Server) writeAuthResult(w http.ResponseWriter, status int, u domain.User) {
	token, err := s.issueSession(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	view, err := s.userView(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, status, authResult{Token: token, User: view})
}

type credentialsBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// getAuthStatus (public) reports whether first-run setup is needed and whether
// self-registration is currently allowed.
func (s *Server) getAuthStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"needsSetup":    count == 0,
		"signupEnabled": s.signupEnabled(),
	})
}

// bootstrap (public) creates the very first administrator. It is only permitted
// while no accounts exist.
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if count > 0 {
		writeJSON(w, http.StatusConflict, errBody("setup has already been completed"))
		return
	}
	var b credentialsBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	u, err := s.createAccount(b.Username, b.Password, domain.UserRoleAdmin)
	if err != nil {
		writeJSON(w, statusForUserErr(err), errBody(err.Error()))
		return
	}
	// Self-registration stays off until an admin explicitly enables it.
	_ = s.store.SetSetting(signupSettingKey, "false")
	s.activity("", "", "info", "configuration", "Created administrator "+u.Username)
	s.writeAuthResult(w, http.StatusCreated, u)
}

// login (public) exchanges credentials for a session token.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var b credentialsBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	u, ok, err := s.store.UserByUsername(strings.TrimSpace(b.Username))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok || u.Disabled || !domain.VerifyPassword(b.Password, u.PasswordHash, u.PasswordSalt) {
		writeJSON(w, http.StatusUnauthorized, errBody("invalid username or password"))
		return
	}
	s.writeAuthResult(w, http.StatusOK, u)
}

// register (public) creates a normal user, only when self-registration is on.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.signupEnabled() {
		writeJSON(w, http.StatusForbidden, errBody("self-registration is disabled"))
		return
	}
	var b credentialsBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	u, err := s.createAccount(b.Username, b.Password, domain.UserRoleUser)
	if err != nil {
		writeJSON(w, statusForUserErr(err), errBody(err.Error()))
		return
	}
	s.activity("", "", "info", "configuration", "New user registered: "+u.Username)
	s.writeAuthResult(w, http.StatusCreated, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if token := bearerToken(r); token != "" {
		_ = s.store.DeleteSession(hashToken(token))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	view, err := s.userView(*u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// statusForUserErr maps account-creation errors to HTTP status codes.
func statusForUserErr(err error) int {
	if errors.Is(err, store.ErrUsernameTaken) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}
