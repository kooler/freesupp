package mail

import (
	"strings"
	"testing"
	"time"

	"github.com/kooler/freesupp/internal/store"
)

const testBaseURL = "https://support.example.com"

func testConversation() store.Conversation {
	return store.Conversation{
		ID:            7,
		VisitorEmail:  "alice@example.com",
		VisitorName:   "Alice",
		Token:         "deadbeef",
		Status:        store.StatusOpen,
		Unread:        true,
		CreatedAt:     time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC),
		LastMessageAt: time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC),
	}
}

func TestRenderOperator(t *testing.T) {
	tests := []struct {
		name        string
		conv        func(store.Conversation) store.Conversation
		body        string
		wantSubject string
		wantIn      []string
		wantNotIn   []string
	}{
		{
			name:        "named visitor",
			conv:        func(c store.Conversation) store.Conversation { return c },
			body:        "My login is broken.\nPlease help.",
			wantSubject: "New support message from Alice (alice@example.com)",
			wantIn: []string{
				"Alice (alice@example.com) sent a support request",
				"My login is broken.\nPlease help.",
				"From: alice@example.com",
				testBaseURL + "/conversations/7",
			},
		},
		{
			name: "anonymous visitor falls back to email",
			conv: func(c store.Conversation) store.Conversation {
				c.VisitorName = ""
				return c
			},
			body:        "hello",
			wantSubject: "New support message from alice@example.com",
			wantIn:      []string{"alice@example.com sent a support request"},
			wantNotIn:   []string{"()"},
		},
		{
			name:        "plain text is not html escaped",
			conv:        func(c store.Conversation) store.Conversation { return c },
			body:        `5 < 6 && "quotes" stay literal`,
			wantSubject: "New support message from Alice (alice@example.com)",
			wantIn:      []string{`5 < 6 && "quotes" stay literal`},
			wantNotIn:   []string{"&lt;", "&amp;", "&#34;"},
		},
		{
			name:        "body is trimmed",
			conv:        func(c store.Conversation) store.Conversation { return c },
			body:        "\n\n  padded  \n\n",
			wantSubject: "New support message from Alice (alice@example.com)",
			wantIn:      []string{"request:\n\npadded\n\n--"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := tt.conv(testConversation())
			msg := store.Message{ID: 1, ConversationID: conv.ID, Sender: store.SenderVisitor, Body: tt.body}

			subject, body, err := renderOperator(testBaseURL, conv, msg)
			if err != nil {
				t.Fatalf("renderOperator: %v", err)
			}
			if subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", subject, tt.wantSubject)
			}
			for _, want := range tt.wantIn {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q\n---\n%s", want, body)
				}
			}
			for _, bad := range tt.wantNotIn {
				if strings.Contains(body, bad) {
					t.Errorf("body unexpectedly contains %q\n---\n%s", bad, body)
				}
			}
		})
	}
}

func TestRenderVisitor(t *testing.T) {
	tests := []struct {
		name      string
		conv      func(store.Conversation) store.Conversation
		body      string
		wantIn    []string
		wantNotIn []string
	}{
		{
			name:   "includes reply body and magic link",
			conv:   func(c store.Conversation) store.Conversation { return c },
			body:   "Try resetting your password.",
			wantIn: []string{"Hi Alice,", "Support replied to your message:", "Try resetting your password.", testBaseURL + "/t/deadbeef"},
			// operator identity must never leak to the visitor
			wantNotIn: []string{"@support.example.com", "operator"},
		},
		{
			name: "no greeting without a name",
			conv: func(c store.Conversation) store.Conversation {
				c.VisitorName = ""
				return c
			},
			body:      "done",
			wantIn:    []string{"Support replied to your message:"},
			wantNotIn: []string{"Hi ,"},
		},
		{
			name:      "plain text is not html escaped",
			conv:      func(c store.Conversation) store.Conversation { return c },
			body:      `use "--force" if a < b`,
			wantIn:    []string{`use "--force" if a < b`},
			wantNotIn: []string{"&lt;", "&#34;"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := tt.conv(testConversation())
			msg := store.Message{
				ID: 2, ConversationID: conv.ID,
				Sender: store.SenderOperator, OperatorEmail: "op@support.example.com", Body: tt.body,
			}

			subject, body, err := renderVisitor(testBaseURL, conv, msg)
			if err != nil {
				t.Fatalf("renderVisitor: %v", err)
			}
			if want := "Re: your support request (#7)"; subject != want {
				t.Errorf("subject = %q, want %q", subject, want)
			}
			for _, want := range tt.wantIn {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q\n---\n%s", want, body)
				}
			}
			for _, bad := range tt.wantNotIn {
				if strings.Contains(body, bad) {
					t.Errorf("body unexpectedly contains %q\n---\n%s", bad, body)
				}
			}
		})
	}
}

func TestRenderReceipt(t *testing.T) {
	conv := testConversation()

	subject, body, err := renderReceipt(testBaseURL, conv)
	if err != nil {
		t.Fatalf("renderReceipt: %v", err)
	}
	if want := "We got your support request (#7)"; subject != want {
		t.Errorf("subject = %q, want %q", subject, want)
	}
	for _, want := range []string{"Hi Alice,", testBaseURL + "/t/deadbeef"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---\n%s", want, body)
		}
	}

	conv.VisitorName = ""
	if _, body, err = renderReceipt(testBaseURL, conv); err != nil {
		t.Fatalf("renderReceipt: %v", err)
	}
	if strings.Contains(body, "Hi ,") {
		t.Errorf("body greets an empty name\n---\n%s", body)
	}
}

func TestURLHelpers(t *testing.T) {
	tests := []struct {
		name, base, want string
		fn               func(base string) string
	}{
		{"thread url", testBaseURL, testBaseURL + "/t/tok", func(b string) string { return ThreadURL(b, "tok") }},
		{"thread url trims slash", testBaseURL + "/", testBaseURL + "/t/tok", func(b string) string { return ThreadURL(b, "tok") }},
		{"inbox url", testBaseURL, testBaseURL + "/conversations/42", func(b string) string { return InboxURL(b, 42) }},
		{"inbox url trims slash", testBaseURL + "//", testBaseURL + "/conversations/42", func(b string) string { return InboxURL(b, 42) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(tt.base); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
