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

func TestPairingBackoffGrows(t *testing.T) {
	// Rounds get further apart: the early ones serve a user who just missed the
	// window, the late ones keep the bridge from hammering the pairing endpoint
	// into a "Can't link new devices right now" throttle.
	prev := time.Duration(0)
	for cycle := 1; cycle <= 8; cycle++ {
		got := pairingBackoff(cycle)
		if got < prev {
			t.Fatalf("backoff shrank at round %d: %s after %s", cycle, got, prev)
		}
		prev = got
	}
	if pairingBackoff(8) < time.Minute {
		t.Fatalf("late rounds are too eager: %s", pairingBackoff(8))
	}
}

// The regression this whole state machine exists for: a code past its TTL is
// dead, and the page must never be handed one to render. Before this, the last
// code of a round stayed in the state through the entire backoff — up to three
// minutes of a QR on screen that could not scan, under a countdown pinned at
// zero, while the phone blamed the user's connection.
func TestExpiredCodeIsNotOffered(t *testing.T) {
	s := newPairingState()
	s.setQR("2@abc", 40*time.Millisecond)

	snap, code := s.snapshot()
	if !snap.HasCode || snap.Expired || code == "" {
		t.Fatalf("a fresh code must be offered: %+v / %q", snap, code)
	}

	time.Sleep(60 * time.Millisecond)

	snap, code = s.snapshot()
	if !snap.Expired {
		t.Fatal("a code past its TTL must report as expired")
	}
	if snap.HasCode || code != "" {
		t.Fatalf("expired state still offers a code: %+v / %q", snap, code)
	}
}

// Between rounds there is no code at all, and the page needs to know how long
// the wait is so it can offer to cut it short instead of looking hung.
func TestWaitingClearsCodesAndCountsDown(t *testing.T) {
	s := newPairingState()
	s.setQR("2@abc", time.Minute)
	s.setPairCode("ABCD1234")
	s.setSocketLive(true)

	s.setWaiting(time.Now().Add(30 * time.Second))

	snap, code := s.snapshot()
	if snap.HasCode || code != "" || snap.PairCode != "" {
		t.Fatalf("waiting state still offers a code: %+v / %q", snap, code)
	}
	if snap.RetryInMS <= 0 || snap.RetryInMS > 30_000 {
		t.Fatalf("retry countdown = %dms, want (0, 30000]", snap.RetryInMS)
	}
	// PairPhone needs the login socket, which is gone between rounds.
	if snap.CanPairPhone {
		t.Fatal("no live socket between rounds, so the phone path must be closed")
	}
}

// A queued request is as good as two: the handlers must never block on a user
// leaning on a button, and the loop only needs to know that it was asked.
func TestPairingRequestsAreNonBlocking(t *testing.T) {
	s := newPairingState()

	if !s.requestRetry() {
		t.Fatal("first retry request should be accepted")
	}
	if s.requestRetry() {
		t.Fatal("second retry request should coalesce, not block")
	}
	if got := <-s.retryReq; got != struct{}{} {
		t.Fatal("retry request never reached the loop")
	}

	if !s.requestPhoneCode("573001234567") {
		t.Fatal("first phone request should be accepted")
	}
	if s.requestPhoneCode("573001234567") {
		t.Fatal("second phone request should coalesce, not block")
	}
	if got := <-s.phoneReq; got != "573001234567" {
		t.Fatalf("phone request = %q, want 573001234567", got)
	}
}

func TestNormalizePairPhone(t *testing.T) {
	// People paste what WhatsApp shows them; the API wants bare digits.
	cases := map[string]string{
		"+57 300 123 4567":    "573001234567",
		"57-300-123-4567":     "573001234567",
		"(52) 1 462 5091298":  "5214625091298",
		"573001234567":        "573001234567",
		"":                    "",
		"12345":               "", // too short to be a country code plus a number
		"5730012345678901234": "", // longer than E.164 allows
		"abc":                 "",
	}
	for in, want := range cases {
		if got := normalizePairPhone(in); got != want {
			t.Errorf("normalizePairPhone(%q) = %q, want %q", in, got, want)
		}
	}
}

