package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// querier is satisfied by both *sql.DB and *sql.Tx so query helpers can run
// inside or outside a transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NewConversation describes a conversation to create. Token is generated when empty.
type NewConversation struct {
	VisitorEmail string
	VisitorName  string
	Token        string
	Unread       bool
}

const conversationColumns = `id, visitor_email, visitor_name, token, status, unread, created_at, last_message_at`

// NewToken returns a 32-byte random magic-link token, hex encoded.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("store: generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// CreateConversation inserts an open conversation and returns it.
func (s *Store) CreateConversation(ctx context.Context, nc NewConversation) (*Conversation, error) {
	return s.createConversation(ctx, s.db, nc)
}

func (s *Store) createConversation(ctx context.Context, q querier, nc NewConversation) (*Conversation, error) {
	email := strings.ToLower(strings.TrimSpace(nc.VisitorEmail))
	if email == "" {
		return nil, errors.New("store: visitor email is required")
	}
	token := nc.Token
	if token == "" {
		var err error
		if token, err = NewToken(); err != nil {
			return nil, err
		}
	}

	now := s.now().UTC()
	ts := formatTime(now)
	res, err := q.ExecContext(ctx,
		`INSERT INTO conversations (visitor_email, visitor_name, token, status, unread, created_at, last_message_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		email, strings.TrimSpace(nc.VisitorName), token, StatusOpen, boolToInt(nc.Unread), ts, ts,
	)
	if err != nil {
		return nil, fmt.Errorf("store: insert conversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: conversation id: %w", err)
	}

	return &Conversation{
		ID:            id,
		VisitorEmail:  email,
		VisitorName:   strings.TrimSpace(nc.VisitorName),
		Token:         token,
		Status:        StatusOpen,
		Unread:        nc.Unread,
		CreatedAt:     now,
		LastMessageAt: now,
	}, nil
}

// GetConversation looks a conversation up by id.
func (s *Store) GetConversation(ctx context.Context, id int64) (*Conversation, error) {
	return s.getConversation(ctx, s.db, `SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, id)
}

// GetConversationByToken looks a conversation up by its magic-link token.
func (s *Store) GetConversationByToken(ctx context.Context, token string) (*Conversation, error) {
	return s.getConversation(ctx, s.db, `SELECT `+conversationColumns+` FROM conversations WHERE token = ?`, token)
}

func (s *Store) getConversation(ctx context.Context, q querier, query string, arg any) (*Conversation, error) {
	c, err := scanConversation(q.QueryRowContext(ctx, query, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get conversation: %w", err)
	}
	return c, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanConversation(row rowScanner) (*Conversation, error) {
	return scanConversationWith(row)
}

// scanConversationWith scans the conversation columns plus any extra
// destinations appended to the select list.
func scanConversationWith(row rowScanner, extra ...any) (*Conversation, error) {
	var (
		c            Conversation
		unread       int
		createdAt    string
		lastMessage  string
		visitorName  sql.NullString
		statusString string
	)
	dest := append([]any{
		&c.ID, &c.VisitorEmail, &visitorName, &c.Token, &statusString, &unread, &createdAt, &lastMessage,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	c.VisitorName = visitorName.String
	c.Status = statusString
	c.Unread = unread != 0

	var err error
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("store: parse created_at: %w", err)
	}
	if c.LastMessageAt, err = parseTime(lastMessage); err != nil {
		return nil, fmt.Errorf("store: parse last_message_at: %w", err)
	}
	return &c, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
