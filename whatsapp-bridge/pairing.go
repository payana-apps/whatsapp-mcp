package main

// Payana: pairing that doesn't race the user.
//
// WhatsApp hands the client six pairing codes: the first lives 60s, the rest 20s
// each, and when they run out the login socket closes — a ~160s window, after
// which every scan fails with the phone's generic "Check your connection and try
// again". Any flow where a human has to be shown a code and act on it inside 20s
// loses that race routinely, and the failure points at the wrong cause.
//
// Two changes make linking a non-event:
//
//   - The window stays open long enough to be human. When the codes run out we
//     reconnect and request a fresh set, so a live code is on offer for ~20
//     minutes instead of ~160 seconds. Bounded, and with backoff between rounds:
//     asking forever is how WhatsApp comes back with "Can't link new devices
//     right now", a throttle that outlives the session that earned it.
//
//   - The code reaches the user without a middleman. `runPairing` publishes the
//     current QR to a loopback page (`/qr`) that refreshes itself as codes
//     rotate — open once, scan whenever — and, when WHATSAPP_PAIR_PHONE is set,
//     asks WhatsApp for an 8-character linking code that can simply be read out.
//
// The pairing page is token-gated like the rest of the API: a pairing code is a
// credential — whoever scans it links THEIR device to the user's account.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waLog "go.mau.fi/whatsmeow/util/log"
	"rsc.io/qr"
)

// pairingState holds the code currently offered to the user. runPairing writes
// it; the /qr handlers read it.
type pairingState struct {
	mu        sync.Mutex
	qrCode    string
	issued    time.Time
	ttl       time.Duration
	pairCode  string
	pairErr   string
	linked    bool
	exhausted bool
	cycle     int
	// waitingUntil is when the next round of codes is due. It is only set
	// between rounds, which is exactly when there is no live code to show.
	waitingUntil time.Time
	// socketLive reports whether a login socket is open, which is the only
	// window in which WhatsApp will issue an 8-character code.
	socketLive bool

	// phoneReq and retryReq carry requests from the page back into the pairing
	// loop. Buffered and written non-blocking: a user leaning on a button must
	// never block an HTTP handler, and a queued request is as good as two.
	phoneReq chan string
	retryReq chan struct{}
}

func newPairingState() *pairingState {
	return &pairingState{
		phoneReq: make(chan string, 1),
		retryReq: make(chan struct{}, 1),
	}
}

func (s *pairingState) setQR(code string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qrCode, s.issued, s.ttl = code, time.Now(), ttl
	s.waitingUntil = time.Time{}
}

func (s *pairingState) setPairCode(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairCode, s.pairErr = code, ""
}

// setPairErr surfaces a failed 8-character-code request on the page. WhatsApp
// rejects these for reasons the user can act on (a malformed number, a throttle),
// and swallowing that into the log leaves the page looking merely slow.
func (s *pairingState) setPairErr(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairErr = msg
}

// setSocketLive tracks whether a login socket is open. PairPhone only works on
// one, so the page uses this to enable the "get a code for my number" path
// instead of offering a button that would fail.
func (s *pairingState) setSocketLive(live bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.socketLive = live
}

// setCycle records which round of codes we're on, so the page can show that the
// bridge is still offering codes rather than looking stalled.
func (s *pairingState) setCycle(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cycle = n
}

// setWaiting drops the expired code and records when the next round lands.
//
// This is the difference between a page that tells the truth and one that
// lies: a round ends when WhatsApp closes the login socket, and every code it
// issued is dead from that moment. Leaving the last one in place left the page
// rendering a QR that could never scan, under a countdown pinned at zero — the
// user sees a code, the phone says "Check your connection and try again", and
// nothing on screen admits the code is the problem.
func (s *pairingState) setWaiting(until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qrCode, s.pairCode = "", ""
	s.socketLive = false
	s.waitingUntil = until
}

// requestPhoneCode asks the pairing loop for an 8-character code. It reports
// false when a request is already queued, so the page can say so.
func (s *pairingState) requestPhoneCode(phone string) bool {
	select {
	case s.phoneReq <- phone:
		return true
	default:
		return false
	}
}

