package main

// Payana hardening layer for the local REST bridge.
//
// Upstream binds the REST API to every interface (":8080") with no auth, no
// content-type check and no origin check, and reads any caller-supplied file
// path off disk. On a shared or office network that lets anyone reachable on
// the LAN send WhatsApp messages as the user or exfiltrate arbitrary files,
// and it is reachable as CSRF from any web page the user visits. This file
// adds loopback-only binding, a shared per-run token, JSON/same-origin
// enforcement and a media-path allowlist. All knobs are env-driven so the
// launcher can wire them without code changes.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	// bridgeHost defaults to loopback so the API is never exposed to the LAN.
	bridgeHost = getenvDefault("WHATSAPP_BRIDGE_HOST", "127.0.0.1")
	// bridgePort is configurable so two engineers on one machine don't collide on 8080.
	bridgePort = getenvIntDefault("WHATSAPP_BRIDGE_PORT", 8080)
	// bridgeToken is the shared secret the launcher mints per run and passes to
	// both this bridge and the MCP server. When empty (manual dev run) auth is
	// skipped, but the loopback bind still applies.
	bridgeToken = os.Getenv("WHATSAPP_BRIDGE_TOKEN")
)

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvIntDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvDurationDefault(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// newToken generates a random hex token; used by the launcher path if it ever
// needs the bridge to self-mint. Kept here so the format stays in one place.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// authorizeRequest enforces the four guards on every API request. Returns true
// only when the request may proceed; otherwise it has already written the error.
func authorizeRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	// A legitimate local MCP client never sets Origin. Any Origin means the
	// request came from a web page (CSRF) — reject it.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "Cross-origin requests are not allowed", http.StatusForbidden)
		return false
	}
	// Require an explicit JSON content type so simple/no-preflight form posts fail.
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	// Require the shared per-run token. main() already refuses to start the
	// server without one (fail-closed), but this checks it too instead of
	// trusting that invariant from the call site — an empty bridgeToken here
	// rejects rather than skips auth.
	got := r.Header.Get("X-Bridge-Token")
	if bridgeToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(bridgeToken)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// sensitiveDirs / sensitiveNames are never allowed in a media path even when
// they sit under the allowed root, so a coerced request can't read secrets.
var sensitiveDirs = []string{".ssh", ".aws", ".gnupg", ".kube", ".config", "Keychains"}

// sensitiveNames are exact file basenames denied anywhere under the root.
var sensitiveNames = map[string]bool{
	".git-credentials": true, ".netrc": true, ".npmrc": true, ".pgpass": true,
	".zsh_history": true, ".bash_history": true, ".python_history": true,
	"id_rsa": true, "id_ed25519": true, "id_ecdsa": true, ".env": true,
}

// validateMediaPath resolves a caller-supplied media path and confirms it lives
// under the allowed root (WHATSAPP_MEDIA_ROOT, default ~/Downloads — a real
// media dir, NOT all of $HOME) and touches none of the sensitive dirs/files.
// Returns the resolved absolute path to use.
func validateMediaPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("invalid media path: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("cannot resolve media path: %v", err)
	}

	root := os.Getenv("WHATSAPP_MEDIA_ROOT")
	if root == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("cannot determine home directory: %v", herr)
		}
		// Default to a real media directory, not the whole home dir — otherwise
		// dotfiles like ~/.git-credentials / ~/.netrc stay readable.
		root = filepath.Join(home, "Downloads")
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootResolved = root
	}

	rel, err := filepath.Rel(rootResolved, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("media path %q is outside the allowed root %q", p, root)
	}

	// Case-fold both sides: macOS (APFS) and Windows resolve paths
	// case-insensitively, so "ID_RSA" or ".SSH" would otherwise slip past an
	// exact-match denylist while pointing at the same file/dir the check means
	// to block.
	sep := string(filepath.Separator)
	resolvedLower := strings.ToLower(resolved)
	for _, deny := range sensitiveDirs {
		denyLower := sep + strings.ToLower(deny)
		if strings.Contains(resolvedLower, denyLower+sep) || strings.HasSuffix(resolvedLower, denyLower) {
			return "", fmt.Errorf("media path in a sensitive location (%s) is not allowed", deny)
		}
	}
	if sensitiveNames[strings.ToLower(filepath.Base(resolved))] {
		return "", fmt.Errorf("media path points at a sensitive file (%s) and is not allowed", filepath.Base(resolved))
	}
	return resolved, nil
}
