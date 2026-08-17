package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// fakeRegistrationInvites is the invite service as the gate sees it.
type fakeRegistrationInvites struct {
	required    bool
	requiredErr error
	invite      *domain.Invite
	lookupErr   error

	gotToken   string
	lookupCall int
}

func (f *fakeRegistrationInvites) InviteRequired(context.Context) (bool, error) {
	if f.requiredErr != nil {
		return false, f.requiredErr
	}
	return f.required, nil
}

func (f *fakeRegistrationInvites) LiveInviteByToken(_ context.Context, rawToken string) (*domain.Invite, error) {
	f.lookupCall++
	f.gotToken = rawToken
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	return f.invite, nil
}

func liveInvite(email string) *domain.Invite {
	return &domain.Invite{
		ID: "invite-1", Email: email, InviterID: "inviter-1",
		CreatedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(14 * 24 * time.Hour),
	}
}

// spy records what the proxy would have received, including the body — which
// is the thing most easily broken by a middleware that reads it.
type spy struct {
	called bool
	body   string
}

func (s *spy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.called = true
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			s.body = string(b)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func gateRequest(method, path, contentType, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func withInviteCookie(r *http.Request, token string) *http.Request {
	r.AddCookie(&http.Cookie{Name: middleware.InviteCookieName, Value: token})
	return r
}

func serveGate(invites middleware.RegistrationInvites, req *http.Request) (*httptest.ResponseRecorder, *spy) {
	next := &spy{}
	gate := middleware.InviteRegistrationGate(invites, testsupport.DiscardLogger())
	rec := httptest.NewRecorder()
	gate(next.handler()).ServeHTTP(rec, req)
	return rec, next
}

// --- the gate is inert unless the town asked for it ---

func TestInviteRegistrationGate_OpenModeLetsEverythingThrough(t *testing.T) {
	invites := &fakeRegistrationInvites{required: false}

	rec, next := serveGate(invites, gateRequest(http.MethodPost,
		"/.ory/self-service/registration", "application/json", `{"traits":{"email":"stranger@example.com"}}`))

	if !next.called {
		t.Fatalf("registration was blocked in open mode: %d %s", rec.Code, rec.Body)
	}
	if invites.lookupCall != 0 {
		t.Error("the gate looked up an invitation in open mode")
	}
}

func TestInviteRegistrationGate_WithoutAServiceItIsNotInstalled(t *testing.T) {
	// A nil interface, which is what a deployment with no invite service hands
	// in. It must be a pass-through rather than a panic on the first lookup.
	rec, next := serveGate(nil, gateRequest(http.MethodPost, "/.ory/self-service/registration", "application/json", `{}`))

	if !next.called {
		t.Fatalf("registration was blocked with no invite service: %d %s", rec.Code, rec.Body)
	}
}

// Only the three paths KratosRegistrationLimit throttles are account creation.
// Everything else under /.ory is a resident signing in, or the SPA re-reading
// a flow it already started — gating those would break the site.
func TestInviteRegistrationGate_GuardsOnlyTheRegistrationPaths(t *testing.T) {
	tests := []struct {
		path      string
		wantGated bool
	}{
		{"/.ory/self-service/registration", true},
		{"/.ory/self-service/registration/browser", true},
		{"/.ory/self-service/registration/api", true},
		{"/.ory/self-service/registration/flows", false},
		{"/.ory/self-service/login/browser", false},
		{"/.ory/sessions/whoami", false},
		{"/.ory/self-service/recovery/browser", false},
		{"/.ory/self-service/settings/browser", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			invites := &fakeRegistrationInvites{required: true, lookupErr: domain.ErrNotFound}

			rec, next := serveGate(invites, gateRequest(http.MethodGet, tt.path, "", ""))

			if tt.wantGated {
				if next.called {
					t.Error("the request reached Kratos without an invitation")
				}
				if rec.Code != http.StatusForbidden {
					t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
				}
				return
			}
			if !next.called {
				t.Errorf("an unrelated path was gated: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

// --- the invitation itself ---

func TestInviteRegistrationGate_RefusesWithoutALiveInvitation(t *testing.T) {
	tests := []struct {
		name      string
		cookie    string
		hasCookie bool
		lookupErr error
	}{
		{name: "no cookie at all"},
		{name: "an empty cookie", cookie: "", hasCookie: true},
		{name: "a whitespace cookie", cookie: "   ", hasCookie: true},
		{name: "an unknown token", cookie: "made-up", hasCookie: true, lookupErr: domain.ErrNotFound},
		{name: "a consumed, revoked or expired token", cookie: "spent", hasCookie: true, lookupErr: domain.ErrNotFound},
	}

	var bodies []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invites := &fakeRegistrationInvites{required: true, lookupErr: tt.lookupErr}
			req := gateRequest(http.MethodGet, "/.ory/self-service/registration/browser", "", "")
			if tt.hasCookie {
				req = withInviteCookie(req, tt.cookie)
			}

			rec, next := serveGate(invites, req)

			if next.called {
				t.Fatal("the request reached Kratos")
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
			}
			if !strings.Contains(rec.Body.String(), "registration is by invitation") {
				t.Errorf("body = %s, want the invitation message", rec.Body)
			}
			bodies = append(bodies, rec.Body.String())
		})
	}

	// One answer for all of them: a caller must not learn from the refusal
	// whether their token was ever real.
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Errorf("body %d = %q differs from %q", i, bodies[i], bodies[0])
		}
	}
}

func TestInviteRegistrationGate_AdmitsAFlowInitWithALiveInvitation(t *testing.T) {
	invites := &fakeRegistrationInvites{required: true, invite: liveInvite("newcomer@example.com")}

	rec, next := serveGate(invites, withInviteCookie(
		gateRequest(http.MethodGet, "/.ory/self-service/registration/browser", "", ""), "raw-token"))

	if !next.called {
		t.Fatalf("a live invitation was refused: %d %s", rec.Code, rec.Body)
	}
	if invites.gotToken != "raw-token" {
		t.Errorf("looked up %q, want the cookie's raw token", invites.gotToken)
	}
}

// --- the address check on the submit ---

func TestInviteRegistrationGate_SubmitRequiresTheInvitedAddress(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantAdmit   bool
	}{
		{
			name:        "json, the invited address",
			contentType: "application/json",
			body:        `{"method":"password","password":"hunter2","traits":{"email":"newcomer@example.com","name":"New"}}`,
			wantAdmit:   true,
		},
		{
			name:        "json, capitalised differently",
			contentType: "application/json",
			body:        `{"traits":{"email":"NewComer@Example.COM"}}`,
			wantAdmit:   true,
		},
		{
			name:        "json, somebody else's address",
			contentType: "application/json",
			body:        `{"traits":{"email":"gatecrasher@example.com"}}`,
		},
		{
			name:        "form, the invited address",
			contentType: "application/x-www-form-urlencoded",
			body:        url.Values{"traits.email": {"newcomer@example.com"}, "method": {"password"}}.Encode(),
			wantAdmit:   true,
		},
		{
			name:        "form, somebody else's address",
			contentType: "application/x-www-form-urlencoded",
			body:        url.Values{"traits.email": {"gatecrasher@example.com"}}.Encode(),
		},
		{
			name:        "no traits in the body at all",
			contentType: "application/json",
			// A second step that carries only a password completes a first step
			// that carried the address and was checked. Nothing to compare, and
			// Kratos will not invent an address that was never submitted.
			body:      `{"method":"password","password":"hunter2"}`,
			wantAdmit: true,
		},
		{
			name:        "a body in a shape neither we nor Kratos can read",
			contentType: "application/octet-stream",
			body:        "\x00\x01\x02",
			wantAdmit:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invites := &fakeRegistrationInvites{required: true, invite: liveInvite("newcomer@example.com")}

			rec, next := serveGate(invites, withInviteCookie(
				gateRequest(http.MethodPost, "/.ory/self-service/registration", tt.contentType, tt.body), "raw-token"))

			if tt.wantAdmit {
				if !next.called {
					t.Fatalf("the submit was refused: %d %s", rec.Code, rec.Body)
				}
				return
			}
			if next.called {
				t.Fatal("a submit for a different address reached Kratos")
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
			}
			if !strings.Contains(rec.Body.String(), "different email address") {
				t.Errorf("body = %s, want the mismatched-address message", rec.Body)
			}
		})
	}
}

