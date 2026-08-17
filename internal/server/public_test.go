package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kooler/freesupp/internal/captcha"
	"github.com/kooler/freesupp/internal/store"
)

func jsonRequest(method, target string, body any) *http.Request {
	var buf bytes.Buffer
	if s, ok := body.(string); ok {
		buf.WriteString(s)
	} else if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, target, &buf)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding response %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestNewMessageSuccess(t *testing.T) {
	env := newTestEnv(t)

	rec := env.do(jsonRequest(http.MethodPost, "/api/messages", map[string]string{
		"email":           "Visitor@Example.com",
		"name":            "  Visitor  ",
		"message":         "  hello there  ",
		"turnstile_token": "tok",
	}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body)
	}
	if got := decodeBody[newMessageResponse](t, rec).Token; got != "" {
		t.Fatalf("response token = %q, want none — the link is emailed", got)
	}
	if env.verifier.token != "tok" {
		t.Errorf("verifier token = %q, want %q", env.verifier.token, "tok")
	}

	conv, err := env.store.GetConversationByToken(context.Background(),
		receiptToken(t, env, "visitor@example.com"))
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	if conv.VisitorEmail != "visitor@example.com" {
		t.Errorf("visitor email = %q, want normalized lowercase", conv.VisitorEmail)
	}
	if conv.VisitorName != "Visitor" {
		t.Errorf("visitor name = %q, want %q", conv.VisitorName, "Visitor")
	}
	if !conv.Unread {
		t.Error("conversation should be unread")
	}

	msgs, err := env.store.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != "hello there" {
		t.Fatalf("messages = %+v, want one trimmed body", msgs)
	}

	// Both operators are notified, and the notification carries the body.
	sent := env.sender.messages()
	if len(sent) != 3 {
		t.Fatalf("sent %d emails, want 3 (one per operator plus the visitor receipt)", len(sent))
	}
	for _, m := range sent {
		if m.To == "visitor@example.com" {
			// The submitter is unverified, so the receipt must not relay their
			// words into the mailbox — only the link belongs there.
			if strings.Contains(m.Body, "hello there") {
				t.Errorf("receipt relays the submitted text: %s", m.Body)
			}
			continue
		}
		if !strings.Contains(m.Body, "hello there") {
			t.Errorf("notification to %s missing message body: %s", m.To, m.Body)
		}
	}
}

// A visitor double-submitting the form keeps one conversation: both messages
// land in it and every submission notifies the operators. The receipt with the
// magic link is sent once, for the submission that opened the thread.
func TestNewMessageDuplicateSubmission(t *testing.T) {
	env := newTestEnv(t)
	payload := map[string]string{"email": "visitor@example.com", "message": "hello there"}

	first := env.do(jsonRequest(http.MethodPost, "/api/messages", payload))
	second := env.do(jsonRequest(http.MethodPost, "/api/messages", payload))

	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d, %d, want %d twice", first.Code, second.Code, http.StatusCreated)
	}

	conv, err := env.store.GetConversationByToken(context.Background(),
		receiptToken(t, env, "visitor@example.com"))
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	msgs, err := env.store.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("messages = %d, want both submissions in one conversation", len(msgs))
	}
	if sent := env.sender.messages(); len(sent) != 5 {
		t.Errorf("sent %d emails, want 5 (two operators × two submissions + one receipt)", len(sent))
	}
}

