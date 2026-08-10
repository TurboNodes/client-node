//go:build windows

package main

import _ "embed"

// Windows' notification area takes an .ico directly.
//
//go:embed assets/tray_icon.ico
var iconData []byte
