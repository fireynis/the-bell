package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/fireynis/the-bell/internal/domain"
)

// InviteCookieName is where the SPA parks the raw invitation token when
// somebody lands on an invitation link, and the only place this gate looks for
// it.
//
// A cookie rather than a query parameter or a header, because the thing that
// has to carry the token is a request the browser makes to Kratos through the
// /.ory proxy — a request this application does not construct and cannot add a
// header to. The SPA sets the cookie on landing and the browser presents it on
// every subsequent proxy call, which is exactly the property needed.
const InviteCookieName = "bell_invite"

// maxRegistrationBodyBytes caps the registration submit body the gate will
// buffer.
//
// The gate has to read the body to compare the submitted address against the
// invitation, and then hand the same bytes on to Kratos, which means holding
// them in memory. A megabyte is orders of magnitude more than a registration
// form — an email, a name, a password and a CSRF token — and refusing beyond it
// is better than either buffering without limit or truncating a body that then
// gets proxied as a corrupted request.
const maxRegistrationBodyBytes = 1 << 20

// RegistrationInvites is what the gate needs of the invite service: whether
// this town requires an invitation, and whether a given raw token names a live
// one.
type RegistrationInvites interface {
	InviteRequired(ctx context.Context) (bool, error)
	LiveInviteByToken(ctx context.Context, rawToken string) (*domain.Invite, error)
}

// InviteRegistrationGate refuses registration without a live invitation, on a
// town that admits people by invitation only.
//
// It guards exactly the paths KratosRegistrationLimit throttles — the two flow
// inits and the submit — and for the same reason: those three are account
// creation, and everything else under /.ory is a resident signing in or a page
// re-reading a flow it already started. Guarding by prefix would break the SPA,
// which fetches the registration flow on every render of the sign-up page.
//
// On the submit it does one thing more. The cookie proves the caller holds an
// invitation; it does not prove the invitation is theirs. Without the address
// check, one leaked link would be a general-purpose key to the town, usable by
// anyone it was forwarded to and reusable until it expired. So the submitted
// traits.email must match the invited address, case-insensitively — the same
// normalisation the service stores and matches on, so a capitalised address in
// the form is not a rejection.
//
// The gate is inert in open mode and inert when no invite service is wired,
// which keeps "invitations are off" a single decision made in one place rather
// than a route that quietly disappears.
func InviteRegistrationGate(invites RegistrationInvites, logger *slog.Logger) func(http.Handler) http.Handler {
	if invites == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isRegistrationFlowPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			required, err := invites.InviteRequired(r.Context())
			if err != nil {
				// Refusing beats guessing, in both directions. Guessing "open"
				// would let strangers register into an invite-only town during a
				// database blip; guessing "invite" would refuse an open town's
				// registrations while claiming they need an invitation they
				// cannot get. A 503 says what is true — the town cannot answer
				// right now — and the applicant's retry succeeds once it can.
				logger.Error("registration gate: reading registration mode failed", "error", err)
				writeError(w, http.StatusServiceUnavailable, "registration is temporarily unavailable")
				return
			}
			if !required {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(InviteCookieName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				writeError(w, http.StatusForbidden, "registration is by invitation")
				return
			}

			invite, err := invites.LiveInviteByToken(r.Context(), cookie.Value)
			if errors.Is(err, domain.ErrNotFound) {
				// Unknown, consumed, revoked and expired are one answer here for
				// the same reason they are one answer at the lookup endpoint.
				writeError(w, http.StatusForbidden, "registration is by invitation")
				return
			}
			if err != nil {
				logger.Error("registration gate: invite lookup failed", "error", err)
				writeError(w, http.StatusServiceUnavailable, "registration is temporarily unavailable")
				return
			}

			if isRegistrationSubmitPath(r.URL.Path) {
				body, ok := readAndRestoreBody(w, r)
				if !ok {
					return
				}
				if email, found := submittedEmail(r.Header.Get("Content-Type"), body); found &&
					!strings.EqualFold(strings.TrimSpace(email), invite.Email) {
					writeError(w, http.StatusForbidden, "this invitation is for a different email address")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isRegistrationSubmitPath reports whether this is the request that actually
// creates the account, as opposed to one that starts a flow. Only the submit
// carries traits to check.
func isRegistrationSubmitPath(path string) bool {
	path = strings.TrimPrefix(path, kratosProxyPrefix)
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	return path == "/self-service/registration"
}

// readAndRestoreBody buffers the request body and puts it back, so the proxy
// forwards exactly the bytes the client sent.
//
// Restoring is the whole point: an http.Request body is a stream, so reading it
// here without replacing it would hand Kratos an empty registration form and
// every sign-up would fail with a validation error nobody could explain.
// ContentLength is deliberately left alone, because the bytes are unchanged.
//
// Reporting false means a response has already been written and the caller must
// stop.
func readAndRestoreBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		return nil, true
	}

	// One byte past the limit, so a body at exactly the cap is accepted and one
	// over it is detected rather than silently truncated.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRegistrationBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return nil, false
	}
	if len(body) > maxRegistrationBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, true
}

// submittedEmail digs the registration address out of a submit body, reporting
// whether it found one.
//
// Kratos accepts both JSON and form encodings for the same flow, so both are
// read. A body in neither shape, or one that does not carry traits.email,
// yields found=false and the gate lets the request through — which is safe
// because the gate and Kratos read the same bytes: a body this cannot parse is
// a body Kratos cannot create an account from either, and a two-step flow whose
// second step omits the traits is completing a first step that carried them and
// was checked here.
func submittedEmail(contentType string, body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// No usable Content-Type. JSON is the SPA's encoding, so it is the one
		// worth attempting blind.
		return jsonTraitEmail(body)
	}

	switch mediaType {
	case "application/json":
		return jsonTraitEmail(body)
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return "", false
		}
		return formTraitEmail(values)
	case "multipart/form-data":
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		// A generous in-memory bound: a registration form has no file parts, so
		// anything that spills to disk is not a registration this gate needs to
		// understand.
		form, err := reader.ReadForm(maxRegistrationBodyBytes)
		if err != nil {
			return "", false
		}
		defer form.RemoveAll()
		return formTraitEmail(form.Value)
	default:
		return "", false
	}
}

// jsonTraitEmail reads traits.email out of a JSON submit body. Unknown fields
// are ignored rather than rejected: this is Kratos's request shape, not ours,
// and it carries a method, a password and a CSRF token besides.
func jsonTraitEmail(body []byte) (string, bool) {
	var payload struct {
		Traits struct {
			Email string `json:"email"`
		} `json:"traits"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}
	if payload.Traits.Email == "" {
		return "", false
	}
	return payload.Traits.Email, true
}

// formTraitEmail reads the flattened "traits.email" field the HTML form
// encoding uses for the same value.
func formTraitEmail(values map[string][]string) (string, bool) {
	got := values["traits.email"]
	if len(got) == 0 || got[0] == "" {
		return "", false
	}
	return got[0], true
}
