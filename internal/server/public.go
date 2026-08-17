package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kooler/freesupp/internal/captcha"
	"github.com/kooler/freesupp/internal/store"
)

// Input limits for visitor submissions.
const (
	maxMessageLen = 10000
	maxFieldLen   = 200
	maxBodyBytes  = 64 << 10
)

// operatorLabel is what visitors see instead of an operator's identity.
const operatorLabel = "Support"

type newMessageRequest struct {
	Email          string `json:"email"`
	Name           string `json:"name"`
	Message        string `json:"message"`
	TurnstileToken string `json:"turnstile_token"`
}

// newMessageResponse carries the thread token only where the caller already
// proved possession of one; POST /api/messages always leaves it empty.
type newMessageResponse struct {
	Token string `json:"token"`
}

type threadMessage struct {
	ID        int64     `json:"id"`
	Sender    string    `json:"sender"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type threadResponse struct {
	Token        string          `json:"token"`
	Status       string          `json:"status"`
	VisitorEmail string          `json:"visitor_email"`
	VisitorName  string          `json:"visitor_name"`
	CreatedAt    time.Time       `json:"created_at"`
	Messages     []threadMessage `json:"messages"`
}

// handleNewMessage accepts a submission from the widget form.
func (s *Server) handleNewMessage(w http.ResponseWriter, r *http.Request) {
	var req newMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email, ok := validEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "please enter a valid email address")
		return
	}
	name := strings.TrimSpace(req.Name)
	if len([]rune(name)) > maxFieldLen {
		writeError(w, http.StatusBadRequest, "name is too long")
		return
	}
	body, ok := validMessage(w, req.Message)
	if !ok {
		return
	}

	if !s.verifyCaptcha(w, r, req.TurnstileToken) {
		return
	}

	conv, msg, created, err := s.store.AddVisitorMessage(r.Context(), email, name, body)
	if err != nil {
		s.log.Error("storing visitor message", "err", err)
		writeError(w, http.StatusInternalServerError, "could not save your message, please try again")
		return
	}
	s.notifier.NotifyOperators(*conv, *msg, s.operatorEmails(r.Context()))

	// This endpoint is unauthenticated and nothing proves the submitter owns the
	// address, so the magic-link token never travels back in the response — it
	// is emailed to the address instead, which is the only check that the person
	// holding the link owns the mailbox. Handing it to the submitter would let
	// anyone open a thread for someone else's address, wait for the owner's own
	// message to join that still-open thread, and read everything in it.
	// Appending to an existing thread sends nothing: that visitor already has
	// their link, and operator replies carry it again.
	if created {
		s.notifier.NotifyVisitorReceipt(*conv)
	}
	writeJSON(w, http.StatusCreated, newMessageResponse{})
}

// handleGetThread returns the conversation behind a magic link.
func (s *Server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	conv, ok := s.conversationByToken(w, r)
	if !ok {
		return
	}

	msgs, err := s.store.ListMessages(r.Context(), conv.ID)
	if err != nil {
		s.log.Error("listing thread messages", "conversation_id", conv.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not load this conversation")
		return
	}

	writeJSON(w, http.StatusOK, buildThreadResponse(conv, msgs))
}

// handleThreadMessage appends a visitor follow-up from the magic-link page.
// An open thread receives the message directly, so the visitor always writes
// into the conversation their link points at. Replying to an archived thread
// falls back to the store threading rules, which start a new conversation with
// a new token rather than reopening the archived one.
func (s *Server) handleThreadMessage(w http.ResponseWriter, r *http.Request) {
	conv, ok := s.conversationByToken(w, r)
	if !ok {
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	body, ok := validMessage(w, req.Message)
	if !ok {
		return
	}

	var (
		target *store.Conversation
		msg    *store.Message
		err    error
	)
	if conv.Status == store.StatusOpen {
		target, msg, err = s.store.AppendVisitorMessage(r.Context(), conv.ID, body)
	} else {
		// Holding a valid token already proves possession of a credential for
		// this visitor, so returning the resulting token discloses nothing new.
		target, msg, _, err = s.store.AddVisitorMessage(r.Context(), conv.VisitorEmail, conv.VisitorName, body)
	}
	if err != nil {
		s.log.Error("storing visitor follow-up", "conversation_id", conv.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not save your message, please try again")
		return
	}
	s.notifier.NotifyOperators(*target, *msg, s.operatorEmails(r.Context()))

	writeJSON(w, http.StatusCreated, newMessageResponse{Token: target.Token})
}

func (s *Server) conversationByToken(w http.ResponseWriter, r *http.Request) (*store.Conversation, bool) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	conv, err := s.store.GetConversationByToken(r.Context(), token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "this conversation link is not valid")
		return nil, false
	case err != nil:
		s.log.Error("looking up conversation by token", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load this conversation")
		return nil, false
	}
	return conv, true
}

// verifyCaptcha reports whether the request may proceed; it writes the error
// response itself when it may not.
func (s *Server) verifyCaptcha(w http.ResponseWriter, r *http.Request, token string) bool {
	err := s.verifier.Verify(r.Context(), token, s.clientIP(r))
	switch {
	case err == nil:
		return true
	case errors.Is(err, captcha.ErrFailed):
		writeError(w, http.StatusBadRequest, "captcha verification failed, please try again")
	default:
		s.log.Error("captcha verification unavailable", "err", err)
		writeError(w, http.StatusServiceUnavailable, "could not verify the captcha, please try again shortly")
	}
	return false
}

func buildThreadResponse(conv *store.Conversation, msgs []store.Message) threadResponse {
	out := threadResponse{
		Token:        conv.Token,
		Status:       conv.Status,
		VisitorEmail: conv.VisitorEmail,
		VisitorName:  conv.VisitorName,
		CreatedAt:    conv.CreatedAt,
		Messages:     make([]threadMessage, 0, len(msgs)),
	}
	for _, m := range msgs {
		// Operator identity stays internal: visitors only ever see "Support".
		author := operatorLabel
		if m.Sender == store.SenderVisitor {
			author = conv.VisitorName
		}
		out.Messages = append(out.Messages, threadMessage{
			ID:        m.ID,
			Sender:    m.Sender,
			Author:    author,
			Body:      m.Body,
			CreatedAt: m.CreatedAt,
		})
	}
	return out
}

// validEmail normalizes and checks a visitor address.
func validEmail(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxFieldLen {
		return "", false
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Name != "" {
		return "", false
	}
	return strings.ToLower(addr.Address), true
}

// validMessage checks a message body, writing the error response on failure.
func validMessage(w http.ResponseWriter, raw string) (string, bool) {
	body := strings.TrimSpace(raw)
	if body == "" {
		writeError(w, http.StatusBadRequest, "message cannot be empty")
		return "", false
	}
	if len([]rune(body)) > maxMessageLen {
		writeError(w, http.StatusBadRequest, "message is too long")
		return "", false
	}
	return body, true
}

// decodeJSON reads a size-capped JSON body, writing the error response itself.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "message is too long")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	// Reject trailing content so a second JSON object cannot sneak through.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Message history, visitor addresses and magic-link tokens must not sit in a
	// shared cache or the back/forward cache after sign-out.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
