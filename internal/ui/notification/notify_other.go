//go:build !darwin

package notification

import "github.com/gen2brain/beeep"

// platformNotify sends a desktop notification. On non-macOS platforms
// click-to-focus is not wired, so this delegates to beeep.
func platformNotify(title, message string, icon any) error {
	return beeep.Notify(title, message, icon)
}
