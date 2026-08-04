package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// Production runs behind TLS, so the Secure attribute Kratos sets is the only
// thing keeping the session cookie off a plaintext connection. It may only be
// dropped for a local dev stack, and the caller must ask for that explicitly.
func TestRewriteSetCookies(t *testing.T) {
	tests := []struct {
		name          string
		cookies       []string
		allowInsecure bool
		want          []string
	}{
		{
			name:          "production keeps Secure on the session cookie",
			cookies:       []string{"ory_kratos_session=abc; Path=/; HttpOnly; Secure; SameSite=Lax"},
			allowInsecure: false,
			want:          []string{"ory_kratos_session=abc; Path=/; HttpOnly; Secure; SameSite=Lax"},
		},
		{
			name:          "dev drops Secure so plain-HTTP localhost works",
			cookies:       []string{"ory_kratos_session=abc; Path=/; HttpOnly; Secure; SameSite=Lax"},
			allowInsecure: true,
			want:          []string{"ory_kratos_session=abc; Path=/; HttpOnly; SameSite=Lax"},
		},
		{
			name:          "dev strips Secure from every cookie, not just the first",
			cookies:       []string{"a=1; Secure", "b=2; Path=/; Secure", "c=3; Secure; HttpOnly"},
			allowInsecure: true,
			want:          []string{"a=1", "b=2; Path=/", "c=3; HttpOnly"},
		},
		{
			name:          "cookie without Secure is passed through unchanged",
			cookies:       []string{"csrf_token=xyz; Path=/; HttpOnly; SameSite=Lax"},
			allowInsecure: true,
			want:          []string{"csrf_token=xyz; Path=/; HttpOnly; SameSite=Lax"},
		},
		{
			name:          "trailing Secure attribute is removed cleanly",
			cookies:       []string{"a=1; Path=/; Secure"},
			allowInsecure: true,
			want:          []string{"a=1; Path=/"},
		},
		{
			name:          "attribute name matching is case-insensitive",
			cookies:       []string{"a=1; Path=/; secure"},
			allowInsecure: true,
			want:          []string{"a=1; Path=/"},
		},
		{
			name:          "empty slice",
			cookies:       nil,
			allowInsecure: true,
			want:          nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteSetCookies(tt.cookies, tt.allowInsecure)
			if !slices.Equal(got, tt.want) {
				t.Errorf("rewriteSetCookies() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The old implementation used strings.Replace(c, "; Secure", "", 1), which
// chewed a hole in any cookie whose value or other attribute merely started
// with "Secure". Matching whole attributes keeps those intact.
func TestRewriteSetCookies_DoesNotMangleSecureSubstrings(t *testing.T) {
	tests := []struct {
		name   string
		cookie string
		want   string
	}{
		{
			name:   "attribute whose name begins with Secure",
			cookie: "a=1; SecureFlag=no; Secure",
			want:   "a=1; SecureFlag=no",
		},
		{
			name:   "path containing the word Secure",
			cookie: "a=1; Path=/Secure; Secure",
			want:   "a=1; Path=/Secure",
		},
		{
			name:   "cookie value containing the word Secure",
			cookie: "redirect_to=https://example.com/Secure; Path=/; Secure",
			want:   "redirect_to=https://example.com/Secure; Path=/",
		},
		{
			name:   "no Secure attribute at all, only lookalikes",
			cookie: "a=Secure; SecureOnly=1",
			want:   "a=Secure; SecureOnly=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteSetCookies([]string{tt.cookie}, true)
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("rewriteSetCookies() = %q, want [%q]", got, tt.want)
			}
		})
	}
}

func TestSpaHandler(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html>spa shell</html>")
	writeFile(t, filepath.Join(dir, "app.js"), "console.log(1)")

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "existing asset is served from disk",
			path: "/app.js",
			want: "console.log(1)",
		},
		{
			// Client-side routes have no file behind them; the shell boots and
			// the router takes over.
			name: "unknown path falls back to the SPA shell",
			path: "/profile/some-user",
			want: "<html>spa shell</html>",
		},
		{
			name: "root serves the SPA shell",
			path: "/",
			want: "<html>spa shell</html>",
		},
	}

	h := spaHandler(dir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if body := rec.Body.String(); body != tt.want {
				t.Errorf("body = %q, want %q", body, tt.want)
			}
		})
	}
}

