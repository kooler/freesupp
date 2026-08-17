package mail

import (
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kooler/freesupp/internal/config"
	"github.com/kooler/freesupp/internal/store"
)

// fakeSender records every Send call and can fail on demand.
type fakeSender struct {
	mu   sync.Mutex
	sent []sentEmail
	err  error
}

type sentEmail struct{ To, Subject, Body string }

func (f *fakeSender) Send(to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentEmail{to, subject, body})
	return f.err
}

func (f *fakeSender) recipients() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.sent))
	for _, e := range f.sent {
		out = append(out, e.To)
	}
	sort.Strings(out)
	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testNotifier(t *testing.T, sender Sender) *Notifier {
	t.Helper()
	cfg := &config.Config{BaseURL: testBaseURL}
	n := NewNotifier(cfg, sender, discardLogger())
	t.Cleanup(n.Wait)
	return n
}

func TestNotifyOperatorsFansOut(t *testing.T) {
	sender := &fakeSender{}
	n := testNotifier(t, sender)
	conv := testConversation()
	msg := store.Message{ID: 1, ConversationID: conv.ID, Sender: store.SenderVisitor, Body: "help me"}

	n.NotifyOperators(conv, msg, []string{"op1@example.com", "op2@example.com", "op3@example.com"})
	n.Wait()

	want := []string{"op1@example.com", "op2@example.com", "op3@example.com"}
	got := sender.recipients()
	if len(got) != len(want) {
		t.Fatalf("sent to %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sent to %v, want %v", got, want)
		}
	}
	for _, e := range sender.sent {
		if !strings.Contains(e.Body, "help me") {
			t.Errorf("body missing message: %q", e.Body)
		}
		if !strings.Contains(e.Body, testBaseURL+"/conversations/7") {
			t.Errorf("body missing inbox link: %q", e.Body)
		}
		if !strings.Contains(e.Subject, "alice@example.com") {
			t.Errorf("subject missing visitor: %q", e.Subject)
		}
	}
}

func TestNotifyOperatorsWithoutRecipients(t *testing.T) {
	sender := &fakeSender{}
	n := testNotifier(t, sender)

	n.NotifyOperators(testConversation(), store.Message{Body: "hi"}, nil)
	n.Wait()

	if len(sender.sent) != 0 {
		t.Fatalf("sent %d emails, want 0", len(sender.sent))
	}
}

func TestNotifyOperatorsKeepsGoingAfterSenderError(t *testing.T) {
	sender := &fakeSender{err: errors.New("smtp down")}
	n := testNotifier(t, sender)

	n.NotifyOperators(testConversation(), store.Message{Body: "hi"}, []string{"op1@example.com", "op2@example.com"})
	n.Wait() // must not panic or block; failures are logged only

	if len(sender.sent) != 2 {
		t.Fatalf("attempted %d sends, want 2", len(sender.sent))
	}
}

func TestNotifyVisitor(t *testing.T) {
	sender := &fakeSender{}
	n := testNotifier(t, sender)
	conv := testConversation()
	msg := store.Message{
		ID: 2, ConversationID: conv.ID,
		Sender: store.SenderOperator, OperatorEmail: "op1@example.com", Body: "have you tried logging out?",
	}

	n.NotifyVisitor(conv, msg)
	n.Wait()

	if len(sender.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(sender.sent))
	}
	e := sender.sent[0]
	if e.To != conv.VisitorEmail {
		t.Errorf("to = %q, want %q", e.To, conv.VisitorEmail)
	}
	if !strings.Contains(e.Body, "have you tried logging out?") {
		t.Errorf("body missing reply: %q", e.Body)
	}
	if !strings.Contains(e.Body, testBaseURL+"/t/deadbeef") {
		t.Errorf("body missing magic link: %q", e.Body)
	}
	if strings.Contains(e.Body, "op1@example.com") {
		t.Errorf("operator address leaked to visitor: %q", e.Body)
	}
}

func TestNotifyVisitorReceipt(t *testing.T) {
	sender := &fakeSender{}
	n := testNotifier(t, sender)
	conv := testConversation()

	n.NotifyVisitorReceipt(conv)
	n.Wait()

	if len(sender.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(sender.sent))
	}
	e := sender.sent[0]
	if e.To != conv.VisitorEmail {
		t.Errorf("to = %q, want the magic link delivered to the visitor's address", e.To)
	}
	if !strings.Contains(e.Body, testBaseURL+"/t/deadbeef") {
		t.Errorf("body missing magic link: %q", e.Body)
	}
}

func TestNotifyVisitorSenderError(t *testing.T) {
	sender := &fakeSender{err: errors.New("smtp down")}
	n := testNotifier(t, sender)

	n.NotifyVisitor(testConversation(), store.Message{Body: "hi"})
	n.Wait()

	if len(sender.sent) != 1 {
		t.Fatalf("attempted %d sends, want 1", len(sender.sent))
	}
}

func TestNotifyIsAsynchronous(t *testing.T) {
	release := make(chan struct{})
	sender := &blockingSender{release: release}
	n := testNotifier(t, sender)

	n.NotifyOperators(testConversation(), store.Message{Body: "hi"}, []string{"op1@example.com"})
	// If NotifyOperators blocked on the sender, we would never reach this line.
	close(release)
	n.Wait()

	if !sender.called {
		t.Fatal("sender was never called")
	}
}

func TestWaitTimeoutGivesUpOnAWedgedSender(t *testing.T) {
	release := make(chan struct{})
	n := testNotifier(t, &blockingSender{release: release})
	t.Cleanup(func() { close(release) })

	n.NotifyOperators(testConversation(), store.Message{Body: "hi"}, []string{"op1@example.com"})
	if n.WaitTimeout(10 * time.Millisecond) {
		t.Error("WaitTimeout reported success while a send was still blocked")
	}
}

func TestWaitTimeoutReportsCompletion(t *testing.T) {
	n := testNotifier(t, &fakeSender{})

	n.NotifyOperators(testConversation(), store.Message{Body: "hi"}, []string{"op1@example.com"})
	if !n.WaitTimeout(5 * time.Second) {
		t.Error("WaitTimeout gave up on a sender that returns immediately")
	}
}

type blockingSender struct {
	release chan struct{}
	called  bool
}

func (s *blockingSender) Send(to, subject, body string) error {
	<-s.release
	s.called = true
	return nil
}
