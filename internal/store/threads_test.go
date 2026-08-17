package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAddVisitorMessageCreatesConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, msg, _, err := s.AddVisitorMessage(ctx, " Visitor@Example.COM ", " Vic ", "  need help\nplease  ")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if conv.VisitorEmail != "visitor@example.com" {
		t.Errorf("VisitorEmail = %q, want normalized", conv.VisitorEmail)
	}
	if conv.VisitorName != "Vic" {
		t.Errorf("VisitorName = %q, want %q", conv.VisitorName, "Vic")
	}
	if len(conv.Token) != 64 {
		t.Errorf("Token = %q, want 64 hex chars", conv.Token)
	}
	if !conv.Unread {
		t.Error("Unread = false, want true")
	}
	if conv.Status != StatusOpen {
		t.Errorf("Status = %q, want %q", conv.Status, StatusOpen)
	}
	if msg.Sender != SenderVisitor || msg.Body != "need help\nplease" {
		t.Errorf("message = %+v, want trimmed visitor message", msg)
	}
	if !conv.LastMessageAt.Equal(msg.CreatedAt) {
		t.Errorf("LastMessageAt = %v, want %v", conv.LastMessageAt, msg.CreatedAt)
	}

	stored, err := s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	assertConversationEqual(t, stored, conv)
}

func TestAddVisitorMessageAppendsToOpenConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	first, _, created, err := s.AddVisitorMessage(ctx, "v@example.com", "Vic", "first")
	if err != nil {
		t.Fatalf("first AddVisitorMessage: %v", err)
	}
	if !created {
		t.Error("created = false, want true for the first message from an address")
	}
	markRead(t, s, first)

	second, msg, created, err := s.AddVisitorMessage(ctx, "V@Example.com", "", "second")
	if err != nil {
		t.Fatalf("second AddVisitorMessage: %v", err)
	}
	// created gates whether the handler may disclose the thread token.
	if created {
		t.Error("created = true, want false when appending to an open thread")
	}
	if second.ID != first.ID {
		t.Fatalf("conversation id = %d, want %d (should append to open thread)", second.ID, first.ID)
	}
	if !second.Unread {
		t.Error("Unread = false, want true after a new visitor message")
	}
	if !second.LastMessageAt.After(first.LastMessageAt) {
		t.Errorf("LastMessageAt = %v, want after %v", second.LastMessageAt, first.LastMessageAt)
	}

	msgs, err := s.ListMessages(ctx, first.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[1].ID != msg.ID {
		t.Fatalf("messages = %+v, want 2 with the new one last", msgs)
	}
}

func TestAddVisitorMessageStartsNewConversationAfterArchive(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	first, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "Vic", "first")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if err := s.Archive(ctx, first.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	second, _, created, err := s.AddVisitorMessage(ctx, "v@example.com", "Vic", "second")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if !created {
		t.Error("created = false, want true when the previous thread was archived")
	}
	if second.ID == first.ID {
		t.Fatal("expected a fresh conversation after archiving")
	}
	if second.Token == first.Token {
		t.Error("expected a fresh token for the new conversation")
	}
	if second.Status != StatusOpen || !second.Unread {
		t.Errorf("conversation = %+v, want open and unread", second)
	}

	archived, err := s.GetConversation(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if archived.Status != StatusArchived {
		t.Errorf("Status = %q, want %q", archived.Status, StatusArchived)
	}
	msgs, err := s.ListMessages(ctx, first.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("archived conversation has %d messages, want 1", len(msgs))
	}
}

func TestAddVisitorMessageFillsMissingName(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", "first"); err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	conv, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "Vic", "second")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if conv.VisitorName != "Vic" {
		t.Errorf("VisitorName = %q, want %q", conv.VisitorName, "Vic")
	}

	// An existing name is never overwritten.
	conv, _, _, err = s.AddVisitorMessage(ctx, "v@example.com", "Other", "third")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if conv.VisitorName != "Vic" {
		t.Errorf("VisitorName = %q, want %q kept", conv.VisitorName, "Vic")
	}
	stored, err := s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if stored.VisitorName != "Vic" {
		t.Errorf("stored VisitorName = %q, want %q", stored.VisitorName, "Vic")
	}
}