// A traversal must never read outside the dist directory. fs.Stat on the
// os.DirFS rejects the path as invalid, and the index.html fallback then
// refuses it too because ServeFile will not serve a request whose URL contains
// a dot-dot segment — so the request is rejected rather than answered.
func TestSpaHandler_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html>spa shell</html>")

	tests := []struct {
		name string
		path string
	}{
		{"relative traversal", "/../etc/passwd"},
		{"percent-encoded traversal", "/..%2f..%2fetc/passwd"},
		{"traversal below a real prefix", "/assets/../../etc/passwd"},
	}

	h := spaHandler(dir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if strings.Contains(rec.Body.String(), "root:") {
				t.Error("response leaked content from outside the served directory")
			}
		})
	}
}

// A year-long Cache-Control on a 404 pins the miss in every intermediary, so a
// file uploaded to that path a minute later stays invisible. Only a response
// that actually carries the file may be cached.
func TestCacheOnSuccess(t *testing.T) {
	const cacheHeader = "public, max-age=31536000"

	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"200 is cached", http.StatusOK, cacheHeader},
		{"206 partial content is cached", http.StatusPartialContent, cacheHeader},
		{"304 revalidation keeps the directive", http.StatusNotModified, cacheHeader},
		{"404 is not cached", http.StatusNotFound, ""},
		{"403 is not cached", http.StatusForbidden, ""},
		{"500 is not cached", http.StatusInternalServerError, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w := &cacheOnSuccess{ResponseWriter: rec}
			w.WriteHeader(tt.status)

			if got := rec.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

// A handler that writes a body without calling WriteHeader is implicitly
// sending 200, and that response is still a cacheable hit.
func TestCacheOnSuccess_ImplicitOK(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &cacheOnSuccess{ResponseWriter: rec}

	if _, err := w.Write([]byte("image bytes")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000" {
		t.Errorf("Cache-Control = %q, want the long-lived directive", got)
	}
}

// Only the first status wins, matching net/http: a later WriteHeader must not
// retroactively add caching to a response already committed as an error.
func TestCacheOnSuccess_IgnoresSecondWriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &cacheOnSuccess{ResponseWriter: rec}

	w.WriteHeader(http.StatusNotFound)
	w.WriteHeader(http.StatusOK)

	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control = %q, want it unset", got)
	}
}

// The whole point of phrasing the field as skipActive is that forgetting it is
// safe. A route group written as guard{...} without mentioning the active
// check must still get it — under the old `active bool` the zero value skipped
// RequireActive, so a new group that forgot one line silently served suspended
// and banned users and nothing failed.
func TestProtected_ZeroGuardEnforcesRequireActive(t *testing.T) {
	tests := []struct {
		name       string
		g          guard
		user       *domain.User
		wantStatus int
	}{
		{
			name:       "zero guard rejects a suspended user",
			g:          guard{},
			user:       &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: false},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "zero guard admits an active user",
			g:          guard{},
			user:       &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "skipActive is the documented opt-out for /v1/me",
			g:          guard{skipActive: true},
			user:       &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: false},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})

			// Apply in reverse so the first middleware ends up outermost.
			mws := (&Server{}).protected(tt.g)
			for i := len(mws) - 1; i >= 0; i-- {
				h = mws[i](h)
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(middleware.WithUser(req.Context(), tt.user))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if reached != (tt.wantStatus == http.StatusOK) {
				t.Errorf("handler reached = %v, want %v", reached, tt.wantStatus == http.StatusOK)
			}
		})
	}
}

// /uploads/ must not enumerate the image directory. http.FileServer renders an
// index page for any directory lacking index.html, which would hand an
// unauthenticated caller the filename of every image ever uploaded.
func TestUploadsDoesNotListDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "private-photo.jpg"), "image bytes")

	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: dir})

	for _, path := range []string{"/uploads/", "/uploads/."} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if strings.Contains(rec.Body.String(), "private-photo.jpg") {
				t.Errorf("listed the upload directory: %q", rec.Body.String())
			}
		})
	}
}

// The files themselves must still be served — the directory guard must not
// take the actual images with it.
func TestUploadsStillServesFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "image bytes")

	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: dir})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uploads/photo.jpg", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "image bytes" {
		t.Errorf("body = %q, want the file contents", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000" {
		t.Errorf("Cache-Control = %q, want the long-lived directive", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
