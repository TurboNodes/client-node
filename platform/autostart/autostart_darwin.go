package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
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
%s    </array>

    <key>RunAtLoad</key>
    <true/>

%s%s</dict>
</plist>
`

// keepAliveAlways is for turbod: a daemon with no UI and no reason to stop,
// which should come straight back however it went down.
const keepAliveAlways = `    <key>KeepAlive</key>
    <true/>
`

// keepAliveOnCrash restarts the job only when it exits badly.
//
// A plain <true/> here is what made the tray app relaunch itself every ten
// seconds forever: launchd restarts the job the moment it exits, the fresh
// process finds the real one already running and exits on purpose, and launchd
// treats that orderly exit as another reason to try again. Every one of those
// short-lived launches asked the running instance to show its popup, so the
// popup opened over and over on its own. Restarting after a crash is still
// worth having — the node is meant to stay connected — but a deliberate exit
// has to stay exited, which is also what the Quit menu item means.
const keepAliveOnCrash = `    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
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
	return writeAndLoadAgent(desktopAgent())
}

// EnableHeadlessAutoStart registers turbod to launch at login and stay up.
//
// LaunchAgents run per user and need no privileges, so this is the same story
// as the desktop app — unlike Linux, where the headless path had to avoid the
// existing system-wide unit's sudo requirement.
func EnableHeadlessAutoStart() error {
	return writeAndLoadAgent(headlessAgent())
}

func desktopAgent() agent {
	return agent{
		plistName:   desktopPlistName,
		label:       desktopLabel,
		args:        []string{AutostartFlag},
		keepAlive:   keepAliveOnCrash,
		logRedirect: desktopLogRedirect,
	}
}

func headlessAgent() agent {
	return agent{
		plistName: headlessPlistName,
		label:     headlessLabel,
		// No AutostartFlag: turbod parses its flags strictly and has no UI to
		// suppress, so the distinction the flag draws does not exist there.
		keepAlive: keepAliveAlways,
	}
}

type agent struct {
	plistName   string
	label       string
	args        []string
	keepAlive   string
	logRedirect string
}

func writeAndLoadAgent(a agent) error {
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

	plistPath := filepath.Join(launchAgentsDir, a.plistName)
	contents := a.render(executable)

	existing, _ := os.ReadFile(plistPath)
	unchanged := string(existing) == contents

	// Nothing to do when the job is already registered exactly like this, and
	// that is the common case: EnableAutoStart runs on every start. Reloading
	// regardless meant restarting the app for no reason every time it started.
	if unchanged && (StartedByService() || jobLoaded(a.label)) {
		return nil
	}

	if !unchanged {
		if err := os.WriteFile(plistPath, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", plistPath, err)
		}
	}

	// launchd is running this very process, so the job it would be asked to
	// reload is the one keeping us alive: unload kills us, and the load meant
	// to follow never runs, which would leave autostart unregistered
	// altogether. The definition on disk is what the next login reads, and
	// writing it was the whole job.
	if StartedByService() {
		return nil
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

// render produces the job definition for this agent running executable.
func (a agent) render(executable string) string {
	return fmt.Sprintf(plistTemplate, a.label,
		programArguments(executable, a.args), a.keepAlive, a.logRedirect)
}

// programArguments renders the executable and its arguments as plist strings.
// An install path is user-chosen text going into an XML document, so it is
// escaped rather than trusted to contain no markup.
func programArguments(executable string, args []string) string {
	var b strings.Builder
	for _, arg := range append([]string{executable}, args...) {
		fmt.Fprintf(&b, "        <string>%s</string>\n", plistEscaper.Replace(arg))
	}
	return b.String()
}

var plistEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// jobLoaded reports whether launchd already knows this label. `launchctl list`
// exits non-zero for a label it has never been given.
func jobLoaded(label string) bool {
	return exec.Command("launchctl", "list", label).Run() == nil
}