func TestAddVisitorMessageErrors(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	tests := []struct {
		name, email, body string
	}{
		{"empty email", "  ", "hello"},
		{"empty body", "v@example.com", "   "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := s.AddVisitorMessage(ctx, tc.email, "", tc.body); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}

	// A failed insert must not leave a conversation behind.
	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM conversations`).Scan(&count); err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	if count != 0 {
		t.Errorf("conversations = %d, want 0 (transaction should roll back)", count)
	}
}

func TestVisitorConversationTokensAreUnique(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		conv, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", "hello")
		if err != nil {
			t.Fatalf("AddVisitorMessage: %v", err)
		}
		if seen[conv.Token] {
			t.Fatalf("duplicate token %q", conv.Token)
		}
		seen[conv.Token] = true
		if err := s.Archive(ctx, conv.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}
	}
	if len(seen) != 20 {
		t.Errorf("distinct tokens = %d, want 20", len(seen))
	}
}

// "One open conversation per visitor" survives concurrent submissions — the
// read-then-insert in AddVisitorMessage relies on writes being serialized.
func TestAddVisitorMessageIsSafeUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, err := s.AddVisitorMessage(ctx, "race@example.com", "", fmt.Sprintf("message %d", i)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("AddVisitorMessage: %v", err)
	}

	convs, err := s.ListConversations(ctx, "")
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1 for a single visitor", len(convs))
	}
	msgs, err := s.ListMessages(ctx, convs[0].ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != writers {
		t.Errorf("got %d messages, want all %d", len(msgs), writers)
	}
}

func TestAppendVisitorMessageTargetsOneConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	first, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "Vic", "first")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if err := s.Archive(ctx, first.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	second, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "Vic", "second")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	markRead(t, s, first)

	// Appending to the older conversation must not touch the newer one, even
	// though the newer one is the visitor's most recently active open thread.
	conv, msg, err := s.AppendVisitorMessage(ctx, first.ID, "follow-up")
	if err != nil {
		t.Fatalf("AppendVisitorMessage: %v", err)
	}
	if conv.ID != first.ID {
		t.Errorf("conversation id = %d, want %d", conv.ID, first.ID)
	}
	if !conv.Unread {
		t.Error("Unread = false, want the conversation flagged unread again")
	}
	if !conv.LastMessageAt.Equal(msg.CreatedAt) {
		t.Errorf("LastMessageAt = %v, want %v", conv.LastMessageAt, msg.CreatedAt)
	}

	firstMsgs, err := s.ListMessages(ctx, first.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(firstMsgs) != 2 || firstMsgs[1].Body != "follow-up" {
		t.Errorf("messages = %+v, want the follow-up appended", firstMsgs)
	}
	secondMsgs, err := s.ListMessages(ctx, second.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(secondMsgs) != 1 {
		t.Errorf("other conversation has %d messages, want 1", len(secondMsgs))
	}
}

func TestAppendVisitorMessageUnknownConversation(t *testing.T) {
	s := openTestStore(t)
	if _, _, err := s.AppendVisitorMessage(context.Background(), 999, "hi"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestAddOperatorReply(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "Vic", "help")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}

	msg, err := s.AddOperatorReply(ctx, conv.ID, "Op@Example.com", " on it ")
	if err != nil {
		t.Fatalf("AddOperatorReply: %v", err)
	}
	if msg.Sender != SenderOperator || msg.OperatorEmail != "op@example.com" || msg.Body != "on it" {
		t.Errorf("message = %+v, want normalized operator reply", msg)
	}

	updated, err := s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if !updated.LastMessageAt.Equal(msg.CreatedAt) {
		t.Errorf("LastMessageAt = %v, want %v", updated.LastMessageAt, msg.CreatedAt)
	}
	if !updated.Unread {
		t.Error("Unread = false, want unread untouched by operator replies")
	}
}

func TestAddOperatorReplyDoesNotSetUnread(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", "help")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	markRead(t, s, conv)
	if _, err := s.AddOperatorReply(ctx, conv.ID, "op@example.com", "reply"); err != nil {
		t.Fatalf("AddOperatorReply: %v", err)
	}

	got, err := s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Unread {
		t.Error("Unread = true, want read after operator reply")
	}
}

func TestAddOperatorReplyErrors(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", "help")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}

	if _, err := s.AddOperatorReply(ctx, 999, "op@example.com", "reply"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown conversation err = %v, want ErrNotFound", err)
	}
	if _, err := s.AddOperatorReply(ctx, conv.ID, "op@example.com", "   "); err == nil {
		t.Error("expected error for empty body")
	}
	if _, err := s.AddOperatorReply(ctx, conv.ID, "", "reply"); err == nil {
		t.Error("expected error for missing operator email")
	}

	msgs, err := s.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("messages = %d, want 1 (failed replies must roll back)", len(msgs))
	}
}

func TestUnreadTransitions(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", "help")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if !conv.Unread {
		t.Fatal("new conversation should be unread")
	}

	markRead(t, s, conv)
	if got := mustGet(t, s, conv.ID); got.Unread {
		t.Error("Unread = true after MarkRead")
	}

	// MarkRead is idempotent.
	if _, err := s.MarkRead(ctx, conv.ID, conv.LastMessageAt); err != nil {
		t.Fatalf("second MarkRead: %v", err)
	}

	if _, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", "still there?"); err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if got := mustGet(t, s, conv.ID); !got.Unread {
		t.Error("Unread = false after a follow-up visitor message")
	}

	// A message that arrived after the reader loaded the history keeps the
	// conversation unread: nobody has seen it yet.
	if cleared, err := s.MarkRead(ctx, conv.ID, conv.LastMessageAt); err != nil || cleared {
		t.Errorf("MarkRead(stale) = %v, %v, want false, nil", cleared, err)
	}
	if got := mustGet(t, s, conv.ID); !got.Unread {
		t.Error("Unread = false, want the unseen message to keep it unread")
	}
}

// markRead clears the unread flag as of the conversation the caller holds.
func markRead(t *testing.T, s *Store, conv *Conversation) {
	t.Helper()
	cleared, err := s.MarkRead(context.Background(), conv.ID, conv.LastMessageAt)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if !cleared {
		t.Fatal("MarkRead cleared nothing, want the unread flag cleared")
	}
}

func TestArchiveUnarchive(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	conv, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", "help")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}

	if err := s.Archive(ctx, conv.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if got := mustGet(t, s, conv.ID); got.Status != StatusArchived {
		t.Errorf("Status = %q, want %q", got.Status, StatusArchived)
	}

	if err := s.Unarchive(ctx, conv.ID); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if got := mustGet(t, s, conv.ID); got.Status != StatusOpen {
		t.Errorf("Status = %q, want %q", got.Status, StatusOpen)
	}
}

func TestStatusMutationsNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	tests := map[string]func(context.Context, int64) error{
		"Archive":   s.Archive,
		"Unarchive": s.Unarchive,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			if err := fn(ctx, 999); !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}

	// MarkRead has nothing to report about a conversation that is not there:
	// it clears no row and that is the whole answer.
	if cleared, err := s.MarkRead(ctx, 999, time.Now()); err != nil || cleared {
		t.Errorf("MarkRead(unknown) = %v, %v, want false, nil", cleared, err)
	}
}

func TestListConversations(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	oldest, _, _, err := s.AddVisitorMessage(ctx, "a@example.com", "Ann", "first from ann")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	middle, _, _, err := s.AddVisitorMessage(ctx, "b@example.com", "Bob", "first from bob")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	// Ann replies again → she moves to the top.
	if _, _, _, err := s.AddVisitorMessage(ctx, "a@example.com", "", "ann again"); err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	newest, _, _, err := s.AddVisitorMessage(ctx, "c@example.com", "Cal", "first from cal")
	if err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	if _, _, _, err := s.AddVisitorMessage(ctx, "a@example.com", "", "ann one more"); err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}

	list, err := s.ListConversations(ctx, StatusOpen)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	gotOrder := make([]int64, len(list))
	for i, c := range list {
		gotOrder[i] = c.ID
	}
	wantOrder := []int64{oldest.ID, newest.ID, middle.ID}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("ids = %v, want %v", gotOrder, wantOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("ids = %v, want %v (last activity DESC)", gotOrder, wantOrder)
		}
	}

	top := list[0]
	if top.VisitorEmail != "a@example.com" || top.VisitorName != "Ann" {
		t.Errorf("visitor info = %q/%q, want ann", top.VisitorEmail, top.VisitorName)
	}
	if !top.Unread {
		t.Error("Unread = false, want true")
	}
	if top.Snippet != "ann one more" || top.LastSender != SenderVisitor {
		t.Errorf("snippet = %q/%q, want last visitor message", top.Snippet, top.LastSender)
	}
	if top.Token == "" {
		t.Error("Token is empty")
	}

	// Operator reply becomes the snippet; archived conversations leave the open list.
	if _, err := s.AddOperatorReply(ctx, middle.ID, "op@example.com", "on it, Bob"); err != nil {
		t.Fatalf("AddOperatorReply: %v", err)
	}
	if err := s.Archive(ctx, newest.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	open, err := s.ListConversations(ctx, StatusOpen)
	if err != nil {
		t.Fatalf("ListConversations open: %v", err)
	}
	if len(open) != 2 || open[0].ID != middle.ID {
		t.Fatalf("open list = %+v, want bob on top of two", open)
	}
	if open[0].Snippet != "on it, Bob" || open[0].LastSender != SenderOperator {
		t.Errorf("snippet = %q/%q, want operator reply", open[0].Snippet, open[0].LastSender)
	}

	archived, err := s.ListConversations(ctx, StatusArchived)
	if err != nil {
		t.Fatalf("ListConversations archived: %v", err)
	}
	if len(archived) != 1 || archived[0].ID != newest.ID {
		t.Fatalf("archived list = %+v, want only the archived conversation", archived)
	}

	all, err := s.ListConversations(ctx, "")
	if err != nil {
		t.Fatalf("ListConversations all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all = %d, want 3", len(all))
	}
}

func TestListConversationsEmptyAndInvalidStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	list, err := s.ListConversations(ctx, StatusOpen)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("len = %d, want 0", len(list))
	}

	if _, err := s.ListConversations(ctx, "bogus"); err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestListConversationsWithoutMessages(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.CreateConversation(ctx, NewConversation{VisitorEmail: "v@example.com"}); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	list, err := s.ListConversations(ctx, StatusOpen)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].Snippet != "" || list[0].LastSender != "" {
		t.Errorf("snippet = %q/%q, want empty", list[0].Snippet, list[0].LastSender)
	}
}

func TestSnippetCollapsesAndTruncates(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"collapses whitespace", "  hello\n\n  world \t!", "hello world !"},
		{"empty", "   ", ""},
		{"short unchanged", "hi", "hi"},
		{"truncates", strings.Repeat("a", snippetLen+10), strings.Repeat("a", snippetLen) + "…"},
		{"multibyte safe", strings.Repeat("é", snippetLen+5), strings.Repeat("é", snippetLen) + "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := snippet(tc.in); got != tc.want {
				t.Errorf("snippet(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestListConversationsSnippetTruncated(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	long := strings.Repeat("word ", 100)
	if _, _, _, err := s.AddVisitorMessage(ctx, "v@example.com", "", long); err != nil {
		t.Fatalf("AddVisitorMessage: %v", err)
	}
	list, err := s.ListConversations(ctx, StatusOpen)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if got := len([]rune(list[0].Snippet)); got > snippetLen+1 {
		t.Errorf("snippet length = %d, want at most %d", got, snippetLen+1)
	}
	if !strings.HasSuffix(list[0].Snippet, "…") {
		t.Errorf("snippet = %q, want ellipsis suffix", list[0].Snippet)
	}
}

func mustGet(t *testing.T, s *Store, id int64) *Conversation {
	t.Helper()
	c, err := s.GetConversation(context.Background(), id)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	return c
}