// requestRetry asks the pairing loop to stop waiting out its backoff and go get
// a fresh set of codes now.
func (s *pairingState) requestRetry() bool {
	select {
	case s.retryReq <- struct{}{}:
		return true
	default:
		return false
	}
}

// setExhausted records that the bridge gave up asking, so the page can say so
// instead of showing a stale code that will never work.
func (s *pairingState) setExhausted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exhausted = true
	s.qrCode, s.pairCode = "", ""
	s.socketLive = false
	s.waitingUntil = time.Time{}
}

// setLinked marks the account linked and drops the codes — nothing should serve
// a pairing credential once it can no longer be used.
func (s *pairingState) setLinked() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.linked = true
	s.qrCode, s.pairCode = "", ""
	s.socketLive = false
	s.waitingUntil = time.Time{}
}

type pairingSnapshot struct {
	Linked bool `json:"linked"`
	// Exhausted means the bridge stopped asking for codes; linking needs a
	// restart. Distinct from "no code yet", which resolves on its own.
	Exhausted bool   `json:"exhausted"`
	HasCode   bool   `json:"has_code"`
	PairCode  string `json:"pair_code,omitempty"`
	PairError string `json:"pair_error,omitempty"`
	AgeMS     int64  `json:"age_ms"`
	TTLMS     int64  `json:"ttl_ms"`
	Cycle     int    `json:"cycle"`
	// Expired is the honest answer to "can I still scan what's on screen?".
	// A code whose TTL has run out is dead even while the bridge still holds
	// it, so the page must stop presenting it as scannable the moment this
	// flips — not when the next round happens to arrive.
	Expired bool `json:"expired"`
	// RetryInMS counts down to the next round while the bridge backs off. Zero
	// when a code is live or when nothing is scheduled.
	RetryInMS int64 `json:"retry_in_ms"`
	// CanPairPhone reports whether an 8-character code can be requested right
	// now — it needs the login socket that only exists inside a round.
	CanPairPhone bool `json:"can_pair_phone"`
}

func (s *pairingState) snapshot() (pairingSnapshot, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := pairingSnapshot{
		Linked:       s.linked,
		Exhausted:    s.exhausted,
		HasCode:      s.qrCode != "",
		PairCode:     s.pairCode,
		PairError:    s.pairErr,
		TTLMS:        s.ttl.Milliseconds(),
		Cycle:        s.cycle,
		CanPairPhone: s.socketLive && !s.linked && !s.exhausted,
	}
	if !s.issued.IsZero() {
		snap.AgeMS = time.Since(s.issued).Milliseconds()
	}
	// A held code past its TTL is not a code. Report it as expired and refuse
	// to hand it out, so the page can't render a QR nobody can scan.
	//
	// A missing TTL is treated as the shortest one WhatsApp uses rather than as
	// "never expires": guessing long here fails open into exactly the bug this
	// guard exists to prevent, and guessing short only costs a redundant
	// refresh.
	code := s.qrCode
	ttl := s.ttl
	if ttl <= 0 {
		ttl = defaultPairingTTL
	}
	if code != "" && time.Since(s.issued) >= ttl {
		snap.Expired, snap.HasCode = true, false
		code = ""
	}
	if !s.waitingUntil.IsZero() {
		if d := time.Until(s.waitingUntil); d > 0 {
			snap.RetryInMS = d.Milliseconds()
		}
	}
	return snap, code
}

// pairingToken authorizes the pairing page only — never /api/send or
// /api/download.
//
// The page's token has to ride in a query string, because a browser navigating
// to a URL can't set a header. That URL then lands in browser history, in
// whatever the user pasted it into, and in the agent transcript that opened it.
// Handing it the bridge token would make every one of those places a copy of
// the credential that sends WhatsApp messages as the user. This one is minted
// per run, grants nothing but linking, and is worthless the moment the account
// is linked.
var pairingToken = mustPairingToken()

func mustPairingToken() string {
	tok, err := newToken()
	if err != nil {
		// Without a token the page would have to be served unauthenticated,
		// and a pairing code is a credential: whoever scans it links THEIR
		// device to this account. Refuse to run instead.
		panic(fmt.Sprintf("could not mint a pairing token: %v", err))
	}
	return tok
}

