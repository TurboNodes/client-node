//go:build !windows && !darwin

package systray

// iconRect has no answer on Linux. A StatusNotifierItem is handed to the
// desktop environment, which draws it wherever it likes and never reports the
// geometry back over the bus, so callers have to fall back to the pointer.
func iconRect() (x, y, w, h float64, ok bool) {
	return 0, 0, 0, 0, false
}
