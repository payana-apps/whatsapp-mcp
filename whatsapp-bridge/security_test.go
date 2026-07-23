package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonReq(method string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, "/api/send", strings.NewReader("{}"))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestAuthorizeRequest(t *testing.T) {
	saved := bridgeToken
	defer func() { bridgeToken = saved }()

	cases := []struct {
		name    string
		token   string // configured server token
		method  string
		headers map[string]string
		want    int // expected status; 200 means authorized
	}{
		{"rejects GET", "", http.MethodGet, map[string]string{"Content-Type": "application/json"}, http.StatusMethodNotAllowed},
		{"rejects cross-origin", "", http.MethodPost, map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"}, http.StatusForbidden},
		{"rejects non-JSON", "", http.MethodPost, map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"rejects missing token", "secret", http.MethodPost, map[string]string{"Content-Type": "application/json"}, http.StatusUnauthorized},
		{"rejects wrong token", "secret", http.MethodPost, map[string]string{"Content-Type": "application/json", "X-Bridge-Token": "nope"}, http.StatusUnauthorized},
		{"accepts correct token", "secret", http.MethodPost, map[string]string{"Content-Type": "application/json", "X-Bridge-Token": "secret"}, http.StatusOK},
		{"accepts when no token configured", "", http.MethodPost, map[string]string{"Content-Type": "application/json"}, http.StatusOK},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bridgeToken = c.token
			w := httptest.NewRecorder()
			ok := authorizeRequest(w, jsonReq(c.method, c.headers))
			if c.want == http.StatusOK {
				if !ok {
					t.Fatalf("expected authorized, got status %d", w.Code)
				}
			} else {
				if ok {
					t.Fatalf("expected rejection %d, but request was authorized", c.want)
				}
				if w.Code != c.want {
					t.Fatalf("expected status %d, got %d", c.want, w.Code)
				}
			}
		})
	}
}

func TestValidateMediaPath(t *testing.T) {
	root := t.TempDir()
	os.Setenv("WHATSAPP_MEDIA_ROOT", root)
	defer os.Unsetenv("WHATSAPP_MEDIA_ROOT")

	// A legit file under the root.
	good := filepath.Join(root, "photo.jpg")
	if err := os.WriteFile(good, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// A file outside the root (simulating ~/.ssh/id_rsa exfiltration).
	outside := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(outside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	// A file inside a sensitive dir under the root.
	sshDir := filepath.Join(root, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(sshDir, "id_rsa")
	if err := os.WriteFile(secret, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := validateMediaPath(""); err != nil {
		t.Fatalf("empty path should be allowed, got %v", err)
	}
	if got, err := validateMediaPath(good); err != nil || got == "" {
		t.Fatalf("path under root should be allowed, got err=%v", err)
	}
	if _, err := validateMediaPath(outside); err == nil {
		t.Fatal("path outside root must be rejected")
	}
	if _, err := validateMediaPath(secret); err == nil {
		t.Fatal("path in a sensitive dir must be rejected")
	}
	// A sensitive file basename sitting directly under the root must be denied.
	netrc := filepath.Join(root, ".netrc")
	if err := os.WriteFile(netrc, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateMediaPath(netrc); err == nil {
		t.Fatal("a sensitive file basename (.netrc) under the root must be rejected")
	}
	if _, err := validateMediaPath(filepath.Join(root, "..", "escape")); err == nil {
		t.Fatal("traversal escaping the root must be rejected")
	}
}
