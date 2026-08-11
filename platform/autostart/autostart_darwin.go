package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// Desktop and headless installs get separate LaunchAgents. They are different
// programs with different lifecycles — the tray app belongs to a login
// session, turbod does not — and a machine may reasonably have both. Sharing a
// label would mean whichever installed last silently replaced the other.
const (
	desktopPlistName = "me.lished.turbo.plist"
	desktopLabel     = "me.lished.Turbo"

	headlessPlistName = "me.lished.turbod.plist"
	headlessLabel     = "me.lished.turbod"
)

// plistTemplate is kept here rather than read from a file at run time. It used
// to be loaded from ./assets relative to the working directory, which resolves
// only when the process happens to be started from the source tree — so
// enabling autostart failed for every real install, which is precisely when it
// matters.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
 "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>

    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>
%s</dict>
</plist>
`

// desktopLogRedirect keeps the tray app's stdout/stderr going to /tmp, as it
// always has. turbod needs no equivalent: it sets up its own log file under
// the config directory on start (see config.InitLogging), which is a better
// place for it than /tmp and is already where anyone debugging a headless node
// is told to look.
const desktopLogRedirect = `
    <!-- Optional: redirect stdout/stderr -->
    <key>StandardOutPath</key>
    <string>/tmp/Turbo.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/Turbo.err</string>
`

// EnableAutoStart registers the desktop app to launch at login.
func EnableAutoStart() error {
	return writeAndLoadAgent(desktopPlistName, desktopLabel, desktopLogRedirect)
}

// EnableHeadlessAutoStart registers turbod to launch at login and stay up.
//
// LaunchAgents run per user and need no privileges, so this is the same story
// as the desktop app — unlike Linux, where the headless path had to avoid the
// existing system-wide unit's sudo requirement.
func EnableHeadlessAutoStart() error {
	return writeAndLoadAgent(headlessPlistName, headlessLabel, "")
}

func writeAndLoadAgent(plistName, label, logRedirect string) error {
	usr, err := user.Current()
	if err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// launchd needs the real path: a LaunchAgent outlives the symlink or
	// relative invocation that happened to start this process.
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	launchAgentsDir := filepath.Join(usr.HomeDir, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}

	plistPath := filepath.Join(launchAgentsDir, plistName)
	contents := fmt.Sprintf(plistTemplate, label, executable, logRedirect)
	if err := os.WriteFile(plistPath, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", plistPath, err)
	}

	// Reinstalling over an existing install leaves the old job loaded, and
	// load refuses to touch a label that is already there. Drop it first;
	// failing here just means there was nothing loaded, which is the common
	// case.
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	// RunAtLoad in the plist means this both registers the job and starts it.
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load %s: %w", plistPath, err)
	}
	return nil
}
