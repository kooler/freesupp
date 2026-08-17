package mail

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"math/big"
	"mime"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kooler/freesupp/internal/config"
)

func TestNewSenderPicksImplementation(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{"smtp configured", &config.Config{SMTPHost: "smtp.example.com", SMTPPort: 587, MailFrom: "a@b.c"}, "*mail.SMTPSender"},
		{"no smtp host", &config.Config{}, "*mail.LogSender"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmt.Sprintf("%T", NewSender(tt.cfg, discardLogger())); got != tt.want {
				t.Errorf("NewSender = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLogSenderNeverFails(t *testing.T) {
	var buf bytes.Buffer
	s := &LogSender{Log: slog.New(slog.NewTextHandler(&buf, nil))}

	if err := s.Send("alice@example.com", "hello", "body text"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	for _, want := range []string{"alice@example.com", "hello", "body text"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log missing %q: %s", want, buf.String())
		}
	}
}

func TestBuildMessage(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 30, 0, 0, time.UTC)
	msg := string(buildMessage("FreeSupp <support@example.com>", "alice@example.com", "Hello", "line one\nline two", now))

	headers, body, ok := strings.Cut(msg, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator in:\n%s", msg)
	}
	for _, want := range []string{
		"From: FreeSupp <support@example.com>",
		"To: alice@example.com",
		"Subject: Hello",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Date: " + now.Format(time.RFC1123Z),
	} {
		if !strings.Contains(headers, want) {
			t.Errorf("headers missing %q:\n%s", want, headers)
		}
	}
	if body != "line one\r\nline two" {
		t.Errorf("body = %q, want CRLF line endings", body)
	}
}

func TestBuildMessageEncodesAndSanitizes(t *testing.T) {
	tests := []struct {
		name           string
		subject, to    string
		wantSubjectHas string
		wantNotIn      string
	}{
		{"utf-8 subject is q-encoded", "Grüße", "a@b.c", "=?utf-8?q?", "Grüße"},
		{"header injection is stripped", "hi\r\nBcc: evil@example.com", "a@b.c", "Subject: hi  Bcc:", "\r\nBcc:"},
		{"recipient injection is stripped", "hi", "a@b.c\r\nBcc: evil@example.com", "To: a@b.c  Bcc:", "\r\nBcc:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := string(buildMessage("s@example.com", tt.to, tt.subject, "body", time.Now()))
			headers, _, _ := strings.Cut(msg, "\r\n\r\n")
			if !strings.Contains(headers, tt.wantSubjectHas) {
				t.Errorf("headers missing %q:\n%s", tt.wantSubjectHas, headers)
			}
			if strings.Contains(headers, tt.wantNotIn) {
				t.Errorf("headers unexpectedly contain %q:\n%s", tt.wantNotIn, headers)
			}
		})
	}
}

// A 200-rune non-ASCII visitor name Q-encodes to well over a kilobyte, and a
// header line longer than 998 octets is rejected by strict MTAs.
func TestBuildMessageFoldsLongSubjects(t *testing.T) {
	subject := "New support message from " + strings.Repeat("Ы", 200)
	msg := string(buildMessage("s@example.com", "a@b.c", subject, "body", time.Now()))

	headers, _, _ := strings.Cut(msg, "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("header line is %d octets, want at most 998:\n%s", len(line), line)
		}
	}
	// Unfolding and decoding must give the subject back unchanged.
	var value string
	for _, line := range strings.Split(strings.ReplaceAll(headers, "\r\n ", " "), "\r\n") {
		if v, ok := strings.CutPrefix(line, "Subject: "); ok {
			value = v
		}
	}
	if value == "" {
		t.Fatalf("no Subject header in:\n%s", headers)
	}
	got, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if got != subject {
		t.Errorf("decoded subject = %q, want %q", got, subject)
	}
}