// POST /api/messages is unauthenticated and never proves the submitter owns the
// address, so the thread token is emailed to that address instead of returned.
// Otherwise an attacker could open a thread for someone else's address, keep
// its token, and read everything the owner later writes into the same thread.
func TestNewMessageEmailsTheTokenInsteadOfReturningIt(t *testing.T) {
	env := newTestEnv(t)

	// An attacker who merely knows the address opens the thread first.
	attacker := env.do(jsonRequest(http.MethodPost, "/api/messages", map[string]string{
		"email": "victim@example.com", "message": "attacker probe",
	}))
	if attacker.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", attacker.Code, http.StatusCreated)
	}
	if got := decodeBody[newMessageResponse](t, attacker).Token; got != "" {
		t.Fatalf("attacker received token %q, want none", got)
	}

	// The victim's own submission joins that still-open thread...
	victim := env.do(jsonRequest(http.MethodPost, "/api/messages", map[string]string{
		"email": "victim@example.com", "message": "my card number is secret",
	}))
	if got := decodeBody[newMessageResponse](t, victim).Token; got != "" {
		t.Fatalf("victim received token %q, want none", got)
	}

	// ...and the only copy of the token went to the victim's mailbox.
	for _, m := range env.sender.messages() {
		if strings.Contains(m.Body, "/t/") && m.To != "victim@example.com" {
			t.Errorf("magic link sent to %s: %s", m.To, m.Body)
		}
	}
	token := receiptToken(t, env, "victim@example.com")
	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/thread/"+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("victim thread status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "my card number is secret") {
		t.Error("victim should see their own history through the emailed link")
	}
}

func TestNewMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		want    int
	}{
		{name: "invalid email", payload: map[string]string{"email": "nope", "message": "hi"}, want: http.StatusBadRequest},
		{name: "empty email", payload: map[string]string{"email": "", "message": "hi"}, want: http.StatusBadRequest},
		{
			name:    "email with display name",
			payload: map[string]string{"email": "Bob <bob@example.com>", "message": "hi"},
			want:    http.StatusBadRequest,
		},
		{
			name:    "email too long",
			payload: map[string]string{"email": strings.Repeat("a", 200) + "@example.com", "message": "hi"},
			want:    http.StatusBadRequest,
		},
		{name: "empty message", payload: map[string]string{"email": "v@example.com", "message": "   "}, want: http.StatusBadRequest},
		{
			name:    "message too long",
			payload: map[string]string{"email": "v@example.com", "message": strings.Repeat("x", maxMessageLen+1)},
			want:    http.StatusBadRequest,
		},
		{
			name:    "name too long",
			payload: map[string]string{"email": "v@example.com", "message": "hi", "name": strings.Repeat("n", maxFieldLen+1)},
			want:    http.StatusBadRequest,
		},
		{name: "malformed json", payload: "{not json", want: http.StatusBadRequest},
		{name: "unknown field", payload: `{"email":"v@example.com","message":"hi","admin":true}`, want: http.StatusBadRequest},
		{
			name:    "second json object",
			payload: `{"email":"v@example.com","message":"hi"}{"message":"smuggled"}`,
			want:    http.StatusBadRequest,
		},
		{
			name:    "trailing garbage",
			payload: `{"email":"v@example.com","message":"hi"} nonsense`,
			want:    http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			rec := env.do(jsonRequest(http.MethodPost, "/api/messages", tt.payload))

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
			if msg := decodeBody[errorResponse](t, rec).Error; msg == "" {
				t.Error("error response has no message")
			}
			if sent := env.sender.messages(); len(sent) != 0 {
				t.Errorf("sent %d emails on a rejected submission, want 0", len(sent))
			}
		})
	}
}

func TestNewMessageOversizedBody(t *testing.T) {
	env := newTestEnv(t)
	huge := strings.Repeat("x", maxBodyBytes+1024)
	rec := env.do(jsonRequest(http.MethodPost, "/api/messages", map[string]string{
		"email": "v@example.com", "message": huge,
	}))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestNewMessageCaptcha(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "rejected", err: captcha.ErrFailed, want: http.StatusBadRequest},
		{name: "service unavailable", err: errors.New("dial tcp: timeout"), want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.verifier.err = tt.err

			rec := env.do(jsonRequest(http.MethodPost, "/api/messages", map[string]string{
				"email": "v@example.com", "message": "hi", "turnstile_token": "bad",
			}))

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
			convs, err := env.store.ListConversations(context.Background(), "")
			if err != nil {
				t.Fatalf("ListConversations() error = %v", err)
			}
			if len(convs) != 0 {
				t.Errorf("stored %d conversations despite failed captcha, want 0", len(convs))
			}
			if sent := env.sender.messages(); len(sent) != 0 {
				t.Errorf("sent %d emails despite failed captcha, want 0", len(sent))
			}
		})
	}
}