// pairingURL is the address to hand the user (or `open`) for the QR page.
func pairingURL() string {
	return fmt.Sprintf("http://%s:%d/qr?t=%s", bridgeHost, bridgePort, pairingToken)
}

// authorizePairingRequest gates the pairing page. The header-based guards in
// authorizeRequest don't fit a browser GET, so this checks the one thing that
// matters here — the shared token — and forbids caching or referrer leakage of
// the URL that carries it.
func authorizePairingRequest(w http.ResponseWriter, r *http.Request) bool {
	return authorizePairingMethod(w, r, http.MethodGet)
}

// authorizePairingAction gates the two endpoints the page POSTs to. Same token,
// same headers; only the method differs, because asking for a code changes
// state and has no business being reachable by a GET.
func authorizePairingAction(w http.ResponseWriter, r *http.Request) bool {
	return authorizePairingMethod(w, r, http.MethodPost)
}

func authorizePairingMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != method {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	// The page's own fetch() calls are same-origin and send no Origin header.
	// A cross-origin POST with no custom header is a CORS "simple request" —
	// it executes even though the attacker can't read the reply, so without
	// this a page the user happens to have open could drive pairing. Same
	// guard the REST API applies; the page just can't use the header form.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "Cross-origin requests are not allowed", http.StatusForbidden)
		return false
	}
	got := r.URL.Query().Get("t")
	if pairingToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(pairingToken)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// registerPairingHandlers exposes the loopback pairing page on the default mux,
// which is what the bridge serves from.
func registerPairingHandlers(state *pairingState) {
	registerPairingHandlersOn(http.DefaultServeMux, state)
}

