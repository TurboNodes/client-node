package autostart

import "os"

// AutostartFlag is passed to the executable by every autostart entry this
// package installs, and by nothing else. It is the app's way of telling a
// launch it asked for itself apart from a launch the user asked for.
//
// The distinction matters because a second launch is normally a request to
// surface the UI: the user double-clicked the app, so the running instance
// shows its popup and the new process exits. A service manager starting the
// app — at login, or after a crash — carries no such request, and treating it
// as one is how the popup ended up opening on its own over and over.
const AutostartFlag = "--autostart"

// StartedByService reports whether this process was started by the autostart
// entry rather than by a person.
func StartedByService() bool {
	for _, arg := range os.Args[1:] {
		if arg == AutostartFlag {
			return true
		}
	}
	return false
}
