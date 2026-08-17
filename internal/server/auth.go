package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kooler/freesupp/internal/auth"
	"github.com/kooler/freesupp/internal/store"
)

// sessionCookie carries the signed operator session.
const sessionCookie = "fs_session"

// ctxKey namespaces values this package puts on the request context.
type ctxKey int

const operatorKey ctxKey = iota

// userIfExists distinguishes "no such user" (nil, nil) from a store failure.
func (s *Server) userIfExists(ctx context.Context, email string) (*store.User, error) {
	user, err := s.store.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return user, err
}

// handleLogout clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the signed-in operator.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, meResponse(userFrom(r.Context())))
}

func meResponse(u *store.User) any {
	return struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"is_admin"`
	}{Email: u.Email, IsAdmin: u.IsAdmin}
}

// requireOperator gates the operator API with the session cookie.
func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.sessionUser(r)
		if err != nil {
			// A store failure is not "signed out": a 401 would bounce the
			// operator to the login screen for an outage that is not theirs.
			s.log.Error("looking up session user", "err", err)
			writeError(w, http.StatusInternalServerError, "could not verify your session, please try again")
			return
		}
		if user == nil {
			writeError(w, http.StatusUnauthorized, "sign in to continue")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), operatorKey, user)))
	})
}

// requireAdmin gates user management; it must run inside requireOperator.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r.Context()).IsAdmin {
			writeError(w, http.StatusForbidden, "only admins can manage users")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sessionUser returns the operator behind the request's cookie, nil when the
// request carries no valid session, or an error when the store fails. The
// users table is re-checked on every request so removing a user takes effect
// at once.
func (s *Server) sessionUser(r *http.Request) (*store.User, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, nil
	}
	sess, err := auth.ParseSession(s.cfg.SessionSecret, cookie.Value, time.Now())
	if err != nil {
		return nil, nil
	}
	return s.userIfExists(r.Context(), sess.Email)
}

func (s *Server) setSession(w http.ResponseWriter, email string) {
	value := auth.SignSession(s.cfg.SessionSecret, auth.Session{
		Email:     email,
		ExpiresAt: time.Now().Add(auth.SessionTTL),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

// secureCookies is off for plain-http deployments (local dev), where a Secure
// cookie would never be sent back.
func (s *Server) secureCookies() bool {
	return strings.HasPrefix(s.cfg.BaseURL, "https://")
}

// userFrom returns the operator attached by requireOperator.
func userFrom(ctx context.Context) *store.User {
	user, _ := ctx.Value(operatorKey).(*store.User)
	return user
}

// operatorFrom returns the signed-in operator's email.
func operatorFrom(ctx context.Context) string {
	if u := userFrom(ctx); u != nil {
		return u.Email
	}
	return ""
}

// operatorEmails lists notification recipients; a failure only costs the
// notification, never the request that triggered it.
func (s *Server) operatorEmails(ctx context.Context) []string {
	emails, err := s.store.UserEmails(ctx)
	if err != nil {
		s.log.Error("listing operator emails for notification", "err", err)
		return nil
	}
	return emails
}
