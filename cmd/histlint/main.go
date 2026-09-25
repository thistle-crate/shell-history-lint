// Command histlint reads a shell history file and prints its entries.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/thistle-crate/shell-history-lint/histfile"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("histlint", flag.ContinueOnError)
	lenient := fs.Bool("lenient", false, "skip malformed entries instead of failing")
	asJSON := fs.Bool("json", false, "print entries as JSON, one per line")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: histlint [--lenient] [--json] <history-file>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		return 1
	}
	defer f.Close()

	result, err := histfile.Parse(f, histfile.Options{Lenient: *lenient})
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		return 1
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	enc := json.NewEncoder(w)
	for _, e := range result.Entries {
		if *asJSON {
			if err := enc.Encode(e); err != nil {
				fmt.Fprintln(os.Stderr, "histlint:", err)
				return 1
			}
			continue
		}
		if e.Timestamp.IsZero() {
			fmt.Fprintln(w, e.Command)
		} else {
			fmt.Fprintf(w, "%s  %s\n", e.Timestamp.Format("2006-01-02 15:04:05"), e.Command)
		}
	}

	if len(result.Skipped) > 0 {
		w.Flush()
		fmt.Fprintf(os.Stderr, "histlint: skipped %d malformed line(s) (rerun without --lenient to see them)\n", len(result.Skipped))
	}

	return 0
}
