// Package histfile parses shell history files into structured entries.
//
// Two on-disk formats are supported:
//
//   - plain: one command per line, as written by bash with HISTTIMEFORMAT unset.
//     There is no timestamp and no reliable way to tell where a command that
//     spans multiple lines actually ends, since bash writes embedded newlines
//     straight into the file.
//   - zsh extended history: lines of the form ": <start>:<elapsed>;<command>",
//     with embedded newlines in <command> escaped as a trailing backslash
//     followed by a real newline. This format carries a timestamp and can be
//     parsed unambiguously.
//
// By default parsing is strict: any line that doesn't fit the detected format
// is treated as an error, because a silently mangled command is worse than a
// tool that refuses to guess. Pass Options.Lenient to skip malformed lines
// instead of failing, and inspect Result.Skipped to see what was dropped.
package histfile

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Format identifies which on-disk shell history format a file uses.
type Format int

const (
	FormatUnknown Format = iota
	FormatPlain
	FormatZshExtended
)

func (f Format) String() string {
	switch f {
	case FormatPlain:
		return "plain"
	case FormatZshExtended:
		return "zsh-extended"
	default:
		return "unknown"
	}
}

// Entry is a single parsed history command.
type Entry struct {
	// Command is the shell command text, with any escaped internal
	// newlines restored.
	Command string
	// Timestamp is when the command started. It is the zero time when the
	// source format doesn't record one (plain bash history).
	Timestamp time.Time
	// Duration is how long the command ran. It is zero when unknown.
	Duration time.Duration
	// Line is the 1-based line number in the source file where the entry
	// starts.
	Line int
}

// Options controls how Parse behaves.
type Options struct {
	// Lenient makes Parse skip lines it can't make sense of instead of
	// returning an error. Skipped lines are reported in Result.Skipped.
	Lenient bool
}

// SkipReason records why a line was dropped during lenient parsing.
type SkipReason struct {
	Line   int
	Reason string
}

// Result holds the outcome of a successful Parse call.
type Result struct {
	Entries []Entry
	Format  Format
	// Skipped is only ever populated when Options.Lenient is true; in
	// strict mode the first problem aborts parsing with an error instead.
	Skipped []SkipReason
}

var zshHeaderRE = regexp.MustCompile(`^: (\d+):(\d+);(.*)$`)

// Parse reads a shell history file and returns its entries.
//
// The format is auto-detected from the first non-blank line. In strict mode
// (the default), a line that doesn't match the detected format, contains
// invalid UTF-8, or is part of an unterminated multi-line continuation
// causes Parse to return an error naming the offending line. In lenient
// mode those lines are skipped and recorded in Result.Skipped instead.
func Parse(r io.Reader, opts Options) (*Result, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, fmt.Errorf("histfile: reading input: %w", err)
	}

	res := &Result{Format: detectFormat(lines)}

	var parseErr error
	switch res.Format {
	case FormatZshExtended:
		parseErr = parseZshExtended(lines, opts, res)
	default:
		res.Format = FormatPlain
		parseErr = parsePlain(lines, opts, res)
	}
	if parseErr != nil {
		return nil, parseErr
	}
	return res, nil
}

func readLines(r io.Reader) ([][]byte, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var lines [][]byte
	for scanner.Scan() {
		line := scanner.Bytes()
		cp := make([]byte, len(line))
		copy(cp, line)
		lines = append(lines, cp)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func detectFormat(lines [][]byte) Format {
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if zshHeaderRE.Match(line) {
			return FormatZshExtended
		}
		return FormatPlain
	}
	return FormatPlain
}

func parsePlain(lines [][]byte, opts Options, res *Result) error {
	for idx, line := range lines {
		lineNo := idx + 1
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if !utf8.Valid(line) {
			if !opts.Lenient {
				return fmt.Errorf("histfile: line %d: invalid UTF-8 (use --lenient to skip)", lineNo)
			}
			res.Skipped = append(res.Skipped, SkipReason{Line: lineNo, Reason: "invalid UTF-8"})
			continue
		}
		res.Entries = append(res.Entries, Entry{
			Command: string(line),
			Line:    lineNo,
		})
	}
	return nil
}

func parseZshExtended(lines [][]byte, opts Options, res *Result) error {
	i := 0
	for i < len(lines) {
		line := lines[i]
		lineNo := i + 1

		if len(bytes.TrimSpace(line)) == 0 {
			i++
			continue
		}

		if !utf8.Valid(line) {
			if !opts.Lenient {
				return fmt.Errorf("histfile: line %d: invalid UTF-8 (use --lenient to skip)", lineNo)
			}
			res.Skipped = append(res.Skipped, SkipReason{Line: lineNo, Reason: "invalid UTF-8"})
			i++
			continue
		}

		m := zshHeaderRE.FindSubmatch(line)
		if m == nil {
			if !opts.Lenient {
				return fmt.Errorf("histfile: line %d: does not match zsh extended history format (use --lenient to skip malformed lines)", lineNo)
			}
			res.Skipped = append(res.Skipped, SkipReason{Line: lineNo, Reason: "does not match zsh extended history format"})
			i++
			continue
		}

		startSec, errStart := strconv.ParseInt(string(m[1]), 10, 64)
		elapsedSec, errElapsed := strconv.ParseInt(string(m[2]), 10, 64)
		if errStart != nil || errElapsed != nil {
			if !opts.Lenient {
				return fmt.Errorf("histfile: line %d: malformed timestamp or duration (use --lenient to skip)", lineNo)
			}
			res.Skipped = append(res.Skipped, SkipReason{Line: lineNo, Reason: "malformed timestamp or duration"})
			i++
			continue
		}

		cmd := string(m[3])
		next := i + 1
		truncated := false

		for strings.HasSuffix(cmd, `\`) {
			if next >= len(lines) {
				if !opts.Lenient {
					return fmt.Errorf("histfile: line %d: unterminated multi-line continuation (use --lenient to skip)", lineNo)
				}
				res.Skipped = append(res.Skipped, SkipReason{Line: lineNo, Reason: "unterminated multi-line continuation"})
				truncated = true
				break
			}
			cont := lines[next]
			if !utf8.Valid(cont) {
				if !opts.Lenient {
					return fmt.Errorf("histfile: line %d: invalid UTF-8 in continuation (use --lenient to skip)", next+1)
				}
				res.Skipped = append(res.Skipped, SkipReason{Line: lineNo, Reason: "invalid UTF-8 in continuation"})
				truncated = true
				next++
				break
			}
			cmd = strings.TrimSuffix(cmd, `\`) + "\n" + string(cont)
			next++
		}

		if !truncated {
			res.Entries = append(res.Entries, Entry{
				Command:   cmd,
				Timestamp: time.Unix(startSec, 0),
				Duration:  time.Duration(elapsedSec) * time.Second,
				Line:      lineNo,
			})
		}
		i = next
	}
	return nil
}
