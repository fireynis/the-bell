package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds one delivery, dial included.
//
// Invitation mail is sent synchronously inside the request that creates the
// invitation, so this is also how long a member can be left waiting on a relay
// that has stopped answering. Ten seconds is comfortably more than a healthy
// relay on the same network needs, and short enough that a dead one produces an
// invitation with email_sent:false rather than a request that appears to hang.
const DefaultTimeout = 10 * time.Second

// SMTPSender delivers mail through one relay, described by a courier-style
// connection URI.
type SMTPSender struct {
	addr     string
	host     string
	from     string
	username string
	password string

	// implicitTLS is smtps://: TLS is established before the SMTP greeting.
	implicitTLS bool
	// startTLS upgrades a plain connection with STARTTLS, and is required
	// rather than opportunistic — see Send.
	startTLS bool
	// skipVerify disables certificate verification. Honoured because the same
	// URI configures Kratos's courier, where an operator with a self-signed
	// relay has already had to set it; silently ignoring it here would send
	// their invitations into a TLS error they had already solved once.
	skipVerify bool

	timeout time.Duration
}

// NewSender parses a courier-style connection URI and returns a sender for it.
//
//	smtp://user:pass@host:25/?disable_starttls=true
//	smtps://user:pass@host:465/
//	smtp://mailhog:1025/?disable_starttls=true
//
// The shape is Kratos's COURIER_SMTP_CONNECTION_URI, deliberately: both composes
// feed this from the same variable, so one relay setting serves the courier and
// the invitation mail. Credentials are optional — MailHog and most in-cluster
// relays have none.
//
// An empty URI is an error rather than a disabled sender. Sending is switched
// off by not constructing a sender at all (see app.Build), which makes the
// off state visible at wiring time instead of hiding inside a value that
// accepts messages and drops them.
//
// Unrecognised query parameters are ignored, because this URI is shared with
// Kratos and may legitimately carry settings that mean nothing here. The two
// that are honoured are disable_starttls and skip_ssl_verify.
func NewSender(connectionURI, fromAddress string) (*SMTPSender, error) {
	if strings.TrimSpace(connectionURI) == "" {
		return nil, fmt.Errorf("smtp connection URI is empty")
	}
	if strings.TrimSpace(fromAddress) == "" {
		return nil, fmt.Errorf("smtp from address is empty")
	}

	u, err := url.Parse(strings.TrimSpace(connectionURI))
	if err != nil {
		return nil, fmt.Errorf("parsing smtp connection URI: %w", err)
	}

	var implicitTLS bool
	switch strings.ToLower(u.Scheme) {
	case "smtp":
	case "smtps":
		implicitTLS = true
	default:
		return nil, fmt.Errorf("smtp connection URI scheme must be smtp or smtps, got %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("smtp connection URI has no host")
	}
	port := u.Port()
	if port == "" {
		// The submission ports each scheme implies. A relay on a different port
		// has to say so, which every real configuration does.
		if implicitTLS {
			port = "465"
		} else {
			port = "25"
		}
	}

	query := u.Query()
	// STARTTLS is on unless the URI turns it off, so a plain smtp:// URI to a
	// real relay is encrypted by default and a MailHog URI has to opt out. The
	// mirror image — opportunistic upgrade, quietly falling back to plaintext —
	// would mean an operator could not tell whether their invitations were
	// encrypted in transit.
	disableSTARTTLS := isTrue(query.Get("disable_starttls"))

	sender := &SMTPSender{
		addr:        net.JoinHostPort(host, port),
		host:        host,
		from:        strings.TrimSpace(fromAddress),
		implicitTLS: implicitTLS,
		startTLS:    !implicitTLS && !disableSTARTTLS,
		skipVerify:  isTrue(query.Get("skip_ssl_verify")),
		timeout:     DefaultTimeout,
	}
	if u.User != nil {
		sender.username = u.User.Username()
		sender.password, _ = u.User.Password()
	}
	return sender, nil
}

// isTrue reads the boolean query parameters courier URIs use. Only "true" and
// "1" enable; anything else, including an absent parameter, leaves the setting
// off — the direction where a typo costs security rather than delivery.
func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1":
		return true
	default:
		return false
	}
}

// From returns the envelope and header sender address.
func (s *SMTPSender) From() string { return s.from }

// SetTimeout overrides DefaultTimeout. It exists for tests, which must not
// spend ten seconds discovering that a fake relay hung.
func (s *SMTPSender) SetTimeout(d time.Duration) {
	if d > 0 {
		s.timeout = d
	}
}

// Send delivers one message.
//
// The context bounds the dial and, through the connection deadline, the whole
// conversation: net/smtp has no context support of its own, so without the
// deadline a relay that accepts the connection and then stops responding would
// hold the request open indefinitely.
//
// Every failure is returned rather than logged and swallowed. The caller — the
// invite service — is the one that decides an undelivered invitation is still a
// usable invitation, and it can only tell the member that if it hears about the
// failure.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.To) == "" {
		return fmt.Errorf("message has no recipient")
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	conn, err := s.dial(ctx)
	if err != nil {
		return err
	}
	// Deadline rather than a watchdog goroutine: every read and write below
	// goes through this connection, so one deadline bounds all of them.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("smtp handshake with %s: %w", s.addr, err)
	}
	defer client.Close()

	if s.startTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			// Refusing beats continuing in the clear. The URI asked for
			// STARTTLS by not disabling it, and quietly sending an invitation —
			// which contains a token that admits its holder to the town — over
			// plaintext is not a degradation the operator agreed to.
			return fmt.Errorf("smtp relay %s does not offer STARTTLS (set disable_starttls=true to send in the clear)", s.addr)
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			return fmt.Errorf("smtp STARTTLS with %s: %w", s.addr, err)
		}
	}

	if s.username != "" {
		// net/smtp refuses PLAIN over an unencrypted connection to anything but
		// localhost, which is the behaviour we want: credentials must not be
		// handed to a remote relay in the clear because a URI forgot its TLS.
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth with %s: %w", s.addr, err)
		}
	}

	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("smtp MAIL FROM %s: %w", s.from, err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write([]byte(buildMessage(s.from, msg, time.Now()))); err != nil {
		return fmt.Errorf("writing message body: %w", err)
	}
	if err := w.Close(); err != nil {
		// Closing the dot-writer is what commits the message, so this is the
		// error that says the relay refused it.
		return fmt.Errorf("completing message: %w", err)
	}

	return client.Quit()
}

// dial opens the transport, with TLS already established for smtps.
func (s *SMTPSender) dial(ctx context.Context) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("dialing smtp relay %s: %w", s.addr, err)
	}
	if !s.implicitTLS {
		return conn, nil
	}

	tlsConn := tls.Client(conn, s.tlsConfig())
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("tls handshake with %s: %w", s.addr, err)
	}
	return tlsConn, nil
}

func (s *SMTPSender) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         s.host,
		InsecureSkipVerify: s.skipVerify, //nolint:gosec // opt-in via skip_ssl_verify, matching the Kratos courier URI this shares
	}
}
