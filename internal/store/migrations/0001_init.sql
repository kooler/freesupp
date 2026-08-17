CREATE TABLE conversations (
  id              INTEGER PRIMARY KEY,
  visitor_email   TEXT NOT NULL,
  visitor_name    TEXT NOT NULL DEFAULT '',
  token           TEXT NOT NULL UNIQUE,
  status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','archived')),
  unread          INTEGER NOT NULL DEFAULT 1,
  created_at      TEXT NOT NULL,
  last_message_at TEXT NOT NULL
);

CREATE TABLE messages (
  id              INTEGER PRIMARY KEY,
  conversation_id INTEGER NOT NULL REFERENCES conversations(id),
  sender          TEXT NOT NULL CHECK (sender IN ('visitor','operator')),
  operator_email  TEXT,
  body            TEXT NOT NULL,
  created_at      TEXT NOT NULL
);

CREATE INDEX idx_conversations_status_last_message_at ON conversations (status, last_message_at);
CREATE INDEX idx_conversations_visitor_email_status ON conversations (visitor_email, status);
CREATE INDEX idx_messages_conversation_id ON messages (conversation_id);