func TestSMTPSenderInvalidAddresses(t *testing.T) {
	tests := []struct {
		name, from, to, wantErr string
	}{
		{"bad from", "not-an-address", "alice@example.com", "invalid MAIL_FROM"},
		{"bad to", "support@example.com", "not-an-address", "invalid recipient"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SMTPSender{Host: "127.0.0.1", Port: 1, From: tt.from}
			err := s.Send(tt.to, "subject", "body")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSMTPSenderDialFailure(t *testing.T) {
	// Port 1 on loopback is not listening.
	s := &SMTPSender{Host: "127.0.0.1", Port: 1, From: "support@example.com"}
	err := s.Send("alice@example.com", "subject", "body")
	if err == nil || !strings.Contains(err.Error(), "dial") {
		t.Fatalf("err = %v, want a dial error", err)
	}
}

// A server that accepts the connection and then goes silent must not park the
// send forever: DialTimeout covers only the TCP handshake, and shutdown waits
// on the goroutine doing the send.
func TestSMTPSenderTimesOutOnASilentServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		<-hold // accepted, but never a greeting
		_ = conn.Close()
	}()

	s := &SMTPSender{
		Host:    "127.0.0.1",
		Port:    ln.Addr().(*net.TCPAddr).Port,
		From:    "support@example.com",
		timeout: 100 * time.Millisecond,
	}
	start := time.Now()
	err = s.Send("alice@example.com", "subject", "body")
	if err == nil || !strings.Contains(err.Error(), "smtp handshake") {
		t.Fatalf("err = %v, want an smtp handshake error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Send took %v, want it bounded by the deadline", elapsed)
	}
}

func TestSMTPSenderDeliversMessage(t *testing.T) {
	srv := startFakeSMTP(t)

	s := &SMTPSender{
		Host: srv.host, Port: srv.port,
		User: "user", Password: "pass",
		From: "FreeSupp <support@example.com>",
		now:  func() time.Time { return time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC) },
	}
	if err := s.Send("alice@example.com", "Hello", "the body"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	tx := srv.transcript()
	for _, want := range []string{
		"MAIL FROM:<support@example.com>", // display name stripped from the envelope
		"RCPT TO:<alice@example.com>",
		"AUTH PLAIN",
		"Subject: Hello",
		"the body",
		"QUIT",
	} {
		if !strings.Contains(tx, want) {
			t.Errorf("transcript missing %q:\n%s", want, tx)
		}
	}
}

// When the server advertises STARTTLS the session must actually be upgraded:
// credentials and the message body may not cross the wire in cleartext.
func TestSMTPSenderUpgradesToSTARTTLS(t *testing.T) {
	srv := startFakeSMTP(t, withSTARTTLS)

	s := &SMTPSender{
		Host: srv.host, Port: srv.port,
		User: "user", Password: "pass",
		From:    "support@example.com",
		rootCAs: srv.roots,
	}
	if err := s.Send("alice@example.com", "Hello", "the body"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.Contains(srv.transcript(), "STARTTLS") {
		t.Fatalf("transcript has no STARTTLS command:\n%s", srv.transcript())
	}
	encrypted := srv.encryptedTranscript()
	if encrypted == nil {
		t.Fatal("TLS handshake never completed")
	}
	joined := strings.Join(encrypted, "\n")
	for _, want := range []string{"AUTH PLAIN", "MAIL FROM:<support@example.com>", "the body"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q was not sent over TLS; encrypted transcript:\n%s", want, joined)
		}
	}
}

// A server that offers STARTTLS and then refuses it is a failed delivery, not a
// silent downgrade to cleartext.
func TestSMTPSenderFailsWhenSTARTTLSIsRefused(t *testing.T) {
	srv := startFakeSMTP(t, withFailingTLS)

	s := &SMTPSender{Host: srv.host, Port: srv.port, From: "support@example.com", rootCAs: srv.roots}
	err := s.Send("alice@example.com", "Hello", "body")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want a STARTTLS error", err)
	}
	if srv.encryptedTranscript() != nil {
		t.Error("handshake completed despite the server refusing STARTTLS")
	}
}

