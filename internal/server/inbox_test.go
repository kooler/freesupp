package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kooler/freesupp/internal/store"
)

// authed builds a request carrying the operator session cookie.
func authed(t *testing.T, sess *http.Cookie, method, target string, body any) *http.Request {
	t.Helper()
	req := jsonRequest(method, target, body)
	req.AddCookie(sess)
	return req
}

// seed adds a visitor message straight through the store.
func seed(t *testing.T, env *testEnv, email, name, body string) *store.Conversation {
	t.Helper()
	conv, _, _, err := env.store.AddVisitorMessage(context.Background(), email, name, body)
	if err != nil {
		t.Fatalf("AddVisitorMessage() error = %v", err)
	}
	return conv
}

// Every handler has a store-failure branch that logs and returns 5xx. Closing
// the store is the cheapest way to reach them; without this they are dead code
// in the suite and a leaked internal error would go unnoticed.
func TestHandlersReportStoreFailures(t *testing.T) {
	tests := []struct {
		name          string
		method, path  string
		body          any
		wantStatus    int
		needOperator  bool
		needSeededURL bool
	}{
		{name: "list conversations", method: http.MethodGet, path: "/api/inbox/conversations", wantStatus: http.StatusInternalServerError, needOperator: true},
		{name: "get conversation", method: http.MethodGet, path: "/api/inbox/conversations/%d", wantStatus: http.StatusInternalServerError, needOperator: true, needSeededURL: true},
		{
			name: "reply", method: http.MethodPost, path: "/api/inbox/conversations/%d/reply",
			body: map[string]string{"message": "hi"}, wantStatus: http.StatusInternalServerError,
			needOperator: true, needSeededURL: true,
		},
		{
			name: "archive", method: http.MethodPost, path: "/api/inbox/conversations/%d/archive",
			wantStatus: http.StatusInternalServerError, needOperator: true, needSeededURL: true,
		},
		{name: "new visitor message", method: http.MethodPost, path: "/api/messages", body: map[string]string{"email": "v@example.com", "message": "hi"}, wantStatus: http.StatusInternalServerError},
		{name: "get thread", method: http.MethodGet, path: "/api/thread/%s", wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			sess := login(t, env)
			conv := seed(t, env, "v@example.com", "Vic", "first message")

			path := tt.path
			switch {
			case tt.needSeededURL:
				path = strings.Replace(tt.path, "%d", strconv.FormatInt(conv.ID, 10), 1)
			case strings.Contains(tt.path, "%s"):
				path = strings.Replace(tt.path, "%s", conv.Token, 1)
			}

			// Everything after this point must fail at the store.
			if err := env.store.Close(); err != nil {
				t.Fatalf("closing store: %v", err)
			}

			req := jsonRequest(tt.method, path, tt.body)
			if tt.needOperator {
				req.AddCookie(sess)
			}
			rec := env.do(req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			msg := decodeBody[errorResponse](t, rec).Error
			if msg == "" {
				t.Fatal("error response has no message")
			}
			// The response must not carry the driver's internal detail.
			for _, leak := range []string{"sql:", "store:", "database"} {
				if strings.Contains(strings.ToLower(msg), leak) {
					t.Errorf("error message %q leaks internal detail %q", msg, leak)
				}
			}
		})
	}
}

func TestListConversationsOrdersByActivity(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)

	first := seed(t, env, "a@example.com", "Ann", "first message")
	second := seed(t, env, "b@example.com", "Bob", "second message")
	// Bump the older conversation so it should come back first.
	if _, err := env.store.AddOperatorReply(context.Background(), first.ID, "ops@example.com", "on it"); err != nil {
		t.Fatalf("AddOperatorReply() error = %v", err)
	}

	rec := env.do(authed(t, sess, http.MethodGet, "/api/inbox/conversations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body)
	}
	got := decodeBody[conversationListResponse](t, rec)

	if len(got.Conversations) != 2 {
		t.Fatalf("got %d conversations, want 2", len(got.Conversations))
	}
	if got.Conversations[0].ID != first.ID || got.Conversations[1].ID != second.ID {
		t.Errorf("order = %d,%d, want %d,%d (newest activity first)",
			got.Conversations[0].ID, got.Conversations[1].ID, first.ID, second.ID)
	}
	head := got.Conversations[0]
	if !head.Unread {
		t.Error("conversation with a visitor message should be unread")
	}
	if head.Snippet != "on it" || head.LastSender != store.SenderOperator {
		t.Errorf("snippet = %q / sender = %q, want last message preview", head.Snippet, head.LastSender)
	}
	if head.VisitorEmail != "a@example.com" || head.VisitorName != "Ann" {
		t.Errorf("visitor = %q / %q, want a@example.com / Ann", head.VisitorEmail, head.VisitorName)
	}
}

