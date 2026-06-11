package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mattn/go-runewidth"
)

// truncateLeft truncates s from the left to fit within maxWidth visual columns,
// prepending ".." if truncation occurs. Shows the end of the string (most specific part).
func truncateLeft(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := ".." + string(runes)
		if runewidth.StringWidth(candidate) <= maxWidth {
			return candidate
		}
		runes = runes[1:]
	}
	return ".."
}

// decodeProjectPath converts a Claude Code encoded project name back to a
// human-readable path. Claude Code encodes by replacing '/' and any non-[a-zA-Z0-9-]
// character (including dots, Chinese chars, spaces) with '-'.
// We resolve ambiguity by matching against the actual filesystem.
func decodeProjectPath(encoded string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return encoded
	}

	// Build the encoded form of the home dir (strip leading /)
	encodedHome := encodePathComponent(home[1:])
	prefix := "-" + encodedHome
	if !strings.HasPrefix(encoded, prefix) {
		return encoded
	}

	suffix := encoded[len(prefix):]
	actual := resolveEncodedSuffix(home, suffix)
	if actual == "" {
		// Fallback: strip home prefix, naive replacement
		return "~" + strings.ReplaceAll(suffix, "-", "/")
	}
	return "~" + actual[len(home):]
}

// encodePathComponent mirrors Claude Code's encoding: only [a-zA-Z0-9-] pass through,
// everything else (slashes, dots, non-ASCII) becomes '-'.
func encodePathComponent(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	return sb.String()
}

// resolveEncodedSuffix walks the filesystem starting at base, matching the
// encoded suffix against actual directory entries to reconstruct the real path.
func resolveEncodedSuffix(base, suffix string) string {
	if suffix == "" {
		return base
	}
	if suffix[0] != '-' {
		return ""
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}

	rest := suffix[1:] // strip leading path-separator dash

	for _, entry := range entries {
		encodedName := encodePathComponent(entry.Name())
		if !strings.HasPrefix(rest, encodedName) {
			continue
		}
		after := rest[len(encodedName):]
		fullPath := filepath.Join(base, entry.Name())
		if after == "" {
			return fullPath
		}
		if after[0] == '-' {
			if result := resolveEncodedSuffix(fullPath, after); result != "" {
				return result
			}
		}
	}

	return ""
}

func copyToClipboard(text string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try xclip first, then xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			return fmt.Errorf("no clipboard utility found (install xclip, xsel, or wl-copy)")
		}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
