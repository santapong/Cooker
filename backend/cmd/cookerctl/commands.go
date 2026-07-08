package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

// clientVersion is the cookerctl build version. Overridable at build time
// via -ldflags "-X main.clientVersion=v1.2.3" to mirror the server binary.
var clientVersion = "v0.1.0-dev"

// usage is the top-level help text. Kept terse; each subcommand prints its
// own flag help via -h.
const usage = `cookerctl — command-line client for a Cooker server

Usage:
  cookerctl [global flags] <command> [args]

Commands:
  version                                 Print client version and the server's
  pipelines list                          List pipelines
  pipelines export <id> [-o file]         Export a pipeline as YAML (stdout or file)
  pipelines import -f <file.yaml>          Import a pipeline from YAML; prints the new id
  pipelines run <id> [--follow]           Trigger a run; --follow polls status to completion
  runs list <pipelineId>                  List runs for a pipeline (newest first)
  runs logs <runId> --pipeline <id> [--stage <stageId>]
                                          Fetch final stage logs for a run

Global flags:
  --server URL    Cooker base URL (default $COOKER_URL or http://localhost:8080)
  --token TOKEN   API token (prefer the COOKER_TOKEN env var; never logged)
  --json          Machine-readable JSON output (list/get commands)

Environment:
  COOKER_URL      Default server base URL
  COOKER_TOKEN    API token (ck_…). Preferred over --token so it never lands in shell history.

Create a token in the UI under Settings → API tokens.
`

// globalConfig is resolved once from flags + env before dispatch.
type globalConfig struct {
	server string
	token  string
	json   bool
}

// run is the testable entry point: it parses argv, dispatches, and
// returns a process exit code. main is a thin wrapper around it. stdout
// and stderr are injected so tests can capture output.
func run(argv []string, stdout, stderr io.Writer) int {
	cfg, rest, err := parseGlobal(argv)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		fmt.Fprint(stderr, usage)
		return 2
	}
	if len(rest) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	ctx := context.Background()
	cmd, args := rest[0], rest[1:]

	switch cmd {
	case "version":
		return cmdVersion(ctx, cfg, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "pipelines":
		return dispatchPipelines(ctx, cfg, args, stdout, stderr)
	case "runs":
		return dispatchRuns(ctx, cfg, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// parseGlobal pulls the global flags off the front of argv and returns the
// resolved config plus the remaining (sub)command arguments. Flags may be
// interspersed before the subcommand. The token falls back to COOKER_TOKEN
// and the server to COOKER_URL.
func parseGlobal(argv []string) (globalConfig, []string, error) {
	fs := flag.NewFlagSet("cookerctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we render usage ourselves
	server := fs.String("server", "", "Cooker base URL")
	token := fs.String("token", "", "API token (prefer COOKER_TOKEN)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(argv); err != nil {
		return globalConfig{}, nil, err
	}

	cfg := globalConfig{server: *server, token: *token, json: *asJSON}
	if cfg.server == "" {
		cfg.server = os.Getenv("COOKER_URL")
	}
	if cfg.server == "" {
		cfg.server = "http://localhost:8080"
	}
	if cfg.token == "" {
		cfg.token = os.Getenv("COOKER_TOKEN")
	}
	return cfg, fs.Args(), nil
}

// --------------------------------------------------------------- version

func cmdVersion(ctx context.Context, cfg globalConfig, stdout, stderr io.Writer) int {
	c := newClient(cfg.server, cfg.token)
	v, err := c.Version(ctx)
	if err != nil {
		// The client version is still useful even if the server is
		// unreachable; print it, then surface the server error.
		fmt.Fprintf(stdout, "Client: %s\n", clientVersion)
		fmt.Fprintf(stderr, "error: could not reach server at %s: %s\n", cfg.server, err)
		return 1
	}
	if cfg.json {
		return printJSON(stdout, stderr, map[string]any{
			"client": clientVersion,
			"server": v,
		})
	}
	fmt.Fprintf(stdout, "Client: %s\n", clientVersion)
	fmt.Fprintf(stdout, "Server: %s (%s, built %s, %s)\n", v.BuildVersion, v.BuildSHA, v.BuildTime, v.GoVersion)
	return 0
}
