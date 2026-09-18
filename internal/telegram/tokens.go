package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// tokensFileName is the per-project file (text based, no sqlite) that stores
// the last successfully connected bot token. Only one token is kept: a
// successful connect overwrites it.
const tokensFileName = "telegram_token"

// tokensFilePath resolves the token file under dir. Empty dir means no
// persistence is possible and callers must skip silently.
func tokensFilePath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, tokensFileName)
}

// SaveToken stores token as the last used bot token under dir. Empty token
// or dir is a no-op. The file is created with 0600 permissions.
func SaveToken(dir, token string) {
	token = strings.TrimSpace(token)
	if token == "" || dir == "" {
		return
	}
	path := tokensFilePath(dir)
	if cur, err := LoadToken(dir); err == nil && cur == token {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// LoadToken returns the last used bot token stored under dir, or "" when
// none is saved.
func LoadToken(dir string) (string, error) {
	path := tokensFilePath(dir)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ownerFileName records which session currently holds the bot. One bot can
// only long-poll from one session at a time (Telegram answers concurrent
// getUpdates with 409 Conflict), so a second session connecting with the
// same token must confirm the takeover instead of flip-flopping silently.
const ownerFileName = "telegram_owner"

// Owner describes a running bot claim.
type Owner struct {
	// Username is the bot username from getMe (stable per bot).
	Username string
	// SessionID is the skynet session currently mirroring.
	SessionID string
	// PID is the OS process holding the bot.
	PID int
}

// ownerFilePath resolves the owner file under dir.
func ownerFilePath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ownerFileName)
}

// encodeOwner serializes an owner claim as plain text.
func encodeOwner(o Owner) string {
	return fmt.Sprintf("%s\n%s\n%d\n", o.Username, o.SessionID, o.PID)
}

// decodeOwner parses an owner claim file.
func decodeOwner(data string) (Owner, bool) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) != 3 || lines[0] == "" || lines[1] == "" {
		return Owner{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[2]))
	if err != nil || pid <= 0 {
		return Owner{}, false
	}
	return Owner{Username: lines[0], SessionID: lines[1], PID: pid}, true
}

// LoadOwner returns the current bot claim, if any.
func LoadOwner(dir string) (Owner, bool) {
	path := ownerFilePath(dir)
	if path == "" {
		return Owner{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Owner{}, false
	}
	o, ok := decodeOwner(string(data))
	if !ok {
		return Owner{}, false
	}
	return o, true
}

// ClaimOwner writes the bot claim for this process. It must be called only
// after TakeoverAllowed returned take or free.
func ClaimOwner(dir string, o Owner) {
	path := ownerFilePath(dir)
	if path == "" || o.Username == "" || o.SessionID == "" || o.PID <= 0 {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(encodeOwner(o)), 0o600)
}

// ReleaseOwner drops the claim when it belongs to the given session.
func ReleaseOwner(dir, sessionID string) {
	path := ownerFilePath(dir)
	if path == "" || sessionID == "" {
		return
	}
	if o, ok := LoadOwner(dir); !ok || o.SessionID != sessionID {
		return
	}
	_ = os.Remove(path)
}

// TakeoverResult describes whether a connect may proceed.
type TakeoverResult int

const (
	// TakeoverFree means no live owner claims the bot.
	TakeoverFree TakeoverResult = iota
	// TakeoverSame means the same session already holds the bot.
	TakeoverSame
	// TakeoverConfirm means another live session holds the bot and the
	// user must confirm the takeover.
	TakeoverConfirm
)

// TakeoverAllowed checks whether username may be connected from sessionID.
// A claim is live only while its process is alive; a stale file behaves as
// free. isAlive reports whether pid is a running process.
func TakeoverAllowed(dir, username, sessionID string, isAlive func(int) bool) TakeoverResult {
	if dir == "" || username == "" || sessionID == "" {
		return TakeoverFree
	}
	o, ok := LoadOwner(dir)
	if !ok || o.Username != username {
		return TakeoverFree
	}
	if o.SessionID == sessionID {
		return TakeoverSame
	}
	if isAlive != nil && isAlive(o.PID) {
		return TakeoverConfirm
	}
	return TakeoverFree
}

// ProcessAlive reports whether pid is a currently running process. It uses
// signal 0 so no signal is actually delivered.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := findProcess(pid)
	if err != nil {
		return false
	}
	return signalProcess(proc, 0)
}

// VerifyResult is the outcome of verifying a token for a connect.
type VerifyResult struct {
	// Token is the verified token, trimmed.
	Token string
	// Username is the bot username from getMe.
	Username string
	// Takeover is the owner-claim state for this bot.
	Takeover TakeoverResult
	// OwnerSession is the session currently holding the bot, if any.
	OwnerSession string
	// Err is a human-readable failure, empty on success.
	Err string
}

// VerifyToken validates token against the Telegram API and, on success,
// persists it as the last used token for dir. sessionID is used only to
// evaluate the owner claim; claiming happens in StartTelegramBot.
func VerifyToken(token, dir, sessionID string) VerifyResult {
	token = strings.TrimSpace(token)
	if token == "" {
		return VerifyResult{Err: "token is empty"}
	}
	bot := NewBot(token)
	if !bot.TestToken() {
		return VerifyResult{Err: "Invalid token or network error"}
	}
	username := bot.Username()
	SaveToken(dir, token)
	res := VerifyResult{Token: token, Username: username, Takeover: TakeoverFree}
	if dir == "" || username == "" {
		return res
	}
	res.Takeover = TakeoverAllowed(dir, username, sessionID, ProcessAlive)
	if o, ok := LoadOwner(dir); ok && o.Username == username {
		res.OwnerSession = o.SessionID
	}
	return res
}

// secret is never shown in the dialog.
func MaskedToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	const tail = 4
	if len(token) <= tail+3 {
		return "..." + token
	}
	return "..." + token[len(token)-tail:]
}
