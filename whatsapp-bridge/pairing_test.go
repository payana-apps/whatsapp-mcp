package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A pairing code is a credential: whoever scans it links their own device to the
// user's account. The page that serves it must be no easier to reach than the
// rest of the API, even though a browser GET can't carry the token header.
func TestAuthorizePairingRequest(t *testing.T) {
	saved := bridgeToken
	defer func() { bridgeToken = saved }()

	cases := []struct {
		name   string
		token  string // configured server token
		method string
		query  string
		want   int // 200 means authorized
	}{
		{"rejects POST", "secret", http.MethodPost, "?t=secret", http.StatusMethodNotAllowed},
		{"rejects missing token", "secret", http.MethodGet, "", http.StatusUnauthorized},
		{"rejects wrong token", "secret", http.MethodGet, "?t=nope", http.StatusUnauthorized},
		{"rejects when no token configured", "", http.MethodGet, "?t=anything", http.StatusUnauthorized},
		{"accepts correct token", "secret", http.MethodGet, "?t=secret", http.StatusOK},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bridgeToken = c.token
			w := httptest.NewRecorder()
			ok := authorizePairingRequest(w, httptest.NewRequest(c.method, "/qr"+c.query, nil))
			if c.want == http.StatusOK {
				if !ok {
					t.Fatalf("expected authorized, got status %d", w.Code)
				}
				return
			}
			if ok {
				t.Fatal("expected rejection, got authorized")
			}
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d", w.Code, c.want)
			}
		})
	}
}

// The URL carries the token in the query string, so it must not be cached or
// leak through a Referer header.
func TestPairingResponseHeaders(t *testing.T) {
	saved := bridgeToken
	defer func() { bridgeToken = saved }()
	bridgeToken = "secret"

	w := httptest.NewRecorder()
	authorizePairingRequest(w, httptest.NewRequest(http.MethodGet, "/qr?t=secret", nil))
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
}

func TestPairingStateSnapshot(t *testing.T) {
	s := newPairingState()

	snap, code := s.snapshot()
	if snap.HasCode || snap.Linked || code != "" {
		t.Fatalf("fresh state should offer nothing, got %+v / %q", snap, code)
	}

	s.setQR("2@abc", 20*time.Second)
	s.setPairCode("ABCD1234")
	s.setCycle(3)
	snap, code = s.snapshot()
	if !snap.HasCode || code != "2@abc" {
		t.Fatalf("expected the current QR to be offered, got %+v / %q", snap, code)
	}
	if snap.PairCode != "ABCD1234" || snap.Cycle != 3 || snap.TTLMS != 20000 {
		t.Fatalf("snapshot lost pairing detail: %+v", snap)
	}

	// Once linked, both codes are dead — the state must stop handing them out.
	s.setLinked()
	snap, code = s.snapshot()
	if !snap.Linked {
		t.Fatal("expected linked")
	}
	if snap.HasCode || code != "" || snap.PairCode != "" {
		t.Fatalf("linked state still offers a code: %+v / %q", snap, code)
	}
}

func TestQRPNGHandler(t *testing.T) {
	savedToken := bridgeToken
	defer func() { bridgeToken = savedToken }()
	bridgeToken = "secret"

	state := newPairingState()
	mux := http.NewServeMux()
	// Mirror the /qr.png handler against a local mux so the test doesn't touch
	// the process-wide DefaultServeMux that registerPairingHandlers uses.
	mux.HandleFunc("/qr.png", func(w http.ResponseWriter, r *http.Request) {
		if !authorizePairingRequest(w, r) {
			return
		}
		snap, code := state.snapshot()
		if snap.Linked {
			http.Error(w, "Already linked", http.StatusGone)
			return
		}
		if code == "" {
			http.Error(w, "No pairing code yet", http.StatusServiceUnavailable)
			return
		}
		png, err := qrPNG(code)
		if err != nil {
			http.Error(w, "Could not render the pairing code", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	})

	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/qr.png?t=secret", nil))
		return w
	}

	if w := get(); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("before the first code: status = %d, want 503", w.Code)
	}

	state.setQR("2@abc", 20*time.Second)
	w := get()
	if w.Code != http.StatusOK {
		t.Fatalf("with a live code: status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if body := w.Body.Bytes(); len(body) < 8 || string(body[1:4]) != "PNG" {
		t.Fatal("body is not a PNG")
	}

	state.setLinked()
	if w := get(); w.Code != http.StatusGone {
		t.Fatalf("after linking: status = %d, want 410", w.Code)
	}
}

func TestPairingStatusJSON(t *testing.T) {
	state := newPairingState()
	state.setQR("2@abc", 20*time.Second)
	snap, _ := state.snapshot()

	var decoded pairingSnapshot
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// The page drives its refresh off these two fields; the raw code must never
	// be part of the JSON (it is only ever rendered as a PNG).
	if decoded.TTLMS != 20000 || decoded.AgeMS < 0 {
		t.Fatalf("status JSON lost the rotation timing: %+v", decoded)
	}
	if got := string(raw); strings.Contains(got, "2@abc") {
		t.Fatalf("status JSON leaked the raw pairing code: %s", got)
	}
}

func TestPairClientDisplayName(t *testing.T) {
	// WhatsApp validates the shape and rejects the pairing request outright when
	// it isn't "Browser (OS)".
	name := pairClientDisplayName()
	if !strings.Contains(name, " (") || !strings.HasSuffix(name, ")") {
		t.Fatalf("display name %q is not in the required \"Browser (OS)\" form", name)
	}
}
