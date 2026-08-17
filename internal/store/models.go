package store

import "time"

// Conversation status values.
const (
	StatusOpen     = "open"
	StatusArchived = "archived"
)

// Message sender values.
const (
	SenderVisitor  = "visitor"
	SenderOperator = "operator"
)

// Conversation is one visitor thread.
type Conversation struct {
	ID            int64
	VisitorEmail  string
	VisitorName   string
	Token         string
	Status        string
	Unread        bool
	CreatedAt     time.Time
	LastMessageAt time.Time
}

// Message is a single entry in a conversation.
type Message struct {
	ID             int64
	ConversationID int64
	Sender         string
	OperatorEmail  string // empty for visitor messages
	Body           string
	CreatedAt      time.Time
}

// timeLayout is the on-disk timestamp format: sortable as text, UTC.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }
