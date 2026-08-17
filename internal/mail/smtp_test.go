package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewSender_ParsesCourierURIs(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantAddr    string
		wantHost    string
		wantUser    string
		wantPass    string
		wantTLS     bool
		wantSTART   bool
		wantSkipTLS bool
	}{
		{
			name:      "plain smtp defaults to STARTTLS and port 25",
			uri:       "smtp://relay.example.com/",
			wantAddr:  "relay.example.com:25",
			wantHost:  "relay.example.com",
			wantSTART: true,
		},
		{
			name:     "disable_starttls turns it off, which is what MailHog needs",
			uri:      "smtp://mailhog:1025/?disable_starttls=true",
			wantAddr: "mailhog:1025",
			wantHost: "mailhog",
		},
		{
			name:     "smtps is implicit TLS on 465",
			uri:      "smtps://relay.example.com/",
			wantAddr: "relay.example.com:465",
			wantHost: "relay.example.com",
			wantTLS:  true,
		},
		{
			name:      "credentials",
			uri:       "smtp://bell:s3cret@relay.example.com:587/",
			wantAddr:  "relay.example.com:587",
			wantHost:  "relay.example.com",
			wantUser:  "bell",
			wantPass:  "s3cret",
			wantSTART: true,
		},
		{
			name:      "percent-encoded credentials are decoded",
			uri:       "smtp://bell%40town:p%40ss%20word@relay.example.com:587/",
			wantAddr:  "relay.example.com:587",
			wantHost:  "relay.example.com",
			wantUser:  "bell@town",
			wantPass:  "p@ss word",
			wantSTART: true,
		},
		{
			name:        "skip_ssl_verify is honoured, as it is for the Kratos courier",
			uri:         "smtps://relay.example.com:465/?skip_ssl_verify=true",
			wantAddr:    "relay.example.com:465",
			wantHost:    "relay.example.com",
			wantTLS:     true,
			wantSkipTLS: true,
		},
		{
			name:      "unrecognised parameters are ignored, since this URI is shared with Kratos",
			uri:       "smtp://relay.example.com:587/?something_kratos_understands=1",
			wantAddr:  "relay.example.com:587",
			wantHost:  "relay.example.com",
			wantSTART: true,
		},
		{
			name:     "an explicit false leaves STARTTLS off only when it says true",
			uri:      "smtp://mailhog:1025/?disable_starttls=false",
			wantAddr: "mailhog:1025",
			wantHost: "mailhog",
			// disable_starttls=false means "do not disable", so STARTTLS stays.
			wantSTART: true,
		},
		{
			name:     "1 counts as true",
			uri:      "smtp://mailhog:1025/?disable_starttls=1",
			wantAddr: "mailhog:1025",
			wantHost: "mailhog",
		},
		{
			name:      "an IPv6 literal keeps its brackets",
			uri:       "smtp://[2001:db8::1]:587/",
			wantAddr:  "[2001:db8::1]:587",
			wantHost:  "2001:db8::1",
			wantSTART: true,
		},
		{
			name:      "surrounding whitespace is trimmed",
			uri:       "  smtp://relay.example.com:587/  ",
			wantAddr:  "relay.example.com:587",
			wantHost:  "relay.example.com",
			wantSTART: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender, err := NewSender(tt.uri, "bell@example.com")
			if err != nil {
				t.Fatalf("NewSender(%q) unexpected error: %v", tt.uri, err)
			}
			if sender.addr != tt.wantAddr {
				t.Errorf("addr = %q, want %q", sender.addr, tt.wantAddr)
			}
			if sender.host != tt.wantHost {
				t.Errorf("host = %q, want %q", sender.host, tt.wantHost)
			}
			if sender.username != tt.wantUser {
				t.Errorf("username = %q, want %q", sender.username, tt.wantUser)
			}
			if sender.password != tt.wantPass {
				t.Errorf("password = %q, want %q", sender.password, tt.wantPass)
			}
			if sender.implicitTLS != tt.wantTLS {
				t.Errorf("implicitTLS = %v, want %v", sender.implicitTLS, tt.wantTLS)
			}
			if sender.startTLS != tt.wantSTART {
				t.Errorf("startTLS = %v, want %v", sender.startTLS, tt.wantSTART)
			}
			if sender.skipVerify != tt.wantSkipTLS {
				t.Errorf("skipVerify = %v, want %v", sender.skipVerify, tt.wantSkipTLS)
			}
			if sender.From() != "bell@example.com" {
				t.Errorf("From() = %q", sender.From())
			}
		})
	}
}

func TestNewSender_RejectsUnusableConfiguration(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		from string
	}{
		// Empty is not "disabled" here. Sending is switched off by never
		// constructing a sender, so that the off state is visible at wiring
		// time rather than hidden in a value that accepts mail and drops it.
		{name: "empty URI", uri: "", from: "bell@example.com"},
		{name: "whitespace URI", uri: "   ", from: "bell@example.com"},
		{name: "empty from address", uri: "smtp://relay.example.com/", from: ""},
		{name: "a scheme that is not SMTP", uri: "https://relay.example.com/", from: "bell@example.com"},
		{name: "no scheme at all", uri: "relay.example.com:25", from: "bell@example.com"},
		{name: "no host", uri: "smtp:///?disable_starttls=true", from: "bell@example.com"},
		{name: "unparseable", uri: "smtp://%zz/", from: "bell@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSender(tt.uri, tt.from); err == nil {
				t.Errorf("NewSender(%q, %q) error = nil, want a refusal", tt.uri, tt.from)
			}
		})
	}
}

func TestBuildMessage(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	msg := Message{To: "newcomer@example.com", Subject: "Ana invited you to join Bellville", Body: "Hello\nthere\n"}

	got := buildMessage("bell@example.com", msg, now)

	for _, want := range []string{
		"From: bell@example.com\r\n",
		"To: newcomer@example.com\r\n",
		"Subject: Ana invited you to join Bellville\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\n\r\nHello\nthere\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message does not contain %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "Date: ") {
		t.Error("message has no Date header")
	}
}

// A town name or a resident's name may be in any script. Sent raw those bytes
// are undefined on the wire and arrive as mojibake, so the subject is RFC 2047
// encoded — and a plain-ASCII subject has to come through untouched, which the
// test above covers.
func TestBuildMessage_EncodesANonASCIISubject(t *testing.T) {
	got := buildMessage("bell@example.com",
		Message{To: "a@example.com", Subject: "Ana invited you to join Ĺittle Bellville"},
		time.Now())

	if strings.Contains(got, "Ĺittle") {
		t.Error("the subject was sent as raw UTF-8 rather than encoded")
	}
	if !strings.Contains(got, "Subject: =?utf-8?") {
		t.Errorf("subject is not RFC 2047 encoded:\n%s", got)
	}
}

// --- delivery, against a fake relay ---

// fakeSMTP is a minimal SMTP server: enough of the conversation for net/smtp to
// complete a delivery, and a record of what it was handed.
//
// A real listener rather than an interface stub, because what is worth testing
// here is the conversation — that the envelope carries the configured sender,
// that the recipient is the invitee, that the body is dot-terminated correctly
// — and none of that is observable above net/smtp.
type fakeSMTP struct {
	addr string

	mu       sync.Mutex
	from     string
	rcpt     []string
	data     string
	sessions int

	// failAt, when set, is the command the relay refuses, standing in for a
	// relay that rejects the recipient or the message.
	failAt string
}

func startFakeSMTP(t *testing.T, failAt string) *fakeSMTP {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	s := &fakeSMTP{addr: ln.Addr().String(), failAt: failAt}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	s.mu.Lock()
	s.sessions++
	s.mu.Unlock()

	r := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

	write("220 fake.test ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))

		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			// No STARTTLS advertised: this relay is the plaintext MailHog case.
			write("250-fake.test")
			write("250 SIZE 35882577")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			if s.failAt == "MAIL" {
				write("550 sender rejected")
				continue
			}
			s.mu.Lock()
			s.from = addrArg(line)
			s.mu.Unlock()
			write("250 OK")
		case strings.HasPrefix(cmd, "RCPT TO"):
			if s.failAt == "RCPT" {
				write("550 no such recipient")
				continue
			}
			s.mu.Lock()
			s.rcpt = append(s.rcpt, addrArg(line))
			s.mu.Unlock()
			write("250 OK")
		case cmd == "DATA":
			if s.failAt == "DATA" {
				write("554 message refused")
				continue
			}
			write("354 send it")
			var body strings.Builder
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				body.WriteString(dataLine)
			}
			s.mu.Lock()
			s.data = body.String()
			s.mu.Unlock()
			write("250 queued")
		case cmd == "QUIT":
			write("221 bye")
			return
		default:
			write("250 OK")
		}
	}
}