func TestNewMessagePassesClientIPToVerifier(t *testing.T) {
	env := newTestEnv(t, map[string]string{"USE_PROXY": "true"})
	r := jsonRequest(http.MethodPost, "/api/messages", map[string]string{"email": "v@example.com", "message": "hi"})
	// The proxy appends the peer it saw, so the visitor is the last entry.
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.9")

	if rec := env.do(r); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if env.verifier.ip != "198.51.100.9" {
		t.Errorf("verifier remote IP = %q, want %q", env.verifier.ip, "198.51.100.9")
	}
}

// Without USE_PROXY the header is attacker-controlled, so Turnstile must be
// told the peer address instead.
func TestNewMessageIgnoresForwardedForWithoutProxy(t *testing.T) {
	env := newTestEnv(t)
	r := jsonRequest(http.MethodPost, "/api/messages", map[string]string{"email": "v@example.com", "message": "hi"})
	r.RemoteAddr = "192.0.2.7:5555"
	r.Header.Set("X-Forwarded-For", "198.51.100.9")

	if rec := env.do(r); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if env.verifier.ip != "192.0.2.7" {
		t.Errorf("verifier remote IP = %q, want the peer address %q", env.verifier.ip, "192.0.2.7")
	}
}

// seedConversation posts one visitor message and returns its thread token.
func seedConversation(t *testing.T, env *testEnv, email, message string) string {
	t.Helper()
	rec := env.do(jsonRequest(http.MethodPost, "/api/messages", map[string]string{
		"email": email, "name": "Visitor", "message": message,
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body)
	}
	return receiptToken(t, env, email)
}

// receiptToken reads the magic-link token out of the latest email carrying one
// to the visitor — the only place the server ever discloses it.
func receiptToken(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	sent := env.sender.messages()
	for i := len(sent) - 1; i >= 0; i-- {
		m := sent[i]
		if !strings.EqualFold(m.To, email) {
			continue
		}
		if _, after, ok := strings.Cut(m.Body, "/t/"); ok {
			return strings.TrimSpace(strings.SplitN(after, "\n", 2)[0])
		}
	}
	t.Fatalf("no email with a magic link was sent to %s", email)
	return ""
}

func TestGetThread(t *testing.T) {
	env := newTestEnv(t)
	token := seedConversation(t, env, "v@example.com", "first question")

	conv, err := env.store.GetConversationByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	if _, err := env.store.AddOperatorReply(context.Background(), conv.ID, "ops@example.com", "here is the answer"); err != nil {
		t.Fatalf("AddOperatorReply() error = %v", err)
	}

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/thread/"+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body)
	}

	got := decodeBody[threadResponse](t, rec)
	if got.Token != token || got.Status != store.StatusOpen {
		t.Errorf("thread = %+v, want token %q and status open", got, token)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(got.Messages))
	}
	if got.Messages[0].Sender != store.SenderVisitor || got.Messages[0].Body != "first question" {
		t.Errorf("first message = %+v, want the visitor question", got.Messages[0])
	}
	if got.Messages[1].Author != operatorLabel {
		t.Errorf("operator message author = %q, want %q", got.Messages[1].Author, operatorLabel)
	}
	// The operator's identity must never reach the visitor.
	if strings.Contains(rec.Body.String(), "ops@example.com") {
		t.Errorf("thread response leaks the operator email: %s", rec.Body)
	}
}

func TestGetThreadUnknownToken(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/thread/deadbeef", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if msg := decodeBody[errorResponse](t, rec).Error; msg == "" {
		t.Error("error response has no message")
	}
}

