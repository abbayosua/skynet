//go:build darwin

package notification

import (
	"os"
	"os/exec"

	"github.com/gen2brain/beeep"
)

// terminalBundleIDs maps $TERM_PROGRAM to a macOS bundle identifier so a
// clicked notification can activate (focus) the terminal window that ran
// SkyNet.
var terminalBundleIDs = map[string]string{
	"Apple_Terminal": "com.apple.Terminal",
	"iTerm.app":      "com.googlecode.iterm2",
	"WarpTerminal":   "dev.warp.Warp-Stable",
	"vscode":         "com.microsoft.VSCode",
	"WezTerm":        "com.github.wez.wezterm",
	"ghostty":        "com.mitchellh.ghostty",
	"Hyper":          "co.zeit.hyper",
	"Tabby":          "org.tabby",
	"alacritty":      "org.alacritty",
	"kitty":          "net.kovidgoyal.kitty",
}

// terminalBundleID returns the bundle identifier for the current terminal,
// or an empty string when it cannot be determined.
func terminalBundleID() string {
	return terminalBundleIDs[os.Getenv("TERM_PROGRAM")]
}

// platformNotify sends a desktop notification. On macOS it prefers
// terminal-notifier so a click on the notification activates the terminal
// window. When the terminal cannot be identified, or terminal-notifier is
// missing, it falls back to beeep.
func platformNotify(title, message string, icon any) error {
	tn, err := exec.LookPath("terminal-notifier")
	if err != nil {
		return beeep.Notify(title, message, icon)
	}

	args := []string{"-title", title, "-message", message, "-group", "SkyNet"}
	if id := terminalBundleID(); id != "" {
		args = append(args, "-activate", id)
	}

	if err := exec.Command(tn, args...).Run(); err != nil {
		return beeep.Notify(title, message, icon)
	}
	return nil
}