func TestListConversationsStatusFilter(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)

	open := seed(t, env, "open@example.com", "", "still open")
	archived := seed(t, env, "done@example.com", "", "all done")
	if err := env.store.Archive(context.Background(), archived.ID); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantIDs []int64
	}{
		{name: "default is open", query: "", wantIDs: []int64{open.ID}},
		{name: "explicit open", query: "?status=open", wantIDs: []int64{open.ID}},
		{name: "archived", query: "?status=archived", wantIDs: []int64{archived.ID}},
		{name: "all", query: "?status=all", wantIDs: []int64{archived.ID, open.ID}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := env.do(authed(t, sess, http.MethodGet, "/api/inbox/conversations"+tt.query, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body)
			}
			got := decodeBody[conversationListResponse](t, rec)
			if len(got.Conversations) != len(tt.wantIDs) {
				t.Fatalf("got %d conversations, want %d", len(got.Conversations), len(tt.wantIDs))
			}
			for i, want := range tt.wantIDs {
				if got.Conversations[i].ID != want {
					t.Errorf("conversation[%d] = %d, want %d", i, got.Conversations[i].ID, want)
				}
			}
		})
	}
}

func TestListConversationsRejectsUnknownStatus(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)

	rec := env.do(authed(t, sess, http.MethodGet, "/api/inbox/conversations?status=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetConversationMarksRead(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)
	conv := seed(t, env, "visitor@example.com", "Vic", "help me please")

	rec := env.do(authed(t, sess, http.MethodGet, conversationPath(conv.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body)
	}
	got := decodeBody[conversationDetail](t, rec)

	if got.Unread {
		t.Error("detail response should report the conversation as read")
	}
	if got.Token != conv.Token || got.VisitorEmail != "visitor@example.com" {
		t.Errorf("detail = %+v, want the seeded conversation", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Body != "help me please" {
		t.Fatalf("messages = %+v, want the visitor message", got.Messages)
	}

	stored, err := env.store.GetConversation(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if stored.Unread {
		t.Error("opening a conversation should clear the unread flag in the store")
	}
}

func TestGetConversationIncludesOperatorEmail(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)
	conv := seed(t, env, "visitor@example.com", "Vic", "hello")
	if _, err := env.store.AddOperatorReply(context.Background(), conv.ID, "ops@example.com", "hi back"); err != nil {
		t.Fatalf("AddOperatorReply() error = %v", err)
	}

	rec := env.do(authed(t, sess, http.MethodGet, conversationPath(conv.ID), nil))
	got := decodeBody[conversationDetail](t, rec)

	if len(got.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(got.Messages))
	}
	if got.Messages[1].Sender != store.SenderOperator || got.Messages[1].OperatorEmail != "ops@example.com" {
		t.Errorf("operator message = %+v, want the replying operator attributed", got.Messages[1])
	}
}

func TestReplySendsVisitorEmail(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)
	conv := seed(t, env, "visitor@example.com", "Vic", "hello")

	rec := env.do(authed(t, sess, http.MethodPost, conversationPath(conv.ID)+"/reply",
		map[string]string{"message": "  here is your answer  "}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body)
	}
	got := decodeBody[inboxMessage](t, rec)
	if got.Body != "here is your answer" || got.OperatorEmail != "ops@example.com" {
		t.Errorf("reply = %+v, want the trimmed body attributed to the signed-in operator", got)
	}

	msgs, err := env.store.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(msgs) != 2 || msgs[1].Sender != store.SenderOperator {
		t.Fatalf("stored messages = %+v, want the reply appended", msgs)
	}

	sent := env.sender.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 to the visitor", len(sent))
	}
	if sent[0].To != "visitor@example.com" {
		t.Errorf("email to = %q, want the visitor", sent[0].To)
	}
	if !strings.Contains(sent[0].Body, "here is your answer") {
		t.Errorf("email body = %q, want it to carry the reply", sent[0].Body)
	}
	if !strings.Contains(sent[0].Body, "/t/"+conv.Token) {
		t.Errorf("email body = %q, want it to carry the magic link", sent[0].Body)
	}
}

func TestReplyRejectsBadBodies(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)
	conv := seed(t, env, "visitor@example.com", "Vic", "hello")

	tests := []struct {
		name       string
		body       any
		wantStatus int
	}{
		{name: "empty message", body: map[string]string{"message": "   "}, wantStatus: http.StatusBadRequest},
		{name: "missing field", body: map[string]string{}, wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: map[string]string{"msg": "hi"}, wantStatus: http.StatusBadRequest},
		{name: "not json", body: "{", wantStatus: http.StatusBadRequest},
		{
			name:       "oversized message",
			body:       map[string]string{"message": strings.Repeat("x", maxMessageLen+1)},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := env.do(authed(t, sess, http.MethodPost, conversationPath(conv.ID)+"/reply", tt.body))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := len(env.sender.messages()); got != 0 {
				t.Errorf("sent %d emails for a rejected reply, want 0", got)
			}
		})
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)
	conv := seed(t, env, "visitor@example.com", "Vic", "hello")

	rec := env.do(authed(t, sess, http.MethodPost, conversationPath(conv.ID)+"/archive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body)
	}
	if got := decodeBody[conversationDetail](t, rec); got.Status != store.StatusArchived {
		t.Errorf("status = %q, want %q", got.Status, store.StatusArchived)
	}
	stored, err := env.store.GetConversation(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if stored.Status != store.StatusArchived {
		t.Fatalf("stored status = %q, want %q", stored.Status, store.StatusArchived)
	}

	rec = env.do(authed(t, sess, http.MethodPost, conversationPath(conv.ID)+"/unarchive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body)
	}
	if got := decodeBody[conversationDetail](t, rec); got.Status != store.StatusOpen {
		t.Errorf("status = %q, want %q", got.Status, store.StatusOpen)
	}
	stored, err = env.store.GetConversation(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if stored.Status != store.StatusOpen {
		t.Errorf("stored status = %q, want %q", stored.Status, store.StatusOpen)
	}
}

func TestInboxUnknownConversation404(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)

	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{name: "detail", method: http.MethodGet, path: "/api/inbox/conversations/999"},
		{
			name: "reply", method: http.MethodPost, path: "/api/inbox/conversations/999/reply",
			body: map[string]string{"message": "hi"},
		},
		{name: "archive", method: http.MethodPost, path: "/api/inbox/conversations/999/archive"},
		{name: "unarchive", method: http.MethodPost, path: "/api/inbox/conversations/999/unarchive"},
		{name: "non-numeric id", method: http.MethodGet, path: "/api/inbox/conversations/abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := env.do(authed(t, sess, tt.method, tt.path, tt.body))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNotFound, rec.Body)
			}
		})
	}
}

func TestInboxRequiresSession(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)
	conv := seed(t, env, "visitor@example.com", "Vic", "hello")

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/inbox/conversations"},
		{http.MethodGet, conversationPath(conv.ID)},
		{http.MethodPost, conversationPath(conv.ID) + "/reply"},
		{http.MethodPost, conversationPath(conv.ID) + "/archive"},
		{http.MethodPost, conversationPath(conv.ID) + "/unarchive"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := env.do(jsonRequest(tt.method, tt.path, map[string]string{"message": "hi"}))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}

	// The same routes work once the session cookie is attached.
	rec := env.do(authed(t, sess, http.MethodGet, "/api/inbox/conversations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// Replying does not clear or set unread; only opening a conversation does.
func TestReplyLeavesUnreadAlone(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)
	conv := seed(t, env, "visitor@example.com", "Vic", "hello")

	env.do(authed(t, sess, http.MethodPost, conversationPath(conv.ID)+"/reply",
		map[string]string{"message": "answer"}))

	stored, err := env.store.GetConversation(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if !stored.Unread {
		t.Error("unread flag changed on reply, want it untouched")
	}
}

func conversationPath(id int64) string {
	return "/api/inbox/conversations/" + strconv.FormatInt(id, 10)
}
