package config

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const logFile = "turbo.log"

// LogPath returns the on-disk path for Turbo's console log.
func LogPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, logFile), nil
}

// logFileEnvVar forces file logging on ("1") or off ("0"), overriding the
// terminal check below.
const logFileEnvVar = "TURBO_LOG_FILE"

// InitLogging points the standard logger at stderr, and additionally at an
// append-only file when there is no terminal to read it.
//
// Running from a terminal is development: the log belongs on screen, and also
// appending it to a file on disk just means the interesting output is in two
// places and the file grows for no one. Launched from Finder, a service
// manager or the packaged app there is nowhere for stderr to go, so the file
// is the only record. Set TURBO_LOG_FILE=1 to force the file on anyway, or 0
// to keep it off.
//
// Returns the log file path, or "" when logging only to the terminal.
func InitLogging() (string, error) {
	if !shouldLogToFile() {
		log.SetOutput(os.Stderr)
		return "", nil
	}

	dir, err := configDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating config dir: %w", err)
	}
	path := filepath.Join(dir, logFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("opening log file: %w", err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return path, nil
}

func shouldLogToFile() bool {
	switch os.Getenv(logFileEnvVar) {
	case "1", "true":
		return true
	case "0", "false":
		return false
	}
	return !stderrIsTerminal()
}

// stderrIsTerminal reports whether stderr is a character device, which is what
// a terminal looks like and what a file or pipe does not.
func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