// The pairing page is the one that has to explain linking, so the markup has to
// actually carry both methods and the brand it is served under. A page that
// silently loses one leaves the agent narrating state it can't see.
func TestPairingPageOffersBothMethodsAndBranding(t *testing.T) {
	for _, want := range []string{
		"panelqr",        // scan path
		"panelphone",     // 8-character-code path
		"/qr/pair-phone", // requested from the page, not from a bridge restart
		"/qr/retry",      // regenerate button
		"Vincular un dispositivo",
		"Vincular con número de teléfono",
	} {
		if !strings.Contains(pairingPageHTML, want) {
			t.Errorf("pairing page lost %q", want)
		}
	}
	if !strings.Contains(pairingPageHTML, payanaLogoSVG) {
		t.Error("pairing page is not carrying the Payana wordmark")
	}
	// Served from a const with no asset pipeline: anything the browser would go
	// fetch is a blank box on a machine that can only reach WhatsApp. The SVG
	// xmlns is a namespace name, not a URL the browser resolves, so it stays.
	for _, fetched := range []string{`src="http`, `src='http`, `href="http`, `href='http`, `src="//`, `href="//`, "@import"} {
		if strings.Contains(pairingPageHTML, fetched) {
			t.Errorf("pairing page fetches an external asset (%s)", fetched)
		}
	}
}

// Exercises the handlers the bridge actually serves, not copies of them.
func TestPairingEndpoints(t *testing.T) {
	saved := bridgeToken
	defer func() { bridgeToken = saved }()
	bridgeToken = "secret"

	state := newPairingState()
	mux := http.NewServeMux()
	registerPairingHandlersOn(mux, state)

	do := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	errorOf := func(w *httptest.ResponseRecorder) string {
		var body struct {
			Error string `json:"error"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		return body.Error
	}

	// Both actions change state, so neither may be reachable by a GET, and both
	// are as token-gated as the rest of the API.
	if w := do(http.MethodGet, "/qr/retry?t=secret"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /qr/retry = %d, want 405", w.Code)
	}
	if w := do(http.MethodPost, "/qr/retry?t=nope"); w.Code != http.StatusUnauthorized {
		t.Errorf("/qr/retry with a bad token = %d, want 401", w.Code)
	}
	if w := do(http.MethodPost, "/qr/pair-phone?t=nope&phone=573001234567"); w.Code != http.StatusUnauthorized {
		t.Errorf("/qr/pair-phone with a bad token = %d, want 401", w.Code)
	}

	// The button reaches the pairing loop.
	if w := do(http.MethodPost, "/qr/retry?t=secret"); w.Code != http.StatusOK || errorOf(w) != "" {
		t.Fatalf("/qr/retry = %d %s", w.Code, w.Body)
	}
	select {
	case <-state.retryReq:
	default:
		t.Fatal("/qr/retry did not reach the pairing loop")
	}

	// A malformed number is rejected before it costs a pairing request.
	if got := errorOf(do(http.MethodPost, "/qr/pair-phone?t=secret&phone=123")); got == "" {
		t.Error("a too-short number should be refused with a reason")
	}

	// PairPhone needs the login socket; without one the page is told to wait
	// rather than handed a button that fails.
	if got := errorOf(do(http.MethodPost, "/qr/pair-phone?t=secret&phone=%2B57+300+123+4567")); got == "" {
		t.Error("with no live socket the phone path should explain itself")
	}

	state.setQR("2@abc", time.Minute)
	state.setSocketLive(true)
	if w := do(http.MethodPost, "/qr/pair-phone?t=secret&phone=%2B57+300+123+4567"); errorOf(w) != "" {
		t.Fatalf("/qr/pair-phone on a live socket: %s", w.Body)
	}
	select {
	case got := <-state.phoneReq:
		if got != "573001234567" {
			t.Fatalf("phone reached the loop as %q, want the normalized digits", got)
		}
	default:
		t.Fatal("/qr/pair-phone did not reach the pairing loop")
	}

	// Once the bridge has given up, the page must not be able to talk it into
	// asking again — that cap is what keeps WhatsApp from throttling linking.
	state.setExhausted()
	if got := errorOf(do(http.MethodPost, "/qr/retry?t=secret")); got == "" {
		t.Error("retry after exhaustion should be refused")
	}
	select {
	case <-state.retryReq:
		t.Fatal("retry after exhaustion still reached the pairing loop")
	default:
	}
}

func TestExhaustedDropsCodes(t *testing.T) {
	s := newPairingState()
	s.setQR("2@abc", 20*time.Second)
	s.setPairCode("ABCD1234")

	s.setExhausted()
	snap, code := s.snapshot()
	if !snap.Exhausted {
		t.Fatal("expected exhausted")
	}
	// A code left on the page after the bridge gave up is a code that will never
	// work — the page has to be able to say so instead of showing it.
	if snap.HasCode || code != "" || snap.PairCode != "" {
		t.Fatalf("exhausted state still offers a code: %+v / %q", snap, code)
	}
	if snap.Linked {
		t.Fatal("exhausted is not linked")
	}
}
