package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

// reportError prints a non-2xx / transport error to stderr and maps it to
// an exit code. A 401 additionally prints the token-creation hint. Returns
// the exit code so callers can `return reportError(...)`.
func reportError(stderr io.Writer, err error) int {
	if errors.Is(err, errUnauthorized) {
		fmt.Fprintln(stderr, "error:", err)
		fmt.Fprintln(stderr, "hint: this server rejected the API token. Create one in the UI under Settings → API tokens,")
		fmt.Fprintln(stderr, "      then export it as COOKER_TOKEN (e.g. export COOKER_TOKEN=ck_…).")
		return 1
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// requireToken guards commands that hit authenticated endpoints, failing
// fast with the same hint rather than making a doomed request.
func requireToken(cfg globalConfig, stderr io.Writer) bool {
	if cfg.token == "" {
		fmt.Fprintln(stderr, "error: no API token provided")
		fmt.Fprintln(stderr, "hint: set COOKER_TOKEN (preferred) or pass --token. Create a token in the UI under Settings → API tokens.")
		return false
	}
	return true
}

// --------------------------------------------------------------- helpers

// printJSON marshals v as indented JSON to stdout, returning an exit code.
func printJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "error: encode json:", err)
		return 1
	}
	return 0
}

// flushErr flushes a tabwriter and maps a flush failure to an exit code.
func flushErr(tw *tabwriter.Writer, stderr io.Writer) int {
	if err := tw.Flush(); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// parseInterspersed parses fs allowing flags to appear after positional
// arguments (e.g. `run p1 --follow`), which the stdlib flag package does
// not support — it stops at the first non-flag. We collect leading
// positionals, re-parse the remainder, and repeat. The collected
// positionals are returned in order; on a flag error the underlying
// ContinueOnError surfaces it. This lets the CLI accept the natural
// kubectl-style ordering instead of forcing flags before the id.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		// First remaining arg is a positional (Parse stopped on it).
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}
