package ui

import (
	"client/quic"
	_ "embed"
	"encoding/json"
	"log"
	"sync"
	"time"

	"fyne.io/systray"
)

//go:embed assets/popup.html
var popupHTML string

// dashboardURL is where the connected view's "Open Dashboard" button sends
// the user's browser.
const dashboardURL = "https://turbo-node.vercel.app/dashboard"

// pairedAutoHideDelay is how long the popup stays open on its own after
// popping up to announce a fresh pairing, if the user never clicks or hovers
// it — same idea as a notification toast.
const pairedAutoHideDelay = 8 * time.Second

// View names understood by the embedded UI.
const (
	ViewPairing    = "pairing"
	ViewConnecting = "connecting"
	ViewConnected  = "connected"
)

// State is the whole UI model. It is marshalled to JSON and handed to
// window.turbo.setState on every change — the UI is a pure function of it,
// so there is no incremental patching to keep in sync.
type State struct {
	View         string               `json:"view"`
	Hosts        []quic.HostProbeView `json:"hosts"`
	Connecting   bool                 `json:"connecting"`
	TotalRewards string               `json:"totalRewards"`
}

var (
	stateMu      sync.Mutex
	currentState State

	// publishSignal is the "UI is stale" flag; see requestPublish.
	publishSignal = make(chan struct{}, 1)

	// onQuit is supplied by the tray so the popup's Quit action and the tray
	// menu take exactly the same path out.
	onQuit func()
	onHide func()

	// autoHideMu guards the pending auto-hide timer armed after a just-paired
	// auto-show. There is at most one at a time.
	autoHideMu    sync.Mutex
	autoHideTimer *time.Timer
)

// StartPopup builds the popup window and starts mirroring quic state into it.
// quit and hide are invoked by the corresponding actions from the UI.
func StartPopup(quit, hide func()) {
	onQuit = quit
	onHide = hide

	initPopup(popupHTML)
	watchQuicState()
	quic.OnJustPaired(showPopupForPairing)
}

// TogglePopup shows the popup anchored to the tray icon, or hides it if shown.
func TogglePopup() {
	cancelAutoHide()
	refreshAnchor()
	togglePopup()
}

// ShowPopup shows the popup if it is not already visible.
func ShowPopup() {
	cancelAutoHide()
	refreshAnchor()
	showPopup()
}

// showPopupForPairing opens the popup to announce a just-completed pairing —
// the one time this happens without the user having clicked anything — and
// arms an auto-hide so a notification nobody is reading does not just sit
// there forever. Any hover or click on the popup, from handlePopupAction,
// cancels it: past that point it behaves like any other manual open.
func showPopupForPairing() {
	ShowPopup()
	armAutoHide(pairedAutoHideDelay)
}

func armAutoHide(d time.Duration) {
	autoHideMu.Lock()
	defer autoHideMu.Unlock()
	if autoHideTimer != nil {
		autoHideTimer.Stop()
	}
	autoHideTimer = time.AfterFunc(d, func() {
		autoHideMu.Lock()
		autoHideTimer = nil
		autoHideMu.Unlock()
		HidePopup()
	})
}

func cancelAutoHide() {
	autoHideMu.Lock()
	defer autoHideMu.Unlock()
	if autoHideTimer != nil {
		autoHideTimer.Stop()
		autoHideTimer = nil
	}
}

// refreshAnchor re-reads where the tray icon currently is, right before the
// popup is placed. The icon slides along the menu bar or taskbar as other
// icons appear and disappear, so a position captured at startup would be
// stale by the time it mattered.
func refreshAnchor() {
	x, y, w, _, ok := systray.IconRect()
	if !ok {
		setPopupAnchor(0, 0, false)
		return
	}
	setPopupAnchor(x+w/2, y, true)
}

// HidePopup hides the popup if it is visible.
func HidePopup() {
	cancelAutoHide()
	hidePopup()
}

