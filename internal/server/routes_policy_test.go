package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/storage"
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

// The route now reads through the image store, so its existence has to follow
// the store and not a config string. It used to be gated on
// cfg.ImageStoragePath, which meant an instance with no usable store still
// answered /uploads by reading whatever directory that string named — a live
// route with nothing on the other end of it.
func TestUploadsRouteFollowsTheImageStore(t *testing.T) {
	tests := []struct {
		name           string
		withStore      bool
		storagePath    string
		wantRegistered bool
	}{
		{
			name:           "a store registers the route",
			withStore:      true,
			storagePath:    "", // deliberately unset: the store decides, not this
			wantRegistered: true,
		},
		{
			// The case the old gate got wrong.
			name:           "a storage path with no store does not",
			withStore:      false,
			storagePath:    "/some/configured/directory",
			wantRegistered: false,
		},
		{
			name:           "neither does nothing at all",
			withStore:      false,
			storagePath:    "",
			wantRegistered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			opts := []Option{}
			if tt.withStore {
				store, err := storage.NewLocalStorage(t.TempDir(), "/uploads/")
				if err != nil {
					t.Fatalf("NewLocalStorage: %v", err)
				}
				opts = append(opts, WithImageStore(store))
			}

			srv := New(config.Config{Port: 0, ImageStoragePath: tt.storagePath}, nil, logger, opts...)

			mux, ok := srv.Handler().(*chi.Mux)
			if !ok {
				t.Fatalf("server handler is %T, want *chi.Mux", srv.Handler())
			}
			pattern := mux.Find(chi.NewRouteContext(), http.MethodGet, "/uploads/photo.jpg")

			if registered := pattern == "/uploads/*"; registered != tt.wantRegistered {
				t.Errorf("route registered = %v (pattern %q), want %v", registered, pattern, tt.wantRegistered)
			}
		})
	}
}

// What the store wrote is what the route serves, with no config string
// agreeing in the middle. This is the coupling the read method exists to
// create: before it, the route read a directory and the store wrote to one,
// and nothing made them the same directory.
func TestUploadsServesWhatTheStoreWrote(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	// A deliberately wrong ImageStoragePath: nothing may read it.
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := New(config.Config{Port: 0, ImageStoragePath: filepath.Join(t.TempDir(), "wrong")}, nil, logger,
		WithImageStore(store))

	saved, err := store.Save(context.Background(), "photo.jpg", strings.NewReader("image bytes"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, store.URL(saved), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "image bytes" {
		t.Errorf("body = %q, want the saved bytes", rec.Body.String())
	}
}

// The read path refuses traversal for the same reason the write path does, and
// answers with the same 404 a missing file gets — a distinct status would let a
// caller map the filesystem one request at a time.
func TestUploadsRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "secret.txt"), "top secret")

	dir := filepath.Join(root, "images")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "photo.jpg"), "image bytes")

	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: dir})

	for _, path := range []string{
		"/uploads/../secret.txt",
		"/uploads/../../etc/passwd",
		"/uploads/..%2fsecret.txt",
		"/uploads/subdir/photo.jpg",
		`/uploads/..\secret.txt`,
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code == http.StatusOK {
				t.Fatalf("status = 200, body: %q", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "top secret") {
				t.Errorf("response leaked content from outside the storage directory: %q", rec.Body.String())
			}
		})
	}
}

// Conditional and range handling survive the move off http.FileServer. Both
// come from http.ServeContent, which is why storage.File carries a Seeker and a
// ModTime rather than being a plain io.ReadCloser — a reader alone can serve
// the whole file and nothing else.
//
// The 304 also has to keep its Cache-Control. That is the case cacheOnSuccess's
// comment is about: a revalidation is how the browser renews the year-long
// directive, so dropping it there would put the client back to asking on every
// load.
func TestUploadsAnswersConditionalRequests(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "image bytes")

	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: dir})

	// First fetch, to learn the modification time the server reports.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uploads/photo.jpg", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	lastModified := rec.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("no Last-Modified header: conditional requests cannot work without one")
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/photo.jpg", nil)
	req.Header.Set("If-Modified-Since", lastModified)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", rec.Body.Len())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000" {
		t.Errorf("Cache-Control on the revalidation = %q, want the long-lived directive", got)
	}
}

func TestUploadsAnswersRangeRequests(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "0123456789")

	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: dir})

	req := httptest.NewRequest(http.MethodGet, "/uploads/photo.jpg", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusPartialContent)
	}
	if rec.Body.String() != "2345" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "2345")
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Errorf("Content-Range = %q, want %q", got, "bytes 2-5/10")
	}
}

// The extension still picks the Content-Type. ServeContent takes the name from
// the URL rather than from the filesystem, so this pins that the right string
// is being handed over.
func TestUploadsSetsContentTypeFromTheName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.png"), "\x89PNG\r\n\x1a\n")

	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: dir})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uploads/photo.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/png") {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
}

// A store that is misconfigured, unreachable or unreadable answers 404 like
// everything else, because the caller must not be able to tell those apart from
// a missing file. That makes the log the only place the real cause can appear,
// and an operator staring at 404s on images they know they uploaded has
// nothing else to go on.
func TestUploadsLogsFailuresThatAreNotAMissingFile(t *testing.T) {
	tests := []struct {
		name      string
		openErr   error
		wantLog   bool
		wantInLog string
	}{
		{
			name:    "a missing object is routine and stays quiet",
			openErr: fs.ErrNotExist,
			wantLog: false,
		},
		{
			name:    "a refused name is the caller's doing, not the server's",
			openErr: storage.ErrUnsafeName,
			wantLog: false,
		},
		{
			name:      "a permission failure is a deployment problem",
			openErr:   fs.ErrPermission,
			wantLog:   true,
			wantInLog: "permission",
		},
		{
			name:      "so is an unreachable backend",
			openErr:   errors.New("connection refused"),
			wantLog:   true,
			wantInLog: "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			srv := New(config.Config{Port: 0}, nil, logger, WithImageStore(errStorage{err: tt.openErr}))

			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uploads/photo.jpg", nil))

			// The status is the same either way; that is the point.
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			// And the reason never reaches the client.
			if tt.wantInLog != "" && strings.Contains(rec.Body.String(), tt.wantInLog) {
				t.Errorf("response leaked the reason: %q", rec.Body.String())
			}

			logged := strings.Contains(logs.String(), "upload could not be read")
			if logged != tt.wantLog {
				t.Errorf("logged = %v, want %v; log: %s", logged, tt.wantLog, logs.String())
			}
			if tt.wantInLog != "" && !strings.Contains(logs.String(), tt.wantInLog) {
				t.Errorf("log does not name the cause %q: %s", tt.wantInLog, logs.String())
			}
		})
	}
}

// errStorage fails every Open with a fixed error. Only the read path is
// exercised through it, so Save and URL are here to satisfy the interface.
type errStorage struct{ err error }

func (errStorage) Save(context.Context, string, io.Reader) (string, error) { return "", nil }
func (s errStorage) Open(context.Context, string) (storage.File, error)    { return nil, s.err }
func (errStorage) URL(path string) string                                  { return "/uploads/" + path }
