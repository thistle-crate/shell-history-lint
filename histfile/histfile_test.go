package histfile

import (
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, input string, opts Options) *Result {
	t.Helper()
	res, err := Parse(strings.NewReader(input), opts)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	return res
}

func mustFail(t *testing.T, input string, opts Options) error {
	t.Helper()
	res, err := Parse(strings.NewReader(input), opts)
	if err == nil {
		t.Fatalf("Parse succeeded with %d entries, want error", len(res.Entries))
	}
	if res != nil {
		t.Errorf("Parse returned a result alongside an error")
	}
	return err
}

func TestFormatString(t *testing.T) {
	cases := map[Format]string{
		FormatPlain:       "plain",
		FormatZshExtended: "zsh-extended",
		FormatUnknown:     "unknown",
	}
	for f, want := range cases {
		if got := f.String(); got != want {
			t.Errorf("Format(%d).String() = %q, want %q", int(f), got, want)
		}
	}
}

func TestParseEmptyInput(t *testing.T) {
	res := mustParse(t, "", Options{})
	if res.Format != FormatPlain {
		t.Errorf("Format = %v, want plain", res.Format)
	}
	if len(res.Entries) != 0 || len(res.Skipped) != 0 {
		t.Errorf("got %d entries and %d skipped, want none", len(res.Entries), len(res.Skipped))
	}
}

func TestParsePlain(t *testing.T) {
	res := mustParse(t, "ls -la\n\ncd /tmp\n   \ngit status\n", Options{})
	if res.Format != FormatPlain {
		t.Fatalf("Format = %v, want plain", res.Format)
	}
	want := []Entry{
		{Command: "ls -la", Line: 1},
		{Command: "cd /tmp", Line: 3},
		{Command: "git status", Line: 5},
	}
	if len(res.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(res.Entries), len(want), res.Entries)
	}
	for i, w := range want {
		if res.Entries[i] != w {
			t.Errorf("entry %d = %+v, want %+v", i, res.Entries[i], w)
		}
		if !res.Entries[i].Timestamp.IsZero() {
			t.Errorf("entry %d has a timestamp, want zero", i)
		}
	}
}