// watchQuicState subscribes to every quic signal the three views need and
// republishes a full State snapshot whenever any of them moves.
//
// The listeners only raise a flag. quic invokes them while holding the lock
// that guards the very state a snapshot has to read (see the comment in
// quic/host_status.go), so reading it from inside a listener deadlocks the
// caller against itself — which froze every probe goroutine and with them the
// whole connect loop. Rebuilding happens on its own goroutine instead, which
// also coalesces bursts of per-host updates into a single push to the webview.
func watchQuicState() {
	go publishLoop()

	quic.OnConnectionChange(func(bool) { requestPublish() })
	quic.OnConnectingChange(func(bool) { requestPublish() })
	quic.OnHostsChange(func([]quic.HostProbeView) { requestPublish() })
	quic.OnPairingChange(func(status quic.PairingStatus, url string) {
		requestPublish()
		if status == quic.PairingNeeded && url != "" {
			openPairingURL(url)
		}
	})
	quic.OnRewardsChange(func(string) { requestPublish() })

	requestPublish()
}

// openPairingURL opens a freshly minted pairing link. Every non-empty
// pairing_url the client ever receives is a direct, one-to-one response to a
// RequestPairing call — the server never sends a real link unprompted — so it
// is safe to open it automatically here rather than routing back through
// another click.
func openPairingURL(url string) {
	if err := OpenURL(url); err != nil {
		log.Println("popup: opening pairing URL:", err)
		return
	}
	// The browser now has the pairing page; leaving the popup open just
	// blocks it from re-anchoring under the icon on the next click.
	HidePopup()
}

// requestPublish marks the UI stale. Never blocks: a pending rebuild already
// covers any number of changes queued behind it, since each one republishes
// the whole state anyway.
func requestPublish() {
	select {
	case publishSignal <- struct{}{}:
	default:
	}
}

func publishLoop() {
	for range publishSignal {
		publishState()
	}
}

// resolveView picks the view for the current quic state.
//
// Pairing wins over connectivity: once the server has asked this node to pair,
// showing "connecting" instead would hide the only action the user can take.
// Otherwise a live connection with a paired node is the connected view, and
// everything else (dialling, retrying, waiting for the server to report in)
// is the connecting view.
func resolveView() string {
	if quic.Status() == quic.PairingNeeded {
		return ViewPairing
	}
	if quic.IsConnected() && quic.Status() == quic.PairingDone {
		return ViewConnected
	}
	return ViewConnecting
}

func publishState() {
	state := State{
		View:         resolveView(),
		Hosts:        quic.HostStatuses(),
		Connecting:   quic.IsConnecting(),
		TotalRewards: quic.TotalRewards(),
	}

	stateMu.Lock()
	currentState = state
	stateMu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		log.Println("popup: encoding state:", err)
		return
	}
	setPopupState(string(data))
}

// CurrentStateJSON returns the latest state as JSON. The popup asks for this
// once its page has loaded, since state changes emitted before then were
// pushed into a webview that had nothing listening yet.
func CurrentStateJSON() string {
	stateMu.Lock()
	state := currentState
	stateMu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// handlePopupAction dispatches an action name sent by the UI. Called from the
// platform webview bridges, on whatever thread they use.
func handlePopupAction(action string) {
	// Any real interaction — a hover or a click on something — cancels a
	// pending just-paired auto-hide. "ready" is the page announcing it
	// finished loading, not the user doing anything, so it does not count.
	if action != "ready" {
		cancelAutoHide()
	}

	switch action {
	case "hover":
		// No further action: cancelling the timer above is the whole point.

	case "pair":
		// The link is minted on demand and arrives asynchronously — see
		// openPairingURL, which opens it the moment it lands. Nothing to do
		// here but ask.
		quic.RequestPairing()

	case "dashboard":
		if err := OpenURL(dashboardURL); err != nil {
			log.Println("popup: opening dashboard:", err)
			return
		}
		HidePopup()

	case "retry":
		quic.RequestReconnect()

	case "close":
		// The popup's own close control: dismiss the window, nothing more. Not
		// stealth (which also removes the tray icon) and not quit.
		HidePopup()

	case "hide":
		if onHide != nil {
			onHide()
		}

	case "quit":
		if onQuit != nil {
			onQuit()
		}

	case "reveal":
		// A relaunch asked the running instance to surface (macOS reopen, or a
		// second process handing off through the single-instance channel).
		RevealFromStealth()

	case "ready":
		// The page finished loading and may have missed earlier pushes.
		requestPublish()

	default:
		log.Println("popup: unknown action:", action)
	}
}
