package autostart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The desktop job must not come back after a clean exit. A KeepAlive of
// <true/> restarted it after every deliberate quit, including the immediate
// exit of a duplicate launch, which turned into a relaunch every ten seconds
// with the popup opening each time.
func TestDesktopAgentRestartsOnlyAfterCrash(t *testing.T) {
	plist := desktopAgent().render("/Applications/Turbo.app/Contents/MacOS/Turbo")

	if !strings.Contains(plist, "<key>SuccessfulExit</key>") {
		t.Errorf("desktop job has no SuccessfulExit condition:\n%s", plist)
	}
	if strings.Contains(plist, "<key>KeepAlive</key>\n    <true/>") {
		t.Errorf("desktop job restarts after a clean exit:\n%s", plist)
	}
}

// A launch by launchd carries the flag; StartedByService reads it back, and
// that is what keeps such a launch from asking the running instance to open
// its popup.
func TestDesktopAgentPassesAutostartFlag(t *testing.T) {
	plist := desktopAgent().render("/Applications/Turbo.app/Contents/MacOS/Turbo")

	if !strings.Contains(plist, "<string>"+AutostartFlag+"</string>") {
		t.Errorf("desktop job does not pass %s:\n%s", AutostartFlag, plist)
	}
}

// turbod is a daemon: it should always come back, and it rejects flags it does
// not know.
func TestHeadlessAgentAlwaysRestartsAndTakesNoFlags(t *testing.T) {
	plist := headlessAgent().render("/usr/local/bin/turbod")

	if !strings.Contains(plist, "<key>KeepAlive</key>\n    <true/>") {
		t.Errorf("headless job does not always restart:\n%s", plist)
	}
	if strings.Contains(plist, AutostartFlag) {
		t.Errorf("headless job passes %s, which turbod rejects:\n%s", AutostartFlag, plist)
	}
}

// An install path is arbitrary text going into an XML document.
func TestRenderEscapesExecutablePath(t *testing.T) {
	plist := desktopAgent().render("/Users/a&b/Turbo")

	if strings.Contains(plist, "a&b") {
		t.Errorf("path not escaped:\n%s", plist)
	}
	if !strings.Contains(plist, "a&amp;b") {
		t.Errorf("path escaped wrongly:\n%s", plist)
	}
}

// Whatever the rest of this file asserts about the contents, launchd has to be
// able to read them.
func TestRenderedPlistsAreValid(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil unavailable")
	}

	for name, a := range map[string]agent{"desktop": desktopAgent(), "headless": headlessAgent()} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), a.plistName)
			if err := os.WriteFile(path, []byte(a.render("/usr/local/bin/Turbo")), 0o644); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
				t.Errorf("plutil -lint: %v: %s", err, out)
			}
		})
	}
}

func TestStartedByService(t *testing.T) {
	args := os.Args
	t.Cleanup(func() { os.Args = args })

	os.Args = []string{"Turbo"}
	if StartedByService() {
		t.Error("a plain launch reported as started by the service manager")
	}

	os.Args = []string{"Turbo", AutostartFlag}
	if !StartedByService() {
		t.Error("a launch carrying " + AutostartFlag + " reported as user-initiated")
	}
}