func TestThreadFollowUpAppends(t *testing.T) {
	env := newTestEnv(t)
	token := seedConversation(t, env, "v@example.com", "first question")
	env.sender.sent = nil

	rec := env.do(jsonRequest(http.MethodPost, "/api/thread/"+token+"/messages",
		map[string]string{"message": "one more thing"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body)
	}
	if got := decodeBody[newMessageResponse](t, rec).Token; got != token {
		t.Errorf("token = %q, want the same conversation %q", got, token)
	}

	conv, err := env.store.GetConversationByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	msgs, err := env.store.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if sent := env.sender.messages(); len(sent) != 2 {
		t.Errorf("sent %d emails for the follow-up, want 2 (one per operator)", len(sent))
	}
}

func TestThreadFollowUpAfterArchiveStartsNewConversation(t *testing.T) {
	env := newTestEnv(t)
	token := seedConversation(t, env, "v@example.com", "first question")

	conv, err := env.store.GetConversationByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	if err := env.store.Archive(context.Background(), conv.ID); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	rec := env.do(jsonRequest(http.MethodPost, "/api/thread/"+token+"/messages",
		map[string]string{"message": "actually one more thing"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body)
	}

	newToken := decodeBody[newMessageResponse](t, rec).Token
	if newToken == token {
		t.Fatal("follow-up after archiving should return a new conversation token")
	}
	fresh, err := env.store.GetConversationByToken(context.Background(), newToken)
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	if fresh.Status != store.StatusOpen || fresh.VisitorEmail != "v@example.com" {
		t.Errorf("new conversation = %+v, want an open thread for the same visitor", fresh)
	}
}

// A magic link identifies one conversation. When an operator unarchives an old
// thread the visitor can have two open conversations at once; a follow-up must
// still land in the one its own link points at, not in whichever is newest.
func TestThreadFollowUpStaysInTheLinkedConversation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	firstToken := seedConversation(t, env, "v@example.com", "first question")
	first, err := env.store.GetConversationByToken(ctx, firstToken)
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	// Archive it, let the visitor open a second thread, then unarchive the first.
	if err := env.store.Archive(ctx, first.ID); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	secondToken := seedConversation(t, env, "v@example.com", "unrelated question")
	if secondToken == firstToken {
		t.Fatal("second submission reused the archived conversation")
	}
	if err := env.store.Unarchive(ctx, first.ID); err != nil {
		t.Fatalf("Unarchive() error = %v", err)
	}

	rec := env.do(jsonRequest(http.MethodPost, "/api/thread/"+firstToken+"/messages",
		map[string]string{"message": "back to my first question"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body)
	}
	if got := decodeBody[newMessageResponse](t, rec).Token; got != firstToken {
		t.Errorf("token = %q, want the linked conversation %q", got, firstToken)
	}

	msgs, err := env.store.ListMessages(ctx, first.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(msgs) != 2 || msgs[1].Body != "back to my first question" {
		t.Errorf("linked conversation messages = %+v, want the follow-up appended", msgs)
	}

	second, err := env.store.GetConversationByToken(ctx, secondToken)
	if err != nil {
		t.Fatalf("GetConversationByToken() error = %v", err)
	}
	other, err := env.store.ListMessages(ctx, second.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(other) != 1 {
		t.Errorf("other conversation has %d messages, want the follow-up to have stayed out", len(other))
	}
}

func TestThreadFollowUpValidation(t *testing.T) {
	env := newTestEnv(t)
	token := seedConversation(t, env, "v@example.com", "first question")

	tests := []struct {
		name   string
		target string
		body   any
		want   int
	}{
		{name: "empty message", target: "/api/thread/" + token + "/messages", body: map[string]string{"message": " "}, want: http.StatusBadRequest},
		{
			name:   "message too long",
			target: "/api/thread/" + token + "/messages",
			body:   map[string]string{"message": strings.Repeat("x", maxMessageLen+1)},
			want:   http.StatusBadRequest,
		},
		{name: "unknown token", target: "/api/thread/nope/messages", body: map[string]string{"message": "hi"}, want: http.StatusNotFound},
		{name: "malformed json", target: "/api/thread/" + token + "/messages", body: "{", want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := env.do(jsonRequest(http.MethodPost, tt.target, tt.body))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestValidEmail(t *testing.T) {
	tests := []struct {
		in    string
		want  string
		valid bool
	}{
		{in: " Visitor@Example.COM ", want: "visitor@example.com", valid: true},
		{in: "a+tag@sub.example.co.uk", want: "a+tag@sub.example.co.uk", valid: true},
		{in: "", valid: false},
		{in: "no-at-sign", valid: false},
		{in: "two@@example.com", valid: false},
		{in: "Bob <bob@example.com>", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := validEmail(tt.in)
			if ok != tt.valid || got != tt.want {
				t.Errorf("validEmail(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.valid)
			}
		})
	}
}
