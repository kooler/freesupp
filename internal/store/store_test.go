package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openTestStore opens a store on a temp file with a deterministic clock.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	clock := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	return s
}

func TestOpenAppliesMigrations(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	for _, table := range []string{"conversations", "messages", "schema_migrations"} {
		var name string
		err := s.DB().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}

	wantIndexes := []string{
		"idx_conversations_status_last_message_at",
		"idx_conversations_visitor_email_status",
		"idx_messages_conversation_id",
	}
	for _, idx := range wantIndexes {
		var name string
		err := s.DB().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='index' AND name = ?`, idx).Scan(&name)
		if err != nil {
			t.Fatalf("index %s missing: %v", idx, err)
		}
	}

	var journal string
	if err := s.DB().QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(journal, "wal") {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := first.CreateConversation(ctx, NewConversation{VisitorEmail: "a@example.com"}); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	var count int
	if err := second.DB().QueryRowContext(ctx, `SELECT count(*) FROM conversations`).Scan(&count); err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	if count != 1 {
		t.Errorf("conversations = %d, want 1 (data lost on re-migration?)", count)
	}

	names, err := migrationNames()
	if err != nil {
		t.Fatalf("migrationNames: %v", err)
	}
	var applied int
	if err := second.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if applied != len(names) {
		t.Errorf("schema_migrations = %d, want %d (each migration applied exactly once)", applied, len(names))
	}
}

func TestOpenBadPath(t *testing.T) {
	_, err := Open(context.Background(), filepath.Join(t.TempDir(), "missing-dir", "test.db"))
	if err == nil {
		t.Fatal("expected error opening a database in a missing directory")
	}
}

func TestCreateAndGetConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	created, err := s.CreateConversation(ctx, NewConversation{
		VisitorEmail: "  Visitor@Example.COM ",
		VisitorName:  " Vic ",
		Unread:       true,
	})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if created.ID == 0 {
		t.Error("ID = 0, want assigned id")
	}
	if created.VisitorEmail != "visitor@example.com" {
		t.Errorf("VisitorEmail = %q, want normalized", created.VisitorEmail)
	}
	if created.VisitorName != "Vic" {
		t.Errorf("VisitorName = %q, want trimmed", created.VisitorName)
	}
	if len(created.Token) != 64 {
		t.Errorf("Token = %q, want 64 hex chars", created.Token)
	}
	if created.Status != StatusOpen {
		t.Errorf("Status = %q, want %q", created.Status, StatusOpen)
	}
	if !created.Unread {
		t.Error("Unread = false, want true")
	}

	byID, err := s.GetConversation(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	assertConversationEqual(t, byID, created)

	byToken, err := s.GetConversationByToken(ctx, created.Token)
	if err != nil {
		t.Fatalf("GetConversationByToken: %v", err)
	}
	assertConversationEqual(t, byToken, created)
}

func TestGetConversationNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.GetConversation(ctx, 404); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetConversation err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetConversationByToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetConversationByToken err = %v, want ErrNotFound", err)
	}
}

func TestCreateConversationErrors(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.CreateConversation(ctx, NewConversation{VisitorEmail: "   "}); err == nil {
		t.Error("expected error for empty visitor email")
	}

	first, err := s.CreateConversation(ctx, NewConversation{VisitorEmail: "a@example.com", Token: "dup"})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := s.CreateConversation(ctx, NewConversation{VisitorEmail: "b@example.com", Token: first.Token}); err == nil {
		t.Error("expected UNIQUE violation for duplicate token")
	}
}

func TestNewTokenIsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if len(tok) != 64 {
			t.Fatalf("token %q length = %d, want 64", tok, len(tok))
		}
		if seen[tok] {
			t.Fatalf("duplicate token %q", tok)
		}
		seen[tok] = true
	}
}

func TestInsertAndListMessages(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, err := s.CreateConversation(ctx, NewConversation{VisitorEmail: "v@example.com"})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	visitorMsg, err := s.InsertMessage(ctx, NewMessage{
		ConversationID: conv.ID,
		Sender:         SenderVisitor,
		Body:           "  hello\nworld  ",
	})
	if err != nil {
		t.Fatalf("InsertMessage visitor: %v", err)
	}
	if visitorMsg.Body != "hello\nworld" {
		t.Errorf("Body = %q, want trimmed with line break preserved", visitorMsg.Body)
	}
	if visitorMsg.OperatorEmail != "" {
		t.Errorf("OperatorEmail = %q, want empty for visitor", visitorMsg.OperatorEmail)
	}

	opMsg, err := s.InsertMessage(ctx, NewMessage{
		ConversationID: conv.ID,
		Sender:         SenderOperator,
		OperatorEmail:  "Op@Example.com",
		Body:           "hi there",
	})
	if err != nil {
		t.Fatalf("InsertMessage operator: %v", err)
	}
	if opMsg.OperatorEmail != "op@example.com" {
		t.Errorf("OperatorEmail = %q, want normalized", opMsg.OperatorEmail)
	}

	msgs, err := s.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].ID != visitorMsg.ID || msgs[1].ID != opMsg.ID {
		t.Errorf("messages out of order: %d, %d", msgs[0].ID, msgs[1].ID)
	}
	if !msgs[0].CreatedAt.Equal(visitorMsg.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v (round-trip mismatch)", msgs[0].CreatedAt, visitorMsg.CreatedAt)
	}

	got, err := s.GetMessage(ctx, opMsg.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if got.Body != opMsg.Body || got.Sender != SenderOperator {
		t.Errorf("GetMessage = %+v, want %+v", got, opMsg)
	}
}

func TestListMessagesEmptyConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, err := s.CreateConversation(ctx, NewConversation{VisitorEmail: "v@example.com"})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	msgs, err := s.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0", len(msgs))
	}
}

func TestListMessagesUnknownConversation(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.ListMessages(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetMessageNotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetMessage(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestInsertMessageErrors(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, err := s.CreateConversation(ctx, NewConversation{VisitorEmail: "v@example.com"})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	tests := []struct {
		name string
		msg  NewMessage
	}{
		{"unknown sender", NewMessage{ConversationID: conv.ID, Sender: "robot", Body: "x"}},
		{"empty body", NewMessage{ConversationID: conv.ID, Sender: SenderVisitor, Body: "   "}},
		{"operator without email", NewMessage{ConversationID: conv.ID, Sender: SenderOperator, Body: "x"}},
		{"unknown conversation", NewMessage{ConversationID: 999, Sender: SenderVisitor, Body: "x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.InsertMessage(ctx, tc.msg); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func assertConversationEqual(t *testing.T, got, want *Conversation) {
	t.Helper()
	if got.ID != want.ID || got.VisitorEmail != want.VisitorEmail || got.VisitorName != want.VisitorName ||
		got.Token != want.Token || got.Status != want.Status || got.Unread != want.Unread {
		t.Errorf("conversation = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.LastMessageAt.Equal(want.LastMessageAt) {
		t.Errorf("LastMessageAt = %v, want %v", got.LastMessageAt, want.LastMessageAt)
	}
}
