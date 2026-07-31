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
//   - The window never closes. When the codes run out we reconnect and request a
//     fresh set, so the bridge keeps offering a live code until the account is
//     linked instead of going quiet and bailing out three minutes later.
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
	mu       sync.Mutex
	qrCode   string
	issued   time.Time
	ttl      time.Duration
	pairCode string
	linked   bool
	cycle    int
}

func newPairingState() *pairingState {
	return &pairingState{}
}

func (s *pairingState) setQR(code string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qrCode, s.issued, s.ttl = code, time.Now(), ttl
}

func (s *pairingState) setPairCode(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairCode = code
}

// setCycle records which round of codes we're on, so the page can show that the
// bridge is still offering codes rather than looking stalled.
func (s *pairingState) setCycle(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cycle = n
}

// setLinked marks the account linked and drops the codes — nothing should serve
// a pairing credential once it can no longer be used.
func (s *pairingState) setLinked() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.linked = true
	s.qrCode, s.pairCode = "", ""
}

type pairingSnapshot struct {
	Linked   bool   `json:"linked"`
	HasCode  bool   `json:"has_code"`
	PairCode string `json:"pair_code,omitempty"`
	AgeMS    int64  `json:"age_ms"`
	TTLMS    int64  `json:"ttl_ms"`
	Cycle    int    `json:"cycle"`
}

func (s *pairingState) snapshot() (pairingSnapshot, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := pairingSnapshot{
		Linked:   s.linked,
		HasCode:  s.qrCode != "",
		PairCode: s.pairCode,
		TTLMS:    s.ttl.Milliseconds(),
		Cycle:    s.cycle,
	}
	if !s.issued.IsZero() {
		snap.AgeMS = time.Since(s.issued).Milliseconds()
	}
	return snap, s.qrCode
}

// pairingURL is the address to hand the user (or `open`) for the QR page. The
// token travels in the query string because a browser can't set headers.
func pairingURL() string {
	return fmt.Sprintf("http://%s:%d/qr?t=%s", bridgeHost, bridgePort, bridgeToken)
}

// authorizePairingRequest gates the pairing page. The header-based guards in
// authorizeRequest don't fit a browser GET, so this checks the one thing that
// matters here — the shared token — and forbids caching or referrer leakage of
// the URL that carries it.
func authorizePairingRequest(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	got := r.URL.Query().Get("t")
	if bridgeToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(bridgeToken)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// registerPairingHandlers exposes the loopback pairing page.
func registerPairingHandlers(state *pairingState) {
	http.HandleFunc("/qr", func(w http.ResponseWriter, r *http.Request) {
		if !authorizePairingRequest(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, pairingPageHTML)
	})

	http.HandleFunc("/qr/status", func(w http.ResponseWriter, r *http.Request) {
		if !authorizePairingRequest(w, r) {
			return
		}
		snap, _ := state.snapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap)
	})

	http.HandleFunc("/qr.png", func(w http.ResponseWriter, r *http.Request) {
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
		logger.Infof("Pairing codes for round %d expired without a scan; requesting a fresh set", cycle)
		client.Disconnect()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
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

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case evt, open := <-qrChan:
			if !open {
				return false, nil
			}
			switch evt.Event {
			case whatsmeow.QRChannelEventCode:
				state.setQR(evt.Code, evt.Timeout)
				// Kept for compatibility with tooling that greps the log for a
				// raw code; the page above is the supported path.
				fmt.Printf("WA_QR_RAW:%s\n", evt.Code)
				printTerminalQR(evt.Code)

				// The linking code has to be requested on a live socket, and the
				// sooner the better: it dies with the same ~160s window as the
				// QRs, so asking on the first code buys the user all of it.
				if phone != "" && !askedForCode {
					askedForCode = true
					code, err := client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, pairClientDisplayName())
					if err != nil {
						logger.Warnf("Could not request a linking code for %s (%v) — the QR page still works", phone, err)
					} else {
						state.setPairCode(code)
						fmt.Printf("WA_PAIR_CODE:%s\n", code)
					}
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