// Port 465 speaks TLS from the first byte, with no STARTTLS command at all.
func TestSMTPSenderImplicitTLS(t *testing.T) {
	srv := startFakeSMTP(t, withImplicitTLS)

	s := &SMTPSender{
		Host: srv.host, Port: srv.port,
		From:             "support@example.com",
		rootCAs:          srv.roots,
		forceImplicitTLS: true, // stands in for Port == 465, which we cannot bind
	}
	if err := s.Send("alice@example.com", "Hello", "the body"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	tx := srv.transcript()
	if strings.Contains(tx, "STARTTLS") {
		t.Errorf("implicit TLS session issued STARTTLS:\n%s", tx)
	}
	if !strings.Contains(tx, "the body") {
		t.Errorf("transcript missing the message body:\n%s", tx)
	}
}

func TestImplicitTLSPortSelection(t *testing.T) {
	if !(&SMTPSender{Port: implicitTLSPort}).implicitTLS() {
		t.Errorf("port %d should use implicit TLS", implicitTLSPort)
	}
	if (&SMTPSender{Port: 587}).implicitTLS() {
		t.Error("port 587 should use STARTTLS, not implicit TLS")
	}
}

func TestSMTPSenderRejectedRecipient(t *testing.T) {
	srv := startFakeSMTP(t)
	srv.rejectRcpt = true

	s := &SMTPSender{Host: srv.host, Port: srv.port, From: "support@example.com"}
	err := s.Send("alice@example.com", "Hello", "body")
	if err == nil || !strings.Contains(err.Error(), "RCPT TO") {
		t.Fatalf("err = %v, want a RCPT TO error", err)
	}
}

// fakeSMTP is a one-connection SMTP server good enough to drive net/smtp.
type fakeSMTP struct {
	host       string
	port       int
	rejectRcpt bool

	// offerTLS advertises STARTTLS; failTLS advertises it but refuses the
	// upgrade. implicitTLS wraps the connection in TLS before the greeting.
	offerTLS    bool
	failTLS     bool
	implicitTLS bool
	tlsConfig   *tls.Config
	roots       *x509.CertPool

	mu       sync.Mutex
	lines    []string
	tlsLines []string // commands received after the handshake
	done     chan struct{}
}

type fakeSMTPOption func(*fakeSMTP)

func withSTARTTLS(f *fakeSMTP)    { f.offerTLS = true }
func withFailingTLS(f *fakeSMTP)  { f.offerTLS, f.failTLS = true, true }
func withImplicitTLS(f *fakeSMTP) { f.implicitTLS = true }

func startFakeSMTP(t *testing.T, opts ...fakeSMTPOption) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	srv := &fakeSMTP{host: "127.0.0.1", port: addr.Port, done: make(chan struct{})}
	for _, o := range opts {
		o(srv)
	}
	if srv.offerTLS || srv.implicitTLS {
		srv.tlsConfig, srv.roots = selfSignedTLS(t, srv.host)
	}

	go func() {
		defer close(srv.done)
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		if srv.implicitTLS {
			tc := tls.Server(conn, srv.tlsConfig)
			if err := tc.Handshake(); err != nil {
				return
			}
			srv.inTLS()
			defer tc.Close()
			srv.serve(tc)
			return
		}
		srv.serve(conn)
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-srv.done:
		case <-time.After(5 * time.Second):
			t.Error("fake SMTP server did not stop")
		}
	})
	return srv
}

// serve greets the client and then runs the command loop.
func (f *fakeSMTP) serve(conn net.Conn) {
	f.commands(conn, true)
}

// commands runs the SMTP command loop, optionally sending the greeting first.
// After STARTTLS it recurses on the TLS connection without greeting again.
func (f *fakeSMTP) commands(conn net.Conn, greet bool) {
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}

	if greet {
		write("220 fake ESMTP")
	}
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.record(line)

		if inData {
			if line == "." {
				inData = false
				write("250 2.0.0 Ok")
			}
			continue
		}

		verb, _, _ := strings.Cut(strings.ToUpper(line), " ")
		switch verb {
		case "EHLO":
			write("250-fake")
			if f.offerTLS {
				write("250-STARTTLS")
			}
			write("250 AUTH PLAIN LOGIN")
		case "STARTTLS":
			if f.failTLS {
				write("454 4.7.0 TLS not available right now")
				continue
			}
			write("220 2.0.0 Ready to start TLS")
			tc := tls.Server(conn, f.tlsConfig)
			if err := tc.Handshake(); err != nil {
				return
			}
			f.inTLS()
			defer tc.Close()
			// Everything after the handshake runs on the encrypted connection.
			f.commands(tc, false)
			return
		case "HELO":
			write("250 fake")
		case "AUTH":
			write("235 2.7.0 Authentication successful")
		case "MAIL":
			write("250 2.1.0 Ok")
		case "RCPT":
			if f.rejectRcpt {
				write("550 5.1.1 No such user")
				continue
			}
			write("250 2.1.5 Ok")
		case "DATA":
			inData = true
			write("354 End data with <CR><LF>.<CR><LF>")
		case "QUIT":
			write("221 2.0.0 Bye")
			return
		default:
			write("250 2.0.0 Ok")
		}
	}
}

func (f *fakeSMTP) record(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
	if f.tlsLines != nil {
		f.tlsLines = append(f.tlsLines, line)
	}
}

// inTLS starts a second transcript covering only post-handshake traffic.
func (f *fakeSMTP) inTLS() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tlsLines = []string{}
}

func (f *fakeSMTP) transcript() string {
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.lines, "\n")
}

// encryptedTranscript is what the server received after the TLS handshake; nil
// means the handshake never happened.
func (f *fakeSMTP) encryptedTranscript() []string {
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tlsLines
}

// selfSignedTLS returns a server config for host plus the pool that trusts it.
func selfSignedTLS(t *testing.T, host string) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP(host)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)

	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}},
		MinVersion:   tls.VersionTLS12,
	}, roots
}
