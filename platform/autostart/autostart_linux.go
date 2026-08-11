package autostart

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// Restart=on-failure, not always: the desktop app exits on purpose when the
// user quits it, and when a second launch hands off to the instance already
// running. Restarting those brings the app back from the dead every few
// seconds — and since each relaunch asks the running instance to show its
// popup, the popup opens on its own, over and over. See AutostartFlag for the
// other half of that fix.
const serviceTemplate = `[Unit]
Description=Turbo
After=network.target

[Service]
ExecStart=/usr/local/bin/Turbo ` + AutostartFlag + `
Restart=on-failure
User=%s
Environment=PATH=/usr/local/bin:/usr/bin
WorkingDirectory=%s

[Install]
WantedBy=multi-user.target
`

func EnableAutoStart() error {
	usr, err := user.Current()
	if err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	os.Link(executable, "/usr/local/bin/Turbo")

	serviceContent := fmt.Sprintf(serviceTemplate, usr.Username, usr.HomeDir)

	err = os.WriteFile("/etc/systemd/system/turbo.service", []byte(serviceContent), 0644)

	err = exec.Command("sudo systemctl daemon-reexec").Run()
	if err != nil {
		return err
	}
	err = exec.Command("sudo systemctl daemon-reload").Run()
	if err != nil {
		return err
	}
	err = exec.Command("sudo systemctl enable turbo.service").Run()
	if err != nil {
		return err
	}
	err = exec.Command("sudo systemctl start turbo.service").Run()
	if err != nil {
		return err
	}

	return nil
}

// headlessServiceName is turbod's own unit, separate from the desktop app's
// system-wide turbo.service above so the two never overwrite each other.
const headlessServiceName = "turbod.service"

// headlessServiceTemplate is a *user* unit, which differs from the system unit
// above in two ways that matter: there is no User= (a user manager already
// runs as that user, and the directive is rejected here), and the install
// target is default.target rather than multi-user.target, which only exists
// for the system manager.
const headlessServiceTemplate = `[Unit]
Description=Turbo node
After=network-online.target

[Service]
ExecStart=%s
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`

// EnableHeadlessAutoStart registers turbod to run in the background and come
// back after a reboot, without ever asking for a password.
//
// EnableAutoStart above installs a system-wide unit: it writes to
// /etc/systemd/system and shells out to sudo. That is fine behind a desktop
// button, where a password prompt has somewhere to appear, but this path runs
// from a piped install script where a prompt would either hang or be swallowed
// — so it stays entirely within the user's own systemd instance, which needs
// no privileges at all.
//
// The one wrinkle is that a user manager normally starts at login and stops at
// logout, which is no good for a machine nobody logs into. `loginctl
// enable-linger` is what makes it start at boot and keep running regardless,
// and a user may enable it for themselves without root.
func EnableHeadlessAutoStart() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// The unit outlives whatever path this process was invoked by.
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	unitDir, err := userUnitDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", unitDir, err)
	}

	unitPath := filepath.Join(unitDir, headlessServiceName)
	unit := fmt.Sprintf(headlessServiceTemplate, executable)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", unitPath, err)
	}

	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w: %s", err, out)
	}
	if out, err := exec.Command("systemctl", "--user", "enable", headlessServiceName).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user enable %s: %w: %s", headlessServiceName, err, out)
	}
	// Restart rather than start: reinstalling over a running node should pick
	// up the new binary, and this covers the not-yet-running case too.
	if out, err := exec.Command("systemctl", "--user", "restart", headlessServiceName).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user restart %s: %w: %s", headlessServiceName, err, out)
	}

	enableLinger()
	return nil
}

// enableLinger asks logind to keep this user's systemd instance running
// outside a login session, so the node survives logout and comes up at boot.
//
// Only a warning if it fails: without it the node still runs now and restarts
// with the session, it just will not come back on its own after a reboot.
// That is a much better outcome than failing an install that has otherwise
// succeeded, and it is the expected result on hosts with no logind at all
// (some containers), where the rest of this still works.
func enableLinger() {
	usr, err := user.Current()
	if err != nil {
		log.Println("autostart: cannot resolve current user for linger:", err)
		return
	}
	if out, err := exec.Command("loginctl", "enable-linger", usr.Username).CombinedOutput(); err != nil {
		log.Printf("autostart: could not enable linger for %s (the node will not "+
			"start on boot until it is enabled): %v: %s", usr.Username, err, out)
	}
}

// userUnitDir is where systemd looks for a user's own units.
func userUnitDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}
