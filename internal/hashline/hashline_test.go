package hashline

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncode(t *testing.T) {
	// Same content should produce the same hash.
	h1 := Encode("func hello() {")
	h2 := Encode("func hello() {")
	require.Equal(t, h1, h2)
	require.Len(t, h1, 4)

	// Different content should (almost certainly) produce different hashes.
	h3 := Encode("func world() {")
	require.NotEqual(t, h1, h3)

	// Empty string should produce a consistent hash.
	h4 := Encode("")
	require.Len(t, h4, 4)

	h5 := Encode("")
	require.Equal(t, h4, h5)
}

func TestEncode_AlphabetOnly(t *testing.T) {
	for i := 0; i < 100; i++ {
		h := Encode(string(rune(i)))
		for _, c := range h {
			require.Contains(t, hashAlphabet, string(c), "hash character %c not in alphabet", c)
		}
	}
}

func TestParseLineID(t *testing.T) {
	tests := []struct {
		input    string
		want     *LineID
		wantNull bool
	}{
		{"15#VKMB", &LineID{LineNumber: 15, Hash: "VKMB"}, false},
		{"1#ABXY", &LineID{LineNumber: 1, Hash: "ABXY"}, false},
		{"1000#ZZQQ", &LineID{LineNumber: 1000, Hash: "ZZQQ"}, false},
		{"", nil, true},
		{"15", nil, true},
		{"#VKMB", nil, true},
		{"abc#VKMB", nil, true},
		{"15#VKMB#extra", nil, true},
		{"15#ABC", nil, true},   // 3 chars — wrong length
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLineID(tt.input)
			if tt.wantNull {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, tt.want.LineNumber, got.LineNumber)
				require.Equal(t, tt.want.Hash, got.Hash)
			}
		})
	}
}

func TestLineID_String(t *testing.T) {
	lid := LineID{LineNumber: 15, Hash: "VKMB"}
	require.Equal(t, "15#VKMB", lid.String())
}

func TestAnnotateLine(t *testing.T) {
	result := AnnotateLine(15, "func hello() {")
	require.Contains(t, result, "15#")
	require.Contains(t, result, "|func hello() {")
	// The hash should be 4 chars between # and |
	parts := strings.Split(result, "#")
	require.Len(t, parts, 2)
	hash := parts[1][:4]
	require.Len(t, hash, 4)
	require.Equal(t, Encode("func hello() {"), hash)
}

func TestAnnotateLines(t *testing.T) {
	content := "line1\nline2\nline3"
	result := AnnotateLines(content, 1)
	lines := strings.Split(result, "\n")
	require.Len(t, lines, 3)
	for i, line := range lines {
		require.Contains(t, line, fmt.Sprintf("%d#", i+1))
		require.Contains(t, line, fmt.Sprintf("|line%d", i+1))
	}
}

func TestVerifyLine(t *testing.T) {
	content := "func hello() {"
	hash := Encode(content)
	require.True(t, VerifyLine(content, hash))
	require.False(t, VerifyLine("different content", hash))
	require.False(t, VerifyLine(content, "AAAA"))
	require.False(t, VerifyLine(content, "A"))
	require.False(t, VerifyLine(content, "AAA"))
	require.False(t, VerifyLine(content, "AAAAA"))
}

func TestRoundTrip(t *testing.T) {
	contents := []string{
		"",
		"single line",
		"line1\nline2\nline3",
		"  indented line",
		"line with\ttab",
	}

	for _, content := range contents {
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lineNum := i + 1
			annotated := AnnotateLine(lineNum, line)
			// Parse the annotated form
			parts := strings.SplitN(annotated, "|", 2)
			require.Len(t, parts, 2)
			require.Equal(t, line, parts[1])
		}
	}
}
