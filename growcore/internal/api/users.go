package api

import (
	"fmt"
	"net/http"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// All handlers in this file are wired behind requireAdmin.

func (s *Server) getUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.Users()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]domain.UserView, 0, len(users))
	for _, u := range users {
		view, err := s.userView(u)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, views)
}

type createUserBody struct {
	Username string             `json:"username"`
	Password string             `json:"password"`
	Role     domain.UserRole    `json:"role"`
	Access   []domain.EnvAccess `json:"access"`
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var b createUserBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	role := b.Role
	if role == "" {
		role = domain.UserRoleUser
	}
	if role != domain.UserRoleAdmin && role != domain.UserRoleUser {
		writeJSON(w, http.StatusBadRequest, errBody(fmt.Sprintf("unknown role %q", role)))
		return
	}
	u, err := s.createAccount(b.Username, b.Password, role)
	if err != nil {
		writeJSON(w, statusForUserErr(err), errBody(err.Error()))
		return
	}
	if err := s.applyAccess(u.ID, role, b.Access); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity("", "", "info", "configuration", "Created user "+u.Username)
	view, err := s.userView(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

type updateUserBody struct {
	Role     *domain.UserRole    `json:"role"`
	Disabled *bool               `json:"disabled"`
	Password *string             `json:"password"`
	Access   *[]domain.EnvAccess `json:"access"`
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target, ok, err := s.store.UserByID(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("user not found"))
		return
	}
	var b updateUserBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// Guard against locking everyone out: the last enabled admin cannot be
	// demoted or disabled.
	demoting := b.Role != nil && *b.Role != domain.UserRoleAdmin
	disabling := b.Disabled != nil && *b.Disabled
	if target.Role == domain.UserRoleAdmin && !target.Disabled && (demoting || disabling) {
		admins, err := s.store.CountAdmins()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if admins <= 1 {
			writeJSON(w, http.StatusConflict, errBody("cannot demote or disable the last administrator"))
			return
		}
	}

	role := target.Role
	if b.Role != nil {
		if *b.Role != domain.UserRoleAdmin && *b.Role != domain.UserRoleUser {
			writeJSON(w, http.StatusBadRequest, errBody(fmt.Sprintf("unknown role %q", *b.Role)))
			return
		}
		role = *b.Role
		if err := s.store.SetUserRole(id, role); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if b.Disabled != nil {
		if err := s.store.SetUserDisabled(id, *b.Disabled); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if b.Password != nil {
		if err := validateCredentials(target.Username, *b.Password); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
			return
		}
		hash, salt, err := domain.HashPassword(*b.Password)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := s.store.SetUserPassword(id, hash, salt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if b.Access != nil {
		if err := s.applyAccess(id, role, *b.Access); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	updated, _, err := s.store.UserByID(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	view, err := s.userView(updated)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity("", "", "info", "configuration", "Updated user "+updated.Username)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if me, ok := currentUser(r); ok && me.ID == id {
		writeJSON(w, http.StatusConflict, errBody("you cannot delete your own account"))
		return
	}
	target, ok, err := s.store.UserByID(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("user not found"))
		return
	}
	if target.Role == domain.UserRoleAdmin && !target.Disabled {
		admins, err := s.store.CountAdmins()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if admins <= 1 {
			writeJSON(w, http.StatusConflict, errBody("cannot delete the last administrator"))
			return
		}
	}
	if err := s.store.DeleteUser(id); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.activity("", "", "info", "configuration", "Deleted user "+target.Username)
	w.WriteHeader(http.StatusNoContent)
}

// applyAccess persists a user's env grants. Admins have implicit access to
// everything, so their explicit grant list is cleared to avoid confusion.
func (s *Server) applyAccess(userID string, role domain.UserRole, access []domain.EnvAccess) error {
	if role == domain.UserRoleAdmin {
		return s.store.SetUserAccess(userID, nil)
	}
	envs, err := s.store.Environments()
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(envs))
	for _, e := range envs {
		known[e.ID] = true
	}
	valid := make([]domain.EnvAccess, 0, len(access))
	for _, g := range access {
		if known[g.EnvironmentID] {
			valid = append(valid, g)
		}
	}
	return s.store.SetUserAccess(userID, valid)
}

// --- self-registration setting ---

func (s *Server) getSignupSetting(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.signupEnabled()})
}

func (s *Server) setSignupSetting(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	value := "false"
	if b.Enabled {
		value = "true"
	}
	if err := s.store.SetSetting(signupSettingKey, value); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": b.Enabled})
}
