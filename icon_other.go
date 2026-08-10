//go:build !windows

package main

import _ "embed"

// macOS and Linux both decode the icon as a regular image. Linux in particular
// cannot read .ico at all — the StatusNotifierItem icon is sent as raw pixels
// decoded by image.Decode, which only has PNG/JPEG/GIF registered — so shipping
// the .ico there left the tray with no icon at all.
//
//go:embed assets/tray_icon.png
var iconData []byte
