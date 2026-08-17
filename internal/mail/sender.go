// Package mail renders and delivers FreeSupp's outbound notification emails.
package mail

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"mime"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/kooler/freesupp/internal/config"
)

// implicitTLSPort speaks TLS from the first byte; every other port uses STARTTLS.
const implicitTLSPort = 465

const dialTimeout = 15 * time.Second

// sendTimeout bounds the whole SMTP conversation. DialTimeout only covers the
// TCP handshake, so without a deadline a server that accepts the connection and
// then stops answering parks a goroutine and a socket forever — and shutdown
// waits on exactly those goroutines.
const sendTimeout = 60 * time.Second

// Sender delivers a single plain-text email.
type Sender interface {
	Send(to, subject, textBody string) error
}

// NewSender returns an SMTP sender when SMTP is configured, otherwise a
// logging-only sender so the app still runs in local development.
func NewSender(cfg *config.Config, log *slog.Logger) Sender {
	if !cfg.SMTPConfigured() {
		return &LogSender{Log: log}
	}
	return &SMTPSender{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		User:     cfg.SMTPUser,
		Password: cfg.SMTPPassword,
		From:     cfg.MailFrom,
	}
}

// LogSender writes emails to the log instead of delivering them (dev mode).
type LogSender struct {
	Log *slog.Logger
}

func (s *LogSender) Send(to, subject, textBody string) error {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	log.Info("email not sent (SMTP not configured)", "to", to, "subject", subject, "body", textBody)
	return nil
}

// SMTPSender delivers email over SMTP.
type SMTPSender struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string

	// now is overridable in tests; defaults to time.Now.
	now func() time.Time
	// rootCAs overrides the TLS trust store and forceImplicitTLS overrides the
	// port-465 rule. Both exist so tests can drive a self-signed fake server.
	rootCAs          *x509.CertPool
	forceImplicitTLS bool
	// timeout overrides sendTimeout so tests need not wait a minute.
	timeout time.Duration
}

func (s *SMTPSender) sendDeadline() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return sendTimeout
}

// implicitTLS reports whether the connection speaks TLS from the first byte.
func (s *SMTPSender) implicitTLS() bool {
	return s.forceImplicitTLS || s.Port == implicitTLSPort
}

func (s *SMTPSender) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12, RootCAs: s.rootCAs}
}

func (s *SMTPSender) Send(to, subject, textBody string) error {
	from, err := address(s.From)
	if err != nil {
		return fmt.Errorf("mail: invalid MAIL_FROM %q: %w", s.From, err)
	}
	rcpt, err := address(to)
	if err != nil {
		return fmt.Errorf("mail: invalid recipient %q: %w", to, err)
	}

	msg := buildMessage(s.From, to, subject, textBody, s.timestamp())

	c, err := s.connect()
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if err := s.startTLS(c); err != nil {
		return err
	}
	if err := s.auth(c); err != nil {
		return err
	}

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(rcpt); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("mail: writing body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: closing body: %w", err)
	}
	return c.Quit()
}

func (s *SMTPSender) connect() (*smtp.Client, error) {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("mail: dial %s: %w", addr, err)
	}
	if err := conn.SetDeadline(time.Now().Add(s.sendDeadline())); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mail: setting deadline on %s: %w", addr, err)
	}
	if s.implicitTLS() {
		conn = tls.Client(conn, s.tlsConfig())
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mail: smtp handshake with %s: %w", addr, err)
	}
	return c, nil
}

func (s *SMTPSender) startTLS(c *smtp.Client) error {
	if s.implicitTLS() {
		return nil
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return nil
	}
	if err := c.StartTLS(s.tlsConfig()); err != nil {
		return fmt.Errorf("mail: STARTTLS: %w", err)
	}
	return nil
}

func (s *SMTPSender) auth(c *smtp.Client) error {
	if s.User == "" {
		return nil
	}
	if ok, _ := c.Extension("AUTH"); !ok {
		return nil
	}
	if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
		return fmt.Errorf("mail: authenticating as %s: %w", s.User, err)
	}
	return nil
}

func (s *SMTPSender) timestamp() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// buildMessage renders an RFC 5322 plain-text UTF-8 message.
func buildMessage(from, to, subject, body string, now time.Time) []byte {
	var b strings.Builder
	writeHeader(&b, "From", from)
	writeHeader(&b, "To", to)
	// A visitor name of 200 non-ASCII runes Q-encodes to well over a kilobyte,
	// and mime.QEncoding.Encode never folds, so the subject is wrapped here:
	// RFC 5322 caps a header line at 998 octets and MTAs do reject longer ones.
	writeFoldedHeader(&b, "Subject", mime.QEncoding.Encode("utf-8", sanitizeHeader(subject)))
	writeHeader(&b, "Date", now.Format(time.RFC1123Z))
	writeHeader(&b, "MIME-Version", "1.0")
	writeHeader(&b, "Content-Type", "text/plain; charset=utf-8")
	writeHeader(&b, "Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	b.WriteString(toCRLF(body))
	return []byte(b.String())
}

func writeHeader(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(": ")
	b.WriteString(sanitizeHeader(value))
	b.WriteString("\r\n")
}

// maxHeaderLine is the RFC 5322 recommended line length; folding happens at
// spaces, which both encoded-word and plain subjects survive because unfolding
// puts the space back.
const maxHeaderLine = 78

func writeFoldedHeader(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(": ")
	line := len(name) + 2
	for i, word := range strings.Split(sanitizeHeader(value), " ") {
		switch {
		case i == 0:
		case line+1+len(word) > maxHeaderLine:
			b.WriteString("\r\n ")
			line = 1
		default:
			b.WriteString(" ")
			line++
		}
		b.WriteString(word)
		line += len(word)
	}
	b.WriteString("\r\n")
}

// sanitizeHeader strips CR/LF so untrusted values cannot inject headers.
func sanitizeHeader(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
}

func toCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// address extracts the bare envelope address from a possibly display-name form.
func address(s string) (string, error) {
	a, err := netmail.ParseAddress(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}
	return a.Address, nil
}