// addrArg pulls the address out of "MAIL FROM:<a@b>" / "RCPT TO:<a@b>".
func addrArg(line string) string {
	_, rest, found := strings.Cut(line, ":")
	if !found {
		return ""
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, "<")
	if i := strings.Index(rest, ">"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func (s *fakeSMTP) received() (from string, rcpt []string, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from, append([]string(nil), s.rcpt...), s.data
}

func senderFor(t *testing.T, addr string) *SMTPSender {
	t.Helper()
	sender, err := NewSender("smtp://"+addr+"/?disable_starttls=true", "bell@example.com")
	if err != nil {
		t.Fatalf("NewSender(): %v", err)
	}
	sender.SetTimeout(3 * time.Second)
	return sender
}

func TestSMTPSender_Send_DeliversTheMessage(t *testing.T) {
	relay := startFakeSMTP(t, "")
	sender := senderFor(t, relay.addr)

	err := sender.Send(context.Background(), Message{
		To:      "newcomer@example.com",
		Subject: "Ana invited you to join Bellville",
		Body:    "Follow the link.\n",
	})
	if err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	from, rcpt, data := relay.received()
	if from != "bell@example.com" {
		t.Errorf("MAIL FROM = %q, want the configured sender", from)
	}
	if len(rcpt) != 1 || rcpt[0] != "newcomer@example.com" {
		t.Errorf("RCPT TO = %v, want the invitee alone", rcpt)
	}
	for _, want := range []string{"Subject: Ana invited you to join Bellville", "Follow the link."} {
		if !strings.Contains(data, want) {
			t.Errorf("delivered message does not contain %q:\n%s", want, data)
		}
	}
}

// A body line of "." would otherwise end the message early. net/smtp's
// dot-writer handles the stuffing; this pins that we go through it.
func TestSMTPSender_Send_DoesNotLetABodyEndTheMessageEarly(t *testing.T) {
	relay := startFakeSMTP(t, "")
	sender := senderFor(t, relay.addr)

	err := sender.Send(context.Background(), Message{
		To: "newcomer@example.com", Subject: "Test", Body: "before\n.\nafter\n",
	})
	if err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	_, _, data := relay.received()
	if !strings.Contains(data, "after") {
		t.Errorf("the message was truncated at the lone dot:\n%s", data)
	}
}

func TestSMTPSender_Send_ReportsRelayRefusals(t *testing.T) {
	for _, failAt := range []string{"MAIL", "RCPT", "DATA"} {
		t.Run(failAt, func(t *testing.T) {
			relay := startFakeSMTP(t, failAt)
			sender := senderFor(t, relay.addr)

			err := sender.Send(context.Background(), Message{To: "newcomer@example.com", Subject: "Test", Body: "hello"})
			if err == nil {
				t.Fatalf("Send() error = nil, want the %s refusal reported", failAt)
			}
		})
	}
}

func TestSMTPSender_Send_ReportsAnUnreachableRelay(t *testing.T) {
	// Port 1 on the loopback: nothing listens there, so the dial fails fast.
	sender, err := NewSender("smtp://127.0.0.1:1/?disable_starttls=true", "bell@example.com")
	if err != nil {
		t.Fatalf("NewSender(): %v", err)
	}
	sender.SetTimeout(2 * time.Second)

	if err := sender.Send(context.Background(), Message{To: "a@example.com", Subject: "s", Body: "b"}); err == nil {
		t.Fatal("Send() error = nil, want a dial failure")
	}
}

// STARTTLS is required rather than opportunistic: the URI asked for it by not
// disabling it, and an invitation carries a token that admits its holder to the
// town. Falling back to plaintext is not a degradation the operator agreed to.
func TestSMTPSender_Send_RefusesToFallBackToPlaintext(t *testing.T) {
	relay := startFakeSMTP(t, "")
	sender, err := NewSender("smtp://"+relay.addr+"/", "bell@example.com")
	if err != nil {
		t.Fatalf("NewSender(): %v", err)
	}
	sender.SetTimeout(3 * time.Second)

	err = sender.Send(context.Background(), Message{To: "a@example.com", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("Send() error = nil, want a refusal: the relay offered no STARTTLS")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("error = %v, want it to name STARTTLS", err)
	}
	if _, _, data := relay.received(); data != "" {
		t.Error("the message was delivered in the clear")
	}
}

func TestSMTPSender_Send_RejectsAMessageWithNoRecipient(t *testing.T) {
	relay := startFakeSMTP(t, "")
	sender := senderFor(t, relay.addr)

	if err := sender.Send(context.Background(), Message{Subject: "s", Body: "b"}); err == nil {
		t.Fatal("Send() error = nil, want a refusal")
	}
	if relay.sessions != 0 {
		t.Error("the relay was dialled for a message with nobody to send it to")
	}
}

func TestSMTPSender_Send_HonoursACancelledContext(t *testing.T) {
	relay := startFakeSMTP(t, "")
	sender := senderFor(t, relay.addr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sender.Send(ctx, Message{To: "a@example.com", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("Send() error = nil, want the cancellation reported")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
}

// The interface is what the invite service depends on, so the concrete sender
// has to satisfy it.
var _ Sender = (*SMTPSender)(nil)
