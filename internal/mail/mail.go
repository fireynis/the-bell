// Package mail sends the handful of messages The Bell itself originates.
//
// Kratos already has a courier for the messages that belong to authentication —
// recovery and verification — and this package deliberately does not try to
// replace it. It exists because an invitation is not an authentication message:
// it is sent by a resident, to somebody who has no identity yet, about a town
// Kratos knows nothing about. The two are configured from the same relay
// settings on purpose (see SMTP_CONNECTION_URI in internal/config), so an
// operator sets a relay once and both halves work.
//
// It is stdlib only — net/smtp and crypto/tls — because one plain-text message
// to one recipient is the whole requirement, and a dependency that grew MIME
// multipart, templating and retry queues would be carrying weight for a feature
// that does not want it.
package mail

import (
	"context"
	"fmt"
	"mime"
	"strings"
	"time"
)

// Message is one plain-text email to one recipient.
//
// One recipient, not a list: every message this package sends is addressed to a
// person whose address somebody typed, and a Bcc-style fan-out would make a
// failure ambiguous — "the send failed" would not say to whom.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers a message, or explains why it could not.
//
// It is an interface so that callers can be tested without a relay and so that
// a deployment with no SMTP configured simply has no sender rather than a
// sender that silently drops mail. Nothing in this codebase implements a
// no-op Sender for that reason: "sending is off" is represented by the absence
// of a Sender, which a caller has to notice.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// buildMessage renders the RFC 5322 bytes for a plain-text message.
//
// The subject is RFC 2047 encoded because it carries a town name and a
// resident's display name, either of which may be non-ASCII; sent raw, those
// bytes are undefined on the wire and render as mojibake in most clients.
// QEncoding leaves a plain-ASCII subject untouched, so the common case is
// unchanged.
//
// Line endings are left as the caller wrote them. net/smtp's Data writer is a
// textproto dot-writer, which converts bare newlines to CRLF and performs the
// dot-stuffing that would otherwise let a body line of "." end the message
// early.
func buildMessage(from string, msg Message, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.Body)
	return b.String()
}
