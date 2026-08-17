// Package server wires HTTP routing for FreeSupp.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kooler/freesupp/internal/captcha"
	"github.com/kooler/freesupp/internal/config"
	"github.com/kooler/freesupp/internal/mail"
	"github.com/kooler/freesupp/internal/store"
)

// Deps are the collaborators handlers need.
type Deps struct {
	Store    *store.Store
	Notifier *mail.Notifier
	Verifier captcha.Verifier
}

// Server holds the dependencies shared by all handlers.
type Server struct {
	cfg      *config.Config
	log      *slog.Logger
	store    *store.Store
	notifier *mail.Notifier
	verifier captcha.Verifier
	limiter  *rateLimiter
	router   chi.Router
}

// New builds a server with its routes registered. Every field of deps is
// required: defaulting a missing Verifier here would silently disable the
// captcha instead of failing loudly.
func New(cfg *config.Config, log *slog.Logger, deps Deps) *Server {
	s := &Server{
		cfg:      cfg,
		log:      log,
		store:    deps.Store,
		notifier: deps.Notifier,
		verifier: deps.Verifier,
		limiter:  newRateLimiter(defaultRateBurst, defaultRateWindow),
		router:   chi.NewRouter(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.Use(middleware.Recoverer)
	s.router.Get("/ping", s.handlePing)
	s.router.Get("/widget.js", s.handleWidgetJS)

	s.router.Post("/auth/logout", s.handleLogout)

	s.router.Route("/api", func(r chi.Router) {
		r.Get("/config", s.handleConfig)
		r.Get("/auth/config", s.handleAuthConfig)
		r.Group(func(r chi.Router) {
			r.Use(s.rateLimit)
			r.Post("/messages", s.handleNewMessage)
			r.Post("/thread/{token}/messages", s.handleThreadMessage)
			// Credential guessing shares the public endpoints' IP budget.
			r.Post("/auth/setup", s.handleSetup)
			r.Post("/auth/login", s.handlePasswordLogin)
		})
		r.Get("/thread/{token}", s.handleGetThread)

		// Operator API.
		r.Group(func(r chi.Router) {
			r.Use(s.requireOperator)
			r.Get("/me", s.handleMe)
			r.Put("/me/password", s.handleChangeMyPassword)
			r.Get("/inbox/conversations", s.handleListConversations)
			r.Get("/inbox/conversations/{id}", s.handleGetConversation)
			r.Post("/inbox/conversations/{id}/reply", s.handleReply)
			r.Post("/inbox/conversations/{id}/archive", s.handleArchive)
			r.Post("/inbox/conversations/{id}/unarchive", s.handleUnarchive)

			// User management is admin-only.
			r.Group(func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Delete("/users/{id}", s.handleDeleteUser)
				r.Put("/users/{id}/admin", s.handleSetUserAdmin)
				r.Put("/users/{id}/password", s.handleResetUserPassword)
			})
		})
	})

	s.appRoutes()
}

// appRoutes serves the two embedded SPAs: their hashed assets under a base of
// their own, their index.html for every route the app owns.
func (s *Server) appRoutes() {
	s.router.Get("/visitor/*", visitorApp.serveFile)
	s.router.Get("/widget", visitorApp.serveIndex)
	s.router.Get("/widget/", visitorApp.serveIndex)
	s.router.Get("/t/{token}", visitorApp.serveIndex)

	s.router.Get("/inbox/*", inboxApp.serveFile)
	s.router.Get("/", inboxApp.serveIndex)
	s.router.Get("/conversations", inboxApp.serveIndex)
	s.router.Get("/conversations/{id}", inboxApp.serveIndex)
}

type configResponse struct {
	TurnstileSiteKey string `json:"turnstile_site_key"`
}

// handleConfig exposes the public settings the visitor app needs. The site key
// is public by design; nothing else belongs here.
func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	key := ""
	if s.cfg.CaptchaConfigured() {
		key = s.cfg.TurnstileSiteKey
	}
	writeJSON(w, http.StatusOK, configResponse{TurnstileSiteKey: key})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
