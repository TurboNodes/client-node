module client

go 1.25.0

require (
	fyne.io/systray v1.12.2
	github.com/Microsoft/go-winio v0.6.2
	github.com/quic-go/quic-go v0.60.0
	golang.org/x/mod v0.35.0
	golang.org/x/sys v0.45.0
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/wailsapp/go-webview2 v1.0.22 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/net v0.55.0 // indirect
)

// fyne.io/systray has no way to hide the tray icon without tearing down the
// systray, which "stealth mode" needs. Vendored with a SetVisible(bool) added
// for darwin/windows/linux: third_party/fyne-systray
replace fyne.io/systray => ./third_party/fyne-systray
