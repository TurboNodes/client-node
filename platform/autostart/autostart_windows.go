package autostart

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func EnableAutoStart() error {
	return setRunKey("Turbo Node", AutostartFlag)
}

// EnableHeadlessAutoStart registers turbod to start with the user's session.
//
// Windows is not a target of the website's terminal install command (that
// script is POSIX shell, and Windows already gets autostart from its
// installer), so nothing reaches this today. It exists so the headless
// entrypoint compiles and behaves sensibly on every platform rather than
// growing a build-tagged hole.
//
// Its own Run value, not the desktop app's: registering under the same name
// would silently replace whichever was installed first.
//
// No AutostartFlag either: turbod parses its flags strictly, and it has no UI
// that a launch it asked for itself would need to keep closed.
func EnableHeadlessAutoStart() error {
	return setRunKey("Turbo Node (headless)")
}

// setRunKey points a per-user Run entry at this executable. HKCU needs no
// elevation, unlike a service or an HKLM entry.
func setRunKey(valueName string, args ...string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Quoted: a Run value is a command line, and the install path routinely
	// contains spaces (C:\Program Files\...). Quoted by hand rather than with
	// strconv.Quote, which would escape every backslash in the path.
	command := `"` + exePath + `"`
	for _, arg := range args {
		command += " " + arg
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	err = key.SetStringValue(valueName, command)
	if err != nil {
		return fmt.Errorf("failed to set registry value: %w", err)
	}

	return nil
}
