// Package hashline implements line-content hashing for the Hashline edit tool.
//
// Every line displayed by the View tool is annotated with a content hash:
//
//	 15#VK|func hello() {
//	 16#XJ|  return "world"
//	 17#MB|}
//
// When the agent edits via hashline_edit, it references the LINE#ID tag.
// The edit is only applied if the content hash still matches, which proves
// the line hasn't changed since the agent last read it. This eliminates
// stale-line errors without requiring the agent to reproduce exact old content.
package hashline

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// hashAlphabet is the 16-character alphabet used for hash encoding.
// These characters are chosen to be unambiguous and readable.
const hashAlphabet = "ZPMQVRWSNKTXJBYH"

// Encode returns a 4-character hash line identifier for the given line content.
// The hash is based on FNV-1a 64-bit, mapped to a 16-char alphabet for
// compact, readable identifiers (65,536 possible values, ~0.0015% collision rate).
func Encode(content string) string {
	h := fnv.New64a()
	h.Write([]byte(content))
	sum := h.Sum64()
	// Take lower 16 bits and encode as 4 chars (4 bits per char = 16 chars)
	c1 := hashAlphabet[(sum>>12)&0xF]
	c2 := hashAlphabet[(sum>>8)&0xF]
	c3 := hashAlphabet[(sum>>4)&0xF]
	c4 := hashAlphabet[sum&0xF]
	return string([]byte{c1, c2, c3, c4})
}

// LineID represents a parsed LINE#ID reference.
type LineID struct {
	LineNumber int    // 1-based line number
	Hash       string // The 4-char content hash
}

// ParseLineID parses a LINE#ID string like "15#VK".
// Returns nil if the format is invalid.
func ParseLineID(s string) *LineID {
	parts := strings.SplitN(s, "#", 2)
	if len(parts) != 2 {
		return nil
	}
	var lineNum int
	if _, err := fmt.Sscanf(parts[0], "%d", &lineNum); err != nil {
		return nil
	}
	if len(parts[1]) != 4 {
		return nil
	}
	return &LineID{
		LineNumber: lineNum,
		Hash:       parts[1],
	}
}

// String returns the LINE#ID format.
func (l LineID) String() string {
	return fmt.Sprintf("%d#%s", l.LineNumber, l.Hash)
}

// AnnotateLine adds a hash tag to a single line for display.
func AnnotateLine(lineNumber int, content string) string {
	hash := Encode(content)
	return fmt.Sprintf("%6d#%s|%s", lineNumber, hash, content)
}

// AnnotateLines adds hash tags to all lines in a content block.
// startLine is the 1-based line number of the first line.
func AnnotateLines(content string, startLine int) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	var result []string
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		lineNum := i + startLine
		result = append(result, AnnotateLine(lineNum, line))
	}

	return strings.Join(result, "\n")
}

// VerifyLine checks that a line's content matches the expected hash.
// Returns true if the hash is valid for the given content.
func VerifyLine(content, expectedHash string) bool {
	if len(expectedHash) != 4 {
		return false
	}
	return Encode(content) == expectedHash
}
