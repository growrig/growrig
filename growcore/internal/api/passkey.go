package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// Passkeys (WebAuthn) sit alongside passwords: any signed-in user can register
// one or more, then sign in with it. The Relying Party is derived per-request
// from the browser's Origin so it works both in cross-origin development and in
// the single-origin embedded build without configuration.

const ceremonyTTL = 5 * time.Minute

// --- per-request Relying Party ---

func (s *Server) webAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	rpID := origin
	if u, err := url.Parse(origin); err == nil && u.Hostname() != "" {
		rpID = u.Hostname()
	}
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "GrowRig",
		RPOrigins:     []string{origin},
	})
}

// --- webauthn.User adapter ---

// webAuthnUser adapts a domain account plus its stored passkeys to the
// webauthn.User interface. The user handle is the account id.
type webAuthnUser struct {
	user  domain.User
	creds []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte                         { return []byte(u.user.ID) }
func (u *webAuthnUser) WebAuthnName() string                       { return u.user.Username }
func (u *webAuthnUser) WebAuthnDisplayName() string                { return u.user.Username }
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// waUser loads a user and their credentials into the webauthn adapter.
func (s *Server) waUser(u domain.User) (*webAuthnUser, error) {
	stored, err := s.store.CredentialsForUser(u.ID)
	if err != nil {
		return nil, err
	}
	creds := make([]webauthn.Credential, 0, len(stored))
	for _, sc := range stored {
		var c webauthn.Credential
		if err := json.Unmarshal(sc.Data, &c); err != nil {
			continue // skip an unreadable record rather than break login
		}
		creds = append(creds, c)
	}
	return &webAuthnUser{user: u, creds: creds}, nil
}

func credIDString(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

// storeCredential persists a freshly created/updated webauthn credential.
func (s *Server) storeCredential(userID, name string, c *webauthn.Credential) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.store.SaveCredential(domain.StoredCredential{
		ID:      credIDString(c.ID),
		UserID:  userID,
		Name:    name,
		Created: time.Now(),
		Data:    data,
	})
}

// --- in-memory ceremony store ---

// ceremonyStore holds the short-lived WebAuthn SessionData between the begin
// and finish steps of a ceremony, keyed by an opaque handle the client echoes
// back. In-memory is sufficient for a single-instance grow controller; a lost
// entry (restart mid-ceremony) simply requires the user to retry.
type ceremonyStore struct {
	mu    sync.Mutex
	items map[string]ceremonyItem
}

type ceremonyItem struct {
	session webauthn.SessionData
	userID  string
	expires time.Time
}

func newCeremonyStore() *ceremonyStore { return &ceremonyStore{items: map[string]ceremonyItem{}} }

func (c *ceremonyStore) put(session webauthn.SessionData, userID string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	handle := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	// Opportunistically evict expired entries.
	now := time.Now()
	for k, v := range c.items {
		if now.After(v.expires) {
			delete(c.items, k)
		}
	}
	c.items[handle] = ceremonyItem{session: session, userID: userID, expires: now.Add(ceremonyTTL)}
	return handle, nil
}

func (c *ceremonyStore) take(handle string) (ceremonyItem, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[handle]
	if ok {
		delete(c.items, handle)
	}
	if ok && time.Now().After(item.expires) {
		return ceremonyItem{}, false
	}
	return item, ok
}

// --- registration (authenticated user adds a passkey) ---

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	wa, err := s.webAuthn(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	waUser, err := s.waUser(*u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	options, session, err := wa.BeginRegistration(waUser)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	handle, err := s.passkeys.put(*session, u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicKey": options.Response, "handle": handle})
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	item, ok := s.passkeys.take(r.URL.Query().Get("handle"))
	if !ok || item.userID != u.ID {
		writeJSON(w, http.StatusBadRequest, errBody("passkey registration expired; please try again"))
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	waUser, err := s.waUser(*u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	credential, err := wa.FinishRegistration(waUser, item.session, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(passkeyError(err)))
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "Passkey"
	}
	if err := s.storeCredential(u.ID, name, credential); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity("", "", "info", "configuration", "Registered a passkey for "+u.Username)
	writeJSON(w, http.StatusCreated, domain.StoredCredential{
		ID: credIDString(credential.ID), Name: name, Created: time.Now(),
	})
}

// --- login (public, discoverable/usernameless) ---

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	wa, err := s.webAuthn(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	options, session, err := wa.BeginDiscoverableLogin()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	handle, err := s.passkeys.put(*session, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicKey": options.Response, "handle": handle})
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	item, ok := s.passkeys.take(r.URL.Query().Get("handle"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody("passkey sign-in expired; please try again"))
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// The handler resolves the account from the credential's user handle (the
	// account id) so login is usernameless.
	var resolved *domain.User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, found, err := s.store.UserByID(string(userHandle))
		if err != nil {
			return nil, err
		}
		if !found || u.Disabled {
			return nil, fmt.Errorf("account not available")
		}
		resolved = &u
		return s.waUser(u)
	}
	_, credential, err := wa.FinishPasskeyLogin(handler, item.session, r)
	if err != nil || resolved == nil {
		writeJSON(w, http.StatusUnauthorized, errBody("passkey sign-in failed"))
		return
	}
	// Persist the bumped signature counter / backup state.
	if data, mErr := json.Marshal(credential); mErr == nil {
		_ = s.store.UpdateCredentialData(credIDString(credential.ID), data)
	}
	s.writeAuthResult(w, http.StatusOK, *resolved)
}

// --- passkey management (authenticated) ---

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	creds, err := s.store.CredentialsForUser(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Never leak the raw credential record; expose only id/name/created.
	out := make([]domain.StoredCredential, 0, len(creds))
	for _, c := range creds {
		out = append(out, domain.StoredCredential{ID: c.ID, Name: c.Name, Created: c.Created})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	if err := s.store.DeleteCredential(u.ID, r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, errBody(err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// passkeyError surfaces a concise message from the (often verbose) WebAuthn
// validation errors.
func passkeyError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ":"); i > 0 && i < 60 {
		return msg
	}
	return "passkey could not be verified"
}
