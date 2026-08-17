package mail

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/kooler/freesupp/internal/store"
)

// SupportLabel is the only sender identity visitors ever see; operator
// addresses are never exposed.
const SupportLabel = "Support"

// operatorData feeds the "new support request" template.
type operatorData struct {
	VisitorLabel string
	VisitorEmail string
	VisitorName  string
	Body         string
	InboxURL     string
}

// visitorData feeds the "reply from support" template.
type visitorData struct {
	VisitorName string
	Body        string
	ThreadURL   string
	Support     string
}

// receiptData feeds the "we got your message" confirmation.
type receiptData struct {
	VisitorName string
	ThreadURL   string
}

var (
	operatorTmpl = template.Must(template.New("operator").Parse(
		`{{.VisitorLabel}} sent a support request:

{{.Body}}

--
From: {{.VisitorEmail}}
Open in the inbox: {{.InboxURL}}
`))

	visitorTmpl = template.Must(template.New("visitor").Parse(
		`{{if .VisitorName}}Hi {{.VisitorName}},

{{end}}{{.Support}} replied to your message:

{{.Body}}

--
To continue this conversation, open your thread:
{{.ThreadURL}}

Please do not reply to this email; use the link above instead.
`))

	receiptTmpl = template.Must(template.New("receipt").Parse(
		`{{if .VisitorName}}Hi {{.VisitorName}},

{{end}}Thanks for getting in touch — we have your message and will reply to this
address.

You can follow the conversation here:
{{.ThreadURL}}

Please do not reply to this email; use the link above instead.
`))
)

// renderOperator builds the subject and body of the operator notification.
func renderOperator(baseURL string, conv store.Conversation, msg store.Message) (subject, body string, err error) {
	body, err = render(operatorTmpl, operatorData{
		VisitorLabel: VisitorLabel(conv),
		VisitorEmail: conv.VisitorEmail,
		VisitorName:  conv.VisitorName,
		Body:         strings.TrimSpace(msg.Body),
		InboxURL:     InboxURL(baseURL, conv.ID),
	})
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("New support message from %s", VisitorLabel(conv)), body, nil
}

// renderVisitor builds the subject and body of the reply notification.
func renderVisitor(baseURL string, conv store.Conversation, msg store.Message) (subject, body string, err error) {
	body, err = render(visitorTmpl, visitorData{
		VisitorName: strings.TrimSpace(conv.VisitorName),
		Body:        strings.TrimSpace(msg.Body),
		ThreadURL:   ThreadURL(baseURL, conv.Token),
		Support:     SupportLabel,
	})
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("Re: your support request (#%d)", conv.ID), body, nil
}

// renderReceipt builds the confirmation that carries the visitor's magic link.
// It deliberately leaves the submitted text out: the widget never proves the
// submitter owns the address, so this mail must not relay a stranger's words
// into someone's inbox.
func renderReceipt(baseURL string, conv store.Conversation) (subject, body string, err error) {
	body, err = render(receiptTmpl, receiptData{
		VisitorName: strings.TrimSpace(conv.VisitorName),
		ThreadURL:   ThreadURL(baseURL, conv.Token),
	})
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("We got your support request (#%d)", conv.ID), body, nil
}

func render(t *template.Template, data any) (string, error) {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("mail: rendering %s template: %w", t.Name(), err)
	}
	return b.String(), nil
}

// VisitorLabel is how a visitor is referred to in operator-facing copy.
func VisitorLabel(conv store.Conversation) string {
	if name := strings.TrimSpace(conv.VisitorName); name != "" {
		return fmt.Sprintf("%s (%s)", name, conv.VisitorEmail)
	}
	return conv.VisitorEmail
}

// ThreadURL is the visitor's magic link.
func ThreadURL(baseURL, token string) string {
	return fmt.Sprintf("%s/t/%s", strings.TrimRight(baseURL, "/"), token)
}

// InboxURL points operators at one conversation in the inbox SPA.
func InboxURL(baseURL string, convID int64) string {
	return fmt.Sprintf("%s/conversations/%d", strings.TrimRight(baseURL, "/"), convID)
}
