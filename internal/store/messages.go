package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// NewMessage describes a message to append to a conversation.
type NewMessage struct {
	ConversationID int64
	Sender         string
	OperatorEmail  string // only for operator messages
	Body           string
}

const messageColumns = `id, conversation_id, sender, operator_email, body, created_at`

// InsertMessage appends a message to an existing conversation.
func (s *Store) InsertMessage(ctx context.Context, nm NewMessage) (*Message, error) {
	return s.insertMessage(ctx, s.db, nm)
}

func (s *Store) insertMessage(ctx context.Context, q querier, nm NewMessage) (*Message, error) {
	if nm.Sender != SenderVisitor && nm.Sender != SenderOperator {
		return nil, fmt.Errorf("store: invalid sender %q", nm.Sender)
	}
	body := strings.TrimSpace(nm.Body)
	if body == "" {
		return nil, errors.New("store: message body is required")
	}

	var operator any
	if nm.Sender == SenderOperator {
		email := strings.ToLower(strings.TrimSpace(nm.OperatorEmail))
		if email == "" {
			return nil, errors.New("store: operator email is required for operator messages")
		}
		operator = email
		nm.OperatorEmail = email
	} else {
		nm.OperatorEmail = ""
	}

	now := s.now().UTC()
	res, err := q.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, sender, operator_email, body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		nm.ConversationID, nm.Sender, operator, body, formatTime(now),
	)
	if err != nil {
		return nil, fmt.Errorf("store: insert message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: message id: %w", err)
	}

	return &Message{
		ID:             id,
		ConversationID: nm.ConversationID,
		Sender:         nm.Sender,
		OperatorEmail:  nm.OperatorEmail,
		Body:           body,
		CreatedAt:      now,
	}, nil
}

// ListMessages returns a conversation's messages oldest first.
// It reports ErrNotFound when the conversation does not exist.
func (s *Store) ListMessages(ctx context.Context, convID int64) ([]Message, error) {
	if _, err := s.GetConversation(ctx, convID); err != nil {
		return nil, err
	}
	return s.listMessages(ctx, s.db, convID)
}

func (s *Store) listMessages(ctx context.Context, q querier, convID int64) ([]Message, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE conversation_id = ? ORDER BY id`, convID)
	if err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan message: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	return out, nil
}

// GetMessage returns a single message by id.
func (s *Store) GetMessage(ctx context.Context, id int64) (*Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get message: %w", err)
	}
	return m, nil
}

func scanMessage(row rowScanner) (*Message, error) {
	var (
		m         Message
		operator  sql.NullString
		createdAt string
	)
	if err := row.Scan(&m.ID, &m.ConversationID, &m.Sender, &operator, &m.Body, &createdAt); err != nil {
		return nil, err
	}
	m.OperatorEmail = operator.String

	var err error
	if m.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("store: parse created_at: %w", err)
	}
	return &m, nil
}