// registerPairingHandlersOn takes the mux explicitly so tests can exercise the
// real handlers. Re-registering on the default mux panics, so without this a
// test could only re-implement each handler and assert against the copy — which
// passes just as happily when the copy and the original have drifted apart.
func registerPairingHandlersOn(mux *http.ServeMux, state *pairingState) {
	mux.HandleFunc("/qr", func(w http.ResponseWriter, r *http.Request) {
		if !authorizePairingRequest(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, pairingPageHTML)
	})

	mux.HandleFunc("/qr/status", func(w http.ResponseWriter, r *http.Request) {
		if !authorizePairingRequest(w, r) {
			return
		}
		snap, _ := state.snapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap)
	})

	// The page asks for a fresh round instead of waiting out the backoff. This
	// is the "regenerate" button: the common case for an expired code is a user
	// who is right there, watching, and would otherwise sit through three
	// minutes of backoff aimed at a user who walked away.
	mux.HandleFunc("/qr/retry", func(w http.ResponseWriter, r *http.Request) {
		if !authorizePairingAction(w, r) {
			return
		}
		snap, _ := state.snapshot()
		w.Header().Set("Content-Type", "application/json")
		if snap.Linked {
			json.NewEncoder(w).Encode(map[string]string{"error": "already linked"})
			return
		}
		if snap.Exhausted {
			// Past the round cap the bridge is deliberately silent; asking
			// again from the page would walk straight into the WhatsApp
			// throttle the cap exists to avoid.
			json.NewEncoder(w).Encode(map[string]string{
				"error": "the bridge stopped asking for codes; restart it when you have the phone at hand",
			})
			return
		}
		// Only meaningful between rounds. Mid-round the loop isn't waiting on
		// anything, so the press would be consumed at the NEXT backoff and
		// cancel it — collapsing the spacing that keeps WhatsApp from
		// throttling this account. The page hides the button here; this
		// refuses it regardless of what the page does.
		if snap.RetryInMS <= 0 {
			json.NewEncoder(w).Encode(map[string]string{
				"error": "a fresh code is already on its way",
			})
			return
		}
		state.requestRetry()
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	// Requesting an 8-character code from the page is what lets the page own
	// the choice of linking method. Claude no longer has to pick one up front
	// and restart the bridge with WHATSAPP_PAIR_PHONE to change its mind.
	mux.HandleFunc("/qr/pair-phone", func(w http.ResponseWriter, r *http.Request) {
		if !authorizePairingAction(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		phone := normalizePairPhone(r.URL.Query().Get("phone"))
		if phone == "" {
			json.NewEncoder(w).Encode(map[string]string{
				"error": "enter your number with country code, digits only",
			})
			return
		}
		snap, _ := state.snapshot()
		if !snap.CanPairPhone {
			json.NewEncoder(w).Encode(map[string]string{
				"error": "no live pairing socket right now — wait for the next code and try again",
			})
			return
		}
		state.requestPhoneCode(phone)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

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
}

func qrPNG(code string) ([]byte, error) {
	c, err := qr.Encode(code, qr.M)
	if err != nil {
		return nil, err
	}
	return c.PNG(), nil
}

// normalizePairPhone reduces what someone types on the page to the digits
// WhatsApp wants. People paste "+57 300 123 4567" and the API wants
// "573001234567"; rejecting that as malformed would be pedantry. Anything left
// that isn't a plausible international number returns empty, so the caller can
// say so instead of burning a pairing request on it.
func normalizePairPhone(raw string) string {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	out := digits.String()
	// Country code plus a subscriber number: shorter is a typo, longer than
	// E.164 allows can't be dialled.
	if len(out) < 8 || len(out) > 15 {
		return ""
	}
	return out
}

// pairClientDisplayName must read as "Browser (OS)" — WhatsApp validates it and
// rejects the pairing request outright when it doesn't.
func pairClientDisplayName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Chrome (Mac OS)"
	case "windows":
		return "Chrome (Windows)"
	default:
		return "Chrome (Linux)"
	}
}

// runPairing links the account, and keeps offering codes until it does. It
// returns nil once WhatsApp confirms the pairing, and an error only for the
// conditions no amount of retrying fixes.
func runPairing(ctx context.Context, client *whatsmeow.Client, logger waLog.Logger, state *pairingState) error {
	phone := strings.TrimSpace(os.Getenv("WHATSAPP_PAIR_PHONE"))

	fmt.Printf("WA_PAIRING_URL:%s\n", pairingURL())
	fmt.Println("Open that URL to scan the QR — it refreshes itself as codes rotate.")
	if phone != "" {
		fmt.Println("Also requesting an 8-character linking code for", phone)
	}

	for cycle := 1; ; cycle++ {
		if client.Store.ID != nil {
			state.setLinked()
			return nil
		}
		state.setCycle(cycle)

		// Drop any retry press left over from an earlier round. The button is
		// only offered between rounds, but a press that raced the arrival of a
		// fresh round would otherwise sit in the buffer and cancel a LATER
		// backoff — turning the eight spaced-out rounds into a burst, which is
		// exactly how WhatsApp decides to throttle linking on this account.
		select {
		case <-state.retryReq:
		default:
		}

		qrChan, err := client.GetQRChannel(ctx)
		if err != nil {
			return fmt.Errorf("could not open the pairing channel: %w", err)
		}
		if err := client.Connect(); err != nil {
			return fmt.Errorf("could not connect to WhatsApp: %w", err)
		}

		linked, err := consumePairingCodes(ctx, client, logger, state, qrChan, phone, cycle)
		if err != nil {
			return err
		}
		if linked {
			state.setLinked()
			return nil
		}

		// The codes ran out and WhatsApp closed the login socket. Upstream gives
		// up here; instead reconnect for a fresh set so a user who arrives late
		// still finds a live code on the page.
		client.Disconnect()
		state.setSocketLive(false)

		// Asking again immediately, forever, is how you get WhatsApp to answer
		// "Can't link new devices right now" — a server-side throttle that no
		// longer has anything to do with this bridge. So back off between rounds
		// and stop after enough of them that nobody is at the keyboard.
		if cycle >= maxPairingRounds {
			state.setExhausted()
			return fmt.Errorf("no one linked the account after %d rounds of pairing codes; "+
				"restart the bridge when you are ready to link (asking WhatsApp for more "+
				"codes right now risks a temporary block on linking new devices)", cycle)
		}
		wait := pairingBackoff(cycle)
		// Drop the dead codes before sleeping. Everything issued this round died
		// with the socket, and the page has to say "expired, fetching a new one"
		// rather than keep a stale QR on screen for up to three minutes.
		state.setWaiting(time.Now().Add(wait))
		logger.Infof("Pairing codes for round %d expired without a scan; asking for a fresh set in %s", cycle, wait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-state.retryReq:
			// Someone is watching the page and pressed the button — skip the
			// wait that exists for the case where nobody is.
			logger.Infof("Fetching a fresh set of pairing codes now (requested from the pairing page)")
		case <-time.After(wait):
		}
	}
}

// defaultPairingTTL is the fallback lifetime for a code whose event carried no
// timeout — the shortest WhatsApp uses, so the guess can only expire a code
// early, never serve a dead one.
const defaultPairingTTL = 20 * time.Second

// maxPairingRounds bounds how long the bridge keeps asking. Six codes plus the
// backoff below is roughly twenty minutes of a live code on the page, which is
// far beyond "I'll grab my phone" and well short of hammering the endpoint.
var maxPairingRounds = getenvIntDefault("WHATSAPP_PAIR_MAX_ROUNDS", 8)

// pairingBackoff spaces out the rounds: the first few come quickly, because the
// common case is a user who is right there and just missed the window.
func pairingBackoff(cycle int) time.Duration {
	switch {
	case cycle <= 2:
		return time.Second
	case cycle <= 4:
		return 15 * time.Second
	case cycle <= 6:
		return time.Minute
	default:
		return 3 * time.Minute
	}
}

// consumePairingCodes drains one round of codes. It reports whether the account
// got linked, and errors only on conditions a retry can't fix.
func consumePairingCodes(
	ctx context.Context,
	client *whatsmeow.Client,
	logger waLog.Logger,
	state *pairingState,
	qrChan <-chan whatsmeow.QRChannelItem,
	phone string,
	cycle int,
) (bool, error) {
	askedForCode := false

	// askPhone requests the 8-character code on the live socket. Shared by the
	// WHATSAPP_PAIR_PHONE path and the page's phone form, so both report the
	// same way — the page needs the failure text, not just the log.
	askPhone := func(number string) {
		askedForCode = true
		code, err := client.PairPhone(ctx, number, true, whatsmeow.PairClientChrome, pairClientDisplayName())
		if err != nil {
			logger.Warnf("Could not request a linking code for %s (%v) — the QR still works", number, err)
			state.setPairErr("WhatsApp rejected that number — check the country code, or scan the QR instead")
			askedForCode = false
			return
		}
		state.setPairCode(code)
		fmt.Printf("WA_PAIR_CODE:%s\n", code)
	}

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()

		case number := <-state.phoneReq:
			// Requested from the page mid-round. The socket is live here, which
			// is the only place PairPhone works.
			askPhone(number)

		case evt, open := <-qrChan:
			if !open {
				return false, nil
			}
			switch evt.Event {
			case whatsmeow.QRChannelEventCode:
				state.setQR(evt.Code, evt.Timeout)
				state.setSocketLive(true)
				// Kept for compatibility with tooling that greps the log for a
				// raw code; the page above is the supported path.
				fmt.Printf("WA_QR_RAW:%s\n", evt.Code)
				printTerminalQR(evt.Code)

				// The linking code has to be requested on a live socket, and the
				// sooner the better: it dies with the same ~160s window as the
				// QRs, so asking on the first code buys the user all of it.
				if phone != "" && !askedForCode {
					askPhone(phone)
				}

			case "success":
				fmt.Println("WA_PAIRED:ok")
				return true, nil

			case "timeout":
				return false, nil

			case "err-client-outdated":
				// No retry helps: WhatsApp refuses this client version outright.
				return false, fmt.Errorf("WhatsApp rejected this client as outdated — the whatsmeow dependency needs a bump")

			case "err-scanned-without-multidevice":
				logger.Warnf("That account still has multi-device disabled; enable it in WhatsApp and scan again")

			case whatsmeow.QRChannelEventError:
				logger.Warnf("Pairing round %d reported an error: %v", cycle, evt.Error)

			default:
				logger.Warnf("Unexpected pairing event %q", evt.Event)
			}
		}
	}
}

// printTerminalQR draws the ASCII QR only for someone watching a real terminal.
// Under the launcher stdout is a log file, where a 40-line block per code buries
// every other line — and the page is the readable path anyway.
func printTerminalQR(code string) {
	info, err := os.Stdout.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return
	}
	fmt.Println("\nScan this QR code with your WhatsApp app:")
	writeTerminalQR(code)
}
