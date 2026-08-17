package mail

import (
	"log/slog"
	"sync"
	"time"

	"github.com/kooler/freesupp/internal/config"
	"github.com/kooler/freesupp/internal/store"
)

// Notifier turns store events into emails. Sending happens on a background
// goroutine so a slow or broken SMTP server never blocks an HTTP request;
// failures are logged and otherwise ignored.
type Notifier struct {
	sender  Sender
	log     *slog.Logger
	baseURL string

	wg sync.WaitGroup
}

// NewNotifier builds a Notifier from config.
func NewNotifier(cfg *config.Config, sender Sender, log *slog.Logger) *Notifier {
	if log == nil {
		log = slog.Default()
	}
	return &Notifier{
		sender:  sender,
		log:     log,
		baseURL: cfg.BaseURL,
	}
}

// NotifyOperators emails the given operators about a visitor message. The
// recipient list comes from the caller because operators live in the database
// and may change at any time.
func (n *Notifier) NotifyOperators(conv store.Conversation, msg store.Message, recipients []string) {
	n.async(func() { n.notifyOperators(conv, msg, recipients) })
}

// NotifyVisitor emails the visitor an operator's reply plus their magic link.
func (n *Notifier) NotifyVisitor(conv store.Conversation, msg store.Message) {
	n.async(func() { n.notifyVisitor(conv, msg) })
}

// NotifyVisitorReceipt emails a new thread's magic link to the visitor. The
// link is delivered to the mailbox rather than handed back to whoever posted
// the form, which is the only thing that ties the token to the address owner.
func (n *Notifier) NotifyVisitorReceipt(conv store.Conversation) {
	n.async(func() { n.notifyVisitorReceipt(conv) })
}

// Wait blocks until all in-flight notifications finish. Used on shutdown and
// in tests.
func (n *Notifier) Wait() { n.wg.Wait() }

// WaitTimeout is Wait with an upper bound, reporting whether everything
// finished. Shutdown uses it so a wedged SMTP server cannot keep the process
// alive: an abandoned send only holds its own socket, which the exit releases.
func (n *Notifier) WaitTimeout(d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.wg.Wait()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func (n *Notifier) async(fn func()) {
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		fn()
	}()
}

func (n *Notifier) notifyOperators(conv store.Conversation, msg store.Message, recipients []string) {
	if len(recipients) == 0 {
		n.log.Warn("no operators to notify, dropping notification", "conversation_id", conv.ID)
		return
	}
	subject, body, err := renderOperator(n.baseURL, conv, msg)
	if err != nil {
		n.log.Error("rendering operator notification", "conversation_id", conv.ID, "err", err)
		return
	}
	for _, op := range recipients {
		n.send(op, subject, body, "operator_notification", conv.ID)
	}
}

func (n *Notifier) notifyVisitor(conv store.Conversation, msg store.Message) {
	subject, body, err := renderVisitor(n.baseURL, conv, msg)
	if err != nil {
		n.log.Error("rendering visitor notification", "conversation_id", conv.ID, "err", err)
		return
	}
	n.send(conv.VisitorEmail, subject, body, "visitor_reply", conv.ID)
}

func (n *Notifier) notifyVisitorReceipt(conv store.Conversation) {
	subject, body, err := renderReceipt(n.baseURL, conv)
	if err != nil {
		n.log.Error("rendering visitor receipt", "conversation_id", conv.ID, "err", err)
		return
	}
	n.send(conv.VisitorEmail, subject, body, "visitor_receipt", conv.ID)
}

func (n *Notifier) send(to, subject, body, kind string, convID int64) {
	if err := n.sender.Send(to, subject, body); err != nil {
		n.log.Error("sending email", "kind", kind, "to", to, "conversation_id", convID, "err", err)
		return
	}
	n.log.Info("email sent", "kind", kind, "to", to, "conversation_id", convID)
}
