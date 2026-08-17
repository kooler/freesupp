package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// snippetLen caps the last-message preview shown in the inbox list.
const snippetLen = 140

// ConversationSummary is a conversation plus a preview of its last message.
type ConversationSummary struct {
	Conversation
	Snippet    string // last message body, whitespace collapsed and truncated
	LastSender string // sender of the last message, empty when there are none
}

// AddVisitorMessage appends a visitor message to that visitor's open
// conversation, creating one when none is open. It marks the conversation
// unread and bumps last_message_at. Both writes happen in one transaction.
//
// created reports whether a new conversation was opened. Callers must not
// disclose the token of a conversation they did not create: the submitter's
// ownership of the address is unverified, so an existing thread's token would
// be handed to whoever guessed the email.
func (s *Store) AddVisitorMessage(ctx context.Context, email, name, body string) (conv *Conversation, msg *Message, created bool, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, nil, false, errors.New("store: visitor email is required")
	}
	name = strings.TrimSpace(name)

	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		conv, err = s.openConversationForVisitor(ctx, tx, email)
		switch {
		case errors.Is(err, ErrNotFound):
			conv, err = s.createConversation(ctx, tx, NewConversation{
				VisitorEmail: email,
				VisitorName:  name,
				Unread:       true,
			})
			if err != nil {
				return err
			}
			created = true
		case err != nil:
			return err
		}

		msg, err = s.appendVisitorMessage(ctx, tx, conv, name, body)
		return err
	})
	if err != nil {
		return nil, nil, false, err
	}
	return conv, msg, created, nil
}

// AppendVisitorMessage adds a visitor message to one specific conversation,
// marking it unread and bumping last_message_at. Follow-ups from a magic link
// use this: the thread is identified by the token the visitor holds, not by
// looking up whichever open conversation is newest for their address.
func (s *Store) AppendVisitorMessage(ctx context.Context, convID int64, body string) (*Conversation, *Message, error) {
	var (
		conv *Conversation
		msg  *Message
	)
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		conv, err = s.getConversation(ctx, tx,
			`SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, convID)
		if err != nil {
			return err
		}
		msg, err = s.appendVisitorMessage(ctx, tx, conv, "", body)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return conv, msg, nil
}

// appendVisitorMessage inserts the message and updates conv in place. name
// fills in a visitor name we did not have before; it never overwrites one.
func (s *Store) appendVisitorMessage(ctx context.Context, tx *sql.Tx, conv *Conversation, name, body string) (*Message, error) {
	msg, err := s.insertMessage(ctx, tx, NewMessage{
		ConversationID: conv.ID,
		Sender:         SenderVisitor,
		Body:           body,
	})
	if err != nil {
		return nil, err
	}

	if conv.VisitorName == "" && name != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE conversations SET visitor_name = ? WHERE id = ?`, name, conv.ID); err != nil {
			return nil, fmt.Errorf("store: update visitor name: %w", err)
		}
		conv.VisitorName = name
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE conversations SET unread = 1, last_message_at = ? WHERE id = ?`,
		formatTime(msg.CreatedAt), conv.ID,
	); err != nil {
		return nil, fmt.Errorf("store: bump conversation: %w", err)
	}
	conv.Unread = true
	conv.LastMessageAt = msg.CreatedAt
	return msg, nil
}

// AddOperatorReply appends an operator message and bumps last_message_at.
// It leaves the unread flag untouched.
func (s *Store) AddOperatorReply(ctx context.Context, convID int64, operatorEmail, body string) (*Message, error) {
	var msg *Message
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := s.getConversation(ctx, tx,
			`SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, convID); err != nil {
			return err
		}

		var err error
		msg, err = s.insertMessage(ctx, tx, NewMessage{
			ConversationID: convID,
			Sender:         SenderOperator,
			OperatorEmail:  operatorEmail,
			Body:           body,
		})
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE conversations SET last_message_at = ? WHERE id = ?`,
			formatTime(msg.CreatedAt), convID,
		); err != nil {
			return fmt.Errorf("store: bump conversation: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// MarkRead clears the unread flag of a conversation, but only while its last
// activity still is asOf — the caller read the history at that point, and a
// visitor message that landed since is one nobody has seen. Clearing it anyway
// would drop the support request out of the unread list silently. It reports
// whether the flag was cleared; an unknown conversation simply clears nothing.
func (s *Store) MarkRead(ctx context.Context, convID int64, asOf time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET unread = 0 WHERE id = ? AND last_message_at = ?`,
		convID, formatTime(asOf))
	if err != nil {
		return false, fmt.Errorf("store: mark conversation read: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: mark conversation read: %w", err)
	}
	return n > 0, nil
}