func TestParsePlainNoTrailingNewline(t *testing.T) {
	res := mustParse(t, "one\ntwo", Options{})
	if len(res.Entries) != 2 || res.Entries[1].Command != "two" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestParsePlainKeepsLeadingColon(t *testing.T) {
	// A command that merely starts with a colon is not a zsh header, and
	// must not flip detection to the extended format.
	res := mustParse(t, ": no-op\necho hi\n", Options{})
	if res.Format != FormatPlain {
		t.Fatalf("Format = %v, want plain", res.Format)
	}
	if len(res.Entries) != 2 || res.Entries[0].Command != ": no-op" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestParsePlainInvalidUTF8(t *testing.T) {
	input := "echo ok\necho \xff\xfe\necho fine\n"

	err := mustFail(t, input, Options{})
	if !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("error = %q, want it to name line 2 and UTF-8", err)
	}

	res := mustParse(t, input, Options{Lenient: true})
	if len(res.Entries) != 2 {
		t.Errorf("got %d entries, want 2", len(res.Entries))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Line != 2 {
		t.Errorf("Skipped = %+v, want one skip at line 2", res.Skipped)
	}
}

func TestParseZshExtended(t *testing.T) {
	input := ": 1690000000:0;git status\n: 1690000019:12;sleep 12\n"
	res := mustParse(t, input, Options{})
	if res.Format != FormatZshExtended {
		t.Fatalf("Format = %v, want zsh-extended", res.Format)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(res.Entries))
	}

	first, second := res.Entries[0], res.Entries[1]
	if first.Command != "git status" || first.Timestamp.Unix() != 1690000000 || first.Duration != 0 || first.Line != 1 {
		t.Errorf("first entry = %+v", first)
	}
	if second.Command != "sleep 12" || second.Timestamp.Unix() != 1690000019 || second.Duration != 12*time.Second || second.Line != 2 {
		t.Errorf("second entry = %+v", second)
	}
}

func TestParseZshDetectionSkipsLeadingBlankLines(t *testing.T) {
	res := mustParse(t, "\n  \n: 5:0;ls\n", Options{})
	if res.Format != FormatZshExtended {
		t.Fatalf("Format = %v, want zsh-extended", res.Format)
	}
	if len(res.Entries) != 1 || res.Entries[0].Line != 3 {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestParseZshCommandContainsSeparators(t *testing.T) {
	// Only the first semicolon ends the header; later ones belong to the command.
	res := mustParse(t, ": 10:0;echo a; echo b: c;\n", Options{})
	if len(res.Entries) != 1 || res.Entries[0].Command != "echo a; echo b: c;" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestParseZshEmptyCommand(t *testing.T) {
	res := mustParse(t, ": 10:0;\n: 11:0;ls\n", Options{})
	if len(res.Entries) != 2 || res.Entries[0].Command != "" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestParseZshMultiLine(t *testing.T) {
	input := ": 1:0;echo one\\\ntwo\\\nthree\n: 2:0;ls\n"
	res := mustParse(t, input, Options{})
	if len(res.Entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(res.Entries), res.Entries)
	}
	if got, want := res.Entries[0].Command, "echo one\ntwo\nthree"; got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
	if res.Entries[0].Line != 1 {
		t.Errorf("multi-line entry Line = %d, want 1", res.Entries[0].Line)
	}
	if res.Entries[1].Command != "ls" || res.Entries[1].Line != 4 {
		t.Errorf("entry after multi-line = %+v, want ls at line 4", res.Entries[1])
	}
}

func TestParseZshStrictMalformedLine(t *testing.T) {
	err := mustFail(t, ": 1:0;ls\ngarbage\n: 2:0;pwd\n", Options{})
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %q, want it to name line 2", err)
	}
}

func TestParseZshLenientMalformedLine(t *testing.T) {
	res := mustParse(t, ": 1:0;ls\ngarbage\n: 2:0;pwd\n", Options{Lenient: true})
	if len(res.Entries) != 2 {
		t.Errorf("got %d entries, want 2", len(res.Entries))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Line != 2 {
		t.Errorf("Skipped = %+v, want one skip at line 2", res.Skipped)
	}
}

func TestParseZshTimestampOverflow(t *testing.T) {
	input := ": 99999999999999999999:0;ls\n: 2:0;pwd\n"

	err := mustFail(t, input, Options{})
	if !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), "timestamp") {
		t.Errorf("error = %q, want it to name line 1 and the timestamp", err)
	}

	res := mustParse(t, input, Options{Lenient: true})
	if len(res.Entries) != 1 || res.Entries[0].Command != "pwd" {
		t.Errorf("entries = %+v", res.Entries)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Line != 1 {
		t.Errorf("Skipped = %+v", res.Skipped)
	}
}

func TestParseZshUnterminatedContinuation(t *testing.T) {
	input := ": 1:0;ls\n: 2:0;echo hi\\\n"
	// The trailing backslash is followed by nothing: the file ends before the
	// continuation line, so after the newline there are no more lines.
	err := mustFail(t, input, Options{})
	if !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "continuation") {
		t.Errorf("error = %q, want it to name line 2 and the continuation", err)
	}

	res := mustParse(t, input, Options{Lenient: true})
	if len(res.Entries) != 1 || res.Entries[0].Command != "ls" {
		t.Errorf("entries = %+v", res.Entries)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Line != 2 {
		t.Errorf("Skipped = %+v", res.Skipped)
	}
}

func TestParseZshInvalidUTF8InContinuation(t *testing.T) {
	input := ": 1:0;a\\\n\xff\n: 2:0;b\n"

	err := mustFail(t, input, Options{})
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %q, want it to name line 2", err)
	}

	res := mustParse(t, input, Options{Lenient: true})
	if len(res.Entries) != 1 || res.Entries[0].Command != "b" {
		t.Errorf("entries = %+v", res.Entries)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Line != 1 {
		t.Errorf("Skipped = %+v", res.Skipped)
	}
}

func TestParseZshInvalidUTF8InHeaderLine(t *testing.T) {
	input := ": 1:0;echo \xff\n: 2:0;ok\n"

	err := mustFail(t, input, Options{})
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error = %q, want it to name line 1", err)
	}

	res := mustParse(t, input, Options{Lenient: true})
	if len(res.Entries) != 1 || res.Entries[0].Command != "ok" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestParseLineTooLong(t *testing.T) {
	// Longer than the scanner's 1 MiB cap; should surface as an error rather
	// than a silently truncated command, even in lenient mode.
	input := "echo " + strings.Repeat("a", 2<<20) + "\n"
	for _, lenient := range []bool{false, true} {
		err := mustFail(t, input, Options{Lenient: lenient})
		if !strings.Contains(err.Error(), "reading input") {
			t.Errorf("lenient=%v: error = %q, want a reading error", lenient, err)
		}
	}
}
