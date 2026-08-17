package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kooler/freesupp/internal/auth"
	"github.com/kooler/freesupp/internal/store"
)

type userResponse struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

func buildUserResponse(u *store.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt}
}

// handleAuthConfig tells the inbox whether to show the first-run setup screen
// instead of the login form. Whether any user exists is public by necessity:
// the setup screen must show before anyone can authenticate.
func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.log.Error("counting users", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load sign-in options")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		SetupRequired bool `json:"setup_required"`
	}{SetupRequired: count == 0})
}

// handleSetup creates the first account. It only works while no users exist,
// which is what stands in for authentication on a fresh install.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	email, hash, ok := s.validCredentials(w, req.Email, req.Password)
	if !ok {
		return
	}

	user, err := s.store.CreateFirstAdmin(r.Context(), email, hash)
	switch {
	case errors.Is(err, store.ErrSetupDone):
		writeError(w, http.StatusForbidden, "this inbox is already set up")
		return
	case err != nil:
		s.log.Error("creating first admin", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create your account, please try again")
		return
	}

	s.log.Info("first admin created", "email", user.Email)
	s.setSession(w, user.Email)
	writeJSON(w, http.StatusCreated, meResponse(user))
}

// handlePasswordLogin signs an operator in with email and password.
func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	user, err := s.userIfExists(r.Context(), req.Email)
	if err != nil {
		s.log.Error("looking up login user", "err", err)
		writeError(w, http.StatusInternalServerError, "could not sign you in, please try again")
		return
	}
	// A missing user still burns a hash comparison so the response time does
	// not reveal which addresses have accounts.
	hash := ""
	if user != nil {
		hash = user.PasswordHash
	}
	if auth.CheckPassword(hash, req.Password) != nil {
		writeError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	s.setSession(w, user.Email)
	writeJSON(w, http.StatusOK, meResponse(user))
}

// handleChangeMyPassword lets any signed-in operator rotate their password.
func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	user := userFrom(r.Context())
	if auth.CheckPassword(user.PasswordHash, req.CurrentPassword) != nil {
		writeError(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	hash, ok := s.hashValidPassword(w, req.NewPassword)
	if !ok {
		return
	}
	if err := s.store.SetUserPassword(r.Context(), user.ID, hash); err != nil {
		s.log.Error("changing own password", "err", err)
		writeError(w, http.StatusInternalServerError, "could not change your password, please try again")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListUsers returns every account for the team management screen.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.log.Error("listing users", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load users")
		return
	}
	out := struct {
		Users []userResponse `json:"users"`
	}{Users: make([]userResponse, 0, len(users))}
	for i := range users {
		out.Users = append(out.Users, buildUserResponse(&users[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateUser adds an account with an initial password the admin shares
// out of band; there is no email confirmation.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	email, hash, ok := s.validCredentials(w, req.Email, req.Password)
	if !ok {
		return
	}

	user, err := s.store.CreateUser(r.Context(), email, hash, req.IsAdmin)
	switch {
	case errors.Is(err, store.ErrEmailTaken):
		writeError(w, http.StatusConflict, "a user with this email already exists")
		return
	case err != nil:
		s.log.Error("creating user", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create the user, please try again")
		return
	}
	writeJSON(w, http.StatusCreated, buildUserResponse(user))
}

// handleDeleteUser removes an account. Self-deletion is blocked, which also
// guarantees at least one admin (the caller) survives every delete.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	target, ok := s.userByParam(w, r)
	if !ok {
		return
	}
	if target.ID == userFrom(r.Context()).ID {
		writeError(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	if err := s.store.DeleteUser(r.Context(), target.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("deleting user", "user_id", target.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not delete the user, please try again")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetUserAdmin promotes or demotes an account.
func (s *Server) handleSetUserAdmin(w http.ResponseWriter, r *http.Request) {
	target, ok := s.userByParam(w, r)
	if !ok {
		return
	}
	var req struct {
		IsAdmin bool `json:"is_admin"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if !req.IsAdmin && target.IsAdmin {
		lastAdmin, err := s.isLastAdmin(r.Context(), w)
		if err != nil {
			return
		}
		if lastAdmin {
			writeError(w, http.StatusBadRequest, "the inbox needs at least one admin")
			return
		}
	}

	if err := s.store.SetUserAdmin(r.Context(), target.ID, req.IsAdmin); err != nil {
		s.log.Error("setting admin flag", "user_id", target.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not update the user, please try again")
		return
	}
	target.IsAdmin = req.IsAdmin
	writeJSON(w, http.StatusOK, buildUserResponse(target))
}

// handleResetUserPassword sets a new password for another account — the only
// recovery path for a forgotten password, since there is no email flow.
func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	target, ok := s.userByParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	hash, ok := s.hashValidPassword(w, req.Password)
	if !ok {
		return
	}
	if err := s.store.SetUserPassword(r.Context(), target.ID, hash); err != nil {
		s.log.Error("resetting user password", "user_id", target.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not reset the password, please try again")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) isLastAdmin(ctx context.Context, w http.ResponseWriter) (bool, error) {
	admins, err := s.store.CountAdmins(ctx)
	if err != nil {
		s.log.Error("counting admins", "err", err)
		writeError(w, http.StatusInternalServerError, "could not update the user, please try again")
		return false, err
	}
	return admins <= 1, nil
}

func (s *Server) userByParam(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return nil, false
	}
	user, err := s.store.UserByID(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "user not found")
		return nil, false
	case err != nil:
		s.log.Error("looking up user", "user_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the user")
		return nil, false
	}
	return user, true
}

// validCredentials checks an email + password pair for account creation,
// writing the error response itself.
func (s *Server) validCredentials(w http.ResponseWriter, rawEmail, password string) (email, hash string, ok bool) {
	email, ok = validEmail(rawEmail)
	if !ok {
		writeError(w, http.StatusBadRequest, "please enter a valid email address")
		return "", "", false
	}
	hash, ok = s.hashValidPassword(w, password)
	if !ok {
		return "", "", false
	}
	return email, hash, true
}

func (s *Server) hashValidPassword(w http.ResponseWriter, password string) (string, bool) {
	if err := auth.ValidatePassword(password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.log.Error("hashing password", "err", err)
		writeError(w, http.StatusInternalServerError, "could not process the password, please try again")
		return "", false
	}
	return hash, true
}
