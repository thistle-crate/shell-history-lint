# shell-history-lint

A small Go library and CLI for parsing shell history files.

## The problem

`~/.bash_history` and `~/.zsh_history` are not one format. Bash writes plain
commands, one per line, with no timestamp, and if you HISTCONTROL a
multi-line command it just writes the literal newlines into the file - there
is no reliable way to tell where one command ends and the next begins short
of re-parsing shell syntax. Zsh's extended history format is better: each
entry looks like

```
: 1690000000:0;git commit -m "fix thing"
```

(start time, elapsed seconds, command), and multi-line commands escape their
internal newlines with a trailing backslash so they can be put back together
exactly. Tools that read these files casually - dedupers, search indexes,
"what did I run last Tuesday" scripts - tend to either crash on the first odd
line or silently mangle it, which is worse, because you don't find out until
the command you "found" in your history isn't the one you actually ran.

`histfile` parses both formats into a common `Entry` type. By default it is
strict: a line that doesn't match the format it detected, isn't valid UTF-8,
or belongs to a multi-line command that never gets a closing line stops the
parse with an error naming the exact line. `--lenient` (or `Options.Lenient`
in the library) turns those into skips instead, so you can process a history
file you know is a little battered without lying to yourself about the
count.

## CLI usage

```
$ histlint ~/.zsh_history
2024-07-22 09:14:03  git status
2024-07-22 09:14:19  git commit -m "fix thing"

$ histlint --json ~/.zsh_history
{"Command":"git status","Timestamp":"2024-07-22T09:14:03Z","Duration":0,"Line":1}
{"Command":"git commit -m \"fix thing\"","Timestamp":"2024-07-22T09:14:19Z","Duration":0,"Line":2}

$ histlint ~/.zsh_history
histlint: line 47: does not match zsh extended history format (use --lenient to skip malformed lines)

$ histlint --lenient ~/.zsh_history
histlint: skipped 1 malformed line(s) (rerun without --lenient to see them)
```

## Library usage

```go
f, err := os.Open(path)
if err != nil {
	log.Fatal(err)
}
defer f.Close()

result, err := histfile.Parse(f, histfile.Options{Lenient: false})
if err != nil {
	log.Fatal(err)
}

for _, e := range result.Entries {
	fmt.Println(e.Command)
}
```

`result.Format` tells you which format was detected (`histfile.FormatPlain`
or `histfile.FormatZshExtended`). In lenient mode, `result.Skipped` lists the
line number and reason for every entry that was dropped.

## Limitations

Plain bash history has no way to distinguish a command's embedded newline
from the boundary between two commands, so `histfile` treats every physical
line as one entry in that format. If you want faithful multi-line recovery,
turn on zsh's extended history (`setopt EXTENDED_HISTORY`) going forward -
there is no way to reconstruct it retroactively from a plain history file.

## Install

```
go install github.com/thistle-crate/shell-history-lint/cmd/histlint@latest
```

## License

MIT, see [LICENSE](LICENSE).