// Archive moves a conversation out of the open list.
func (s *Store) Archive(ctx context.Context, convID int64) error {
	return s.updateConversation(ctx, `UPDATE conversations SET status = ? WHERE id = ?`, StatusArchived, convID)
}

// Unarchive moves a conversation back to the open list.
func (s *Store) Unarchive(ctx context.Context, convID int64) error {
	return s.updateConversation(ctx, `UPDATE conversations SET status = ? WHERE id = ?`, StatusOpen, convID)
}

// ListConversations returns conversations newest activity first. An empty
// status lists every conversation.
func (s *Store) ListConversations(ctx context.Context, status string) ([]ConversationSummary, error) {
	if status != "" && status != StatusOpen && status != StatusArchived {
		return nil, fmt.Errorf("store: invalid status %q", status)
	}

	// The correlated subquery picks the newest message per conversation.
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+prefixColumns("c", conversationColumns)+`, m.sender, m.body
		 FROM conversations c
		 LEFT JOIN messages m ON m.id = (
		     SELECT id FROM messages WHERE conversation_id = c.id ORDER BY id DESC LIMIT 1
		 )
		 WHERE ? = '' OR c.status = ?
		 ORDER BY c.last_message_at DESC, c.id DESC`, status, status)
	if err != nil {
		return nil, fmt.Errorf("store: list conversations: %w", err)
	}
	defer rows.Close()

	out := []ConversationSummary{}
	for rows.Next() {
		var (
			sum    ConversationSummary
			sender sql.NullString
			body   sql.NullString
		)
		c, err := scanConversationWith(rows, &sender, &body)
		if err != nil {
			return nil, fmt.Errorf("store: scan conversation: %w", err)
		}
		sum.Conversation = *c
		sum.LastSender = sender.String
		sum.Snippet = snippet(body.String)
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list conversations: %w", err)
	}
	return out, nil
}

// openConversationForVisitor returns the visitor's most recently active open
// conversation, or ErrNotFound.
func (s *Store) openConversationForVisitor(ctx context.Context, q querier, email string) (*Conversation, error) {
	c, err := scanConversation(q.QueryRowContext(ctx,
		`SELECT `+conversationColumns+` FROM conversations
		 WHERE visitor_email = ? AND status = ?
		 ORDER BY last_message_at DESC, id DESC LIMIT 1`, email, StatusOpen))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: find open conversation: %w", err)
	}
	return c, nil
}

func (s *Store) updateConversation(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: update conversation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update conversation: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
}

// prefixColumns qualifies a comma-separated column list with a table alias.
func prefixColumns(alias, columns string) string {
	parts := strings.Split(columns, ", ")
	for i, p := range parts {
		parts[i] = alias + "." + p
	}
	return strings.Join(parts, ", ")
}

// snippet collapses whitespace and truncates for list previews.
func snippet(body string) string {
	s := strings.Join(strings.Fields(body), " ")
	r := []rune(s)
	if len(r) <= snippetLen {
		return s
	}
	return strings.TrimRight(string(r[:snippetLen]), " ") + "…"
}