func TestInviteRegistrationGate_ReadsAMultipartSubmit(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("traits.email", "gatecrasher@example.com"); err != nil {
		t.Fatalf("writing multipart field: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	invites := &fakeRegistrationInvites{required: true, invite: liveInvite("newcomer@example.com")}
	rec, next := serveGate(invites, withInviteCookie(
		gateRequest(http.MethodPost, "/.ory/self-service/registration", w.FormDataContentType(), buf.String()), "raw-token"))

	if next.called {
		t.Fatal("a multipart submit for a different address reached Kratos")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// The gate reads the body to check the address and then has to hand Kratos the
// same bytes. Without the restore, every sign-up would reach Kratos as an empty
// form and fail with a validation error nobody could explain.
func TestInviteRegistrationGate_RestoresTheBodyForKratos(t *testing.T) {
	body := `{"method":"password","password":"hunter2","traits":{"email":"newcomer@example.com","name":"New"}}`
	invites := &fakeRegistrationInvites{required: true, invite: liveInvite("newcomer@example.com")}
	req := withInviteCookie(gateRequest(http.MethodPost, "/.ory/self-service/registration", "application/json", body), "raw-token")
	wantLength := req.ContentLength

	rec, next := serveGate(invites, req)

	if !next.called {
		t.Fatalf("the submit was refused: %d %s", rec.Code, rec.Body)
	}
	if next.body != body {
		t.Errorf("Kratos received %q, want the original body %q", next.body, body)
	}
	if req.ContentLength != wantLength {
		t.Errorf("ContentLength = %d, want it unchanged at %d", req.ContentLength, wantLength)
	}
}

func TestInviteRegistrationGate_RefusesAnEnormousSubmitBody(t *testing.T) {
	huge, err := json.Marshal(map[string]any{"note": strings.Repeat("a", 2<<20)})
	if err != nil {
		t.Fatalf("building the body: %v", err)
	}
	invites := &fakeRegistrationInvites{required: true, invite: liveInvite("newcomer@example.com")}

	rec, next := serveGate(invites, withInviteCookie(
		gateRequest(http.MethodPost, "/.ory/self-service/registration", "application/json", string(huge)), "raw-token"))

	if next.called {
		t.Fatal("an oversized body was proxied")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

// A flow init has no body to check, so reading one must not be a precondition.
func TestInviteRegistrationGate_DoesNotReadTheBodyOnAFlowInit(t *testing.T) {
	invites := &fakeRegistrationInvites{required: true, invite: liveInvite("newcomer@example.com")}

	rec, next := serveGate(invites, withInviteCookie(
		gateRequest(http.MethodGet, "/.ory/self-service/registration/browser", "", ""), "raw-token"))

	if !next.called {
		t.Fatalf("a flow init with a live invitation was refused: %d %s", rec.Code, rec.Body)
	}
}

// --- when the town cannot answer ---

func TestInviteRegistrationGate_RefusesRatherThanGuessingTheMode(t *testing.T) {
	invites := &fakeRegistrationInvites{requiredErr: errors.New("database unavailable")}

	rec, next := serveGate(invites, withInviteCookie(
		gateRequest(http.MethodPost, "/.ory/self-service/registration", "application/json", `{}`), "raw-token"))

	if next.called {
		t.Fatal("registration proceeded while the mode was unknown")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	// Not a 403: the applicant did nothing wrong and a retry will work. Telling
	// them they need an invitation would send them looking for one they may
	// already hold.
	if strings.Contains(rec.Body.String(), "by invitation") {
		t.Errorf("body = %s, want a temporary-failure message", rec.Body)
	}
}

func TestInviteRegistrationGate_RefusesWhenTheInviteLookupFails(t *testing.T) {
	invites := &fakeRegistrationInvites{required: true, lookupErr: errors.New("connection reset")}

	rec, next := serveGate(invites, withInviteCookie(
		gateRequest(http.MethodGet, "/.ory/self-service/registration/browser", "", ""), "raw-token"))

	if next.called {
		t.Fatal("registration proceeded on a failed invitation lookup")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
