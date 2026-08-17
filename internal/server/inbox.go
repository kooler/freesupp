package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kooler/freesupp/internal/store"
)

type conversationSummary struct {
	ID            int64     `json:"id"`
	VisitorEmail  string    `json:"visitor_email"`
	VisitorName   string    `json:"visitor_name"`
	Status        string    `json:"status"`
	Unread        bool      `json:"unread"`
	Snippet       string    `json:"snippet"`
	LastSender    string    `json:"last_sender"`
	CreatedAt     time.Time `json:"created_at"`
	LastMessageAt time.Time `json:"last_message_at"`
}

type conversationListResponse struct {
	Conversations []conversationSummary `json:"conversations"`
}

type inboxMessage struct {
	ID            int64     `json:"id"`
	Sender        string    `json:"sender"`
	OperatorEmail string    `json:"operator_email,omitempty"`
	Body          string    `json:"body"`
	CreatedAt     time.Time `json:"created_at"`
}

type conversationDetail struct {
	ID            int64          `json:"id"`
	VisitorEmail  string         `json:"visitor_email"`
	VisitorName   string         `json:"visitor_name"`
	Token         string         `json:"token"`
	Status        string         `json:"status"`
	Unread        bool           `json:"unread"`
	CreatedAt     time.Time      `json:"created_at"`
	LastMessageAt time.Time      `json:"last_message_at"`
	Messages      []inboxMessage `json:"messages"`
}

// handleListConversations lists conversations for the inbox, newest activity
// first. Without an explicit status it lists the open ones.
func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "":
		status = store.StatusOpen
	case store.StatusOpen, store.StatusArchived:
	case "all":
		status = "" // store treats an empty status as "no filter"
	default:
		writeError(w, http.StatusBadRequest, "unknown status filter")
		return
	}

	convs, err := s.store.ListConversations(r.Context(), status)
	if err != nil {
		s.log.Error("listing conversations", "status", status, "err", err)
		writeError(w, http.StatusInternalServerError, "could not load conversations")
		return
	}

	out := conversationListResponse{Conversations: make([]conversationSummary, 0, len(convs))}
	for _, c := range convs {
		out.Conversations = append(out.Conversations, conversationSummary{
			ID:            c.ID,
			VisitorEmail:  c.VisitorEmail,
			VisitorName:   c.VisitorName,
			Status:        c.Status,
			Unread:        c.Unread,
			Snippet:       c.Snippet,
			LastSender:    c.LastSender,
			CreatedAt:     c.CreatedAt,
			LastMessageAt: c.LastMessageAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetConversation returns the full history and marks it read.
func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	conv, ok := s.conversationByID(w, r)
	if !ok {
		return
	}

	msgs, err := s.store.ListMessages(r.Context(), conv.ID)
	if err != nil {
		s.log.Error("listing conversation messages", "conversation_id", conv.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not load this conversation")
		return
	}

	// Opening a conversation is what clears the unread flag — but only for the
	// history this response carries. A visitor message that arrived while we
	// were reading stays unread, so the operator still sees it in the list.
	if conv.Unread {
		cleared, err := s.store.MarkRead(r.Context(), conv.ID, conv.LastMessageAt)
		if err != nil {
			s.log.Error("marking conversation read", "conversation_id", conv.ID, "err", err)
		} else {
			conv.Unread = !cleared
		}
	}

	writeJSON(w, http.StatusOK, buildConversationDetail(conv, msgs))
}

// handleReply appends an operator reply and emails the visitor.
func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	conv, ok := s.conversationByID(w, r)
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

	operator := operatorFrom(r.Context())
	msg, err := s.store.AddOperatorReply(r.Context(), conv.ID, operator, body)
	if err != nil {
		s.log.Error("storing operator reply", "conversation_id", conv.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not send your reply")
		return
	}
	conv.LastMessageAt = msg.CreatedAt
	s.notifier.NotifyVisitor(*conv, *msg)

	writeJSON(w, http.StatusCreated, inboxMessage{
		ID:            msg.ID,
		Sender:        msg.Sender,
		OperatorEmail: msg.OperatorEmail,
		Body:          msg.Body,
		CreatedAt:     msg.CreatedAt,
	})
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	s.setStatus(w, r, store.StatusArchived)
}

func (s *Server) handleUnarchive(w http.ResponseWriter, r *http.Request) {
	s.setStatus(w, r, store.StatusOpen)
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request, status string) {
	conv, ok := s.conversationByID(w, r)
	if !ok {
		return
	}

	var err error
	if status == store.StatusArchived {
		err = s.store.Archive(r.Context(), conv.ID)
	} else {
		err = s.store.Unarchive(r.Context(), conv.ID)
	}
	if err != nil {
		s.log.Error("updating conversation status", "conversation_id", conv.ID, "status", status, "err", err)
		writeError(w, http.StatusInternalServerError, "could not update this conversation")
		return
	}
	conv.Status = status

	writeJSON(w, http.StatusOK, buildConversationDetail(conv, nil))
}

func (s *Server) conversationByID(w http.ResponseWriter, r *http.Request) (*store.Conversation, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return nil, false
	}

	conv, err := s.store.GetConversation(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "conversation not found")
		return nil, false
	case err != nil:
		s.log.Error("looking up conversation", "conversation_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not load this conversation")
		return nil, false
	}
	return conv, true
}

func buildConversationDetail(conv *store.Conversation, msgs []store.Message) conversationDetail {
	out := conversationDetail{
		ID:            conv.ID,
		VisitorEmail:  conv.VisitorEmail,
		VisitorName:   conv.VisitorName,
		Token:         conv.Token,
		Status:        conv.Status,
		Unread:        conv.Unread,
		CreatedAt:     conv.CreatedAt,
		LastMessageAt: conv.LastMessageAt,
		Messages:      make([]inboxMessage, 0, len(msgs)),
	}
	for _, m := range msgs {
		out.Messages = append(out.Messages, inboxMessage{
			ID:            m.ID,
			Sender:        m.Sender,
			OperatorEmail: m.OperatorEmail,
			Body:          m.Body,
			CreatedAt:     m.CreatedAt,
		})
	}
	return out
}
