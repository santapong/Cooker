package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
)

// ------------------------------------------------------------- pipelines

func dispatchPipelines(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "error: pipelines requires a subcommand: list | export | import | run")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdPipelinesList(ctx, cfg, stdout, stderr)
	case "export":
		return cmdPipelinesExport(ctx, cfg, rest, stdout, stderr)
	case "import":
		return cmdPipelinesImport(ctx, cfg, rest, stdout, stderr)
	case "run":
		return cmdPipelinesRun(ctx, cfg, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown pipelines subcommand %q\n", sub)
		return 2
	}
}

func cmdPipelinesList(ctx context.Context, cfg globalConfig, stdout, stderr io.Writer) int {
	if !requireToken(cfg, stderr) {
		return 1
	}
	c := newClient(cfg.server, cfg.token)
	ps, err := c.ListPipelines(ctx)
	if err != nil {
		return reportError(stderr, err)
	}
	if cfg.json {
		return printJSON(stdout, stderr, ps)
	}
	if len(ps) == 0 {
		fmt.Fprintln(stdout, "No pipelines.")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTAGES\tVERSION\tUPDATED")
	for _, p := range ps {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n",
			p.ID, p.Name, len(p.Stages), p.Version, p.UpdatedAt.Format(time.RFC3339))
	}
	return flushErr(tw, stderr)
}

func cmdPipelinesExport(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pipelines export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write YAML to this file instead of stdout")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: cookerctl pipelines export <id> [-o file]")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	id := pos[0]
	c := newClient(cfg.server, cfg.token)
	doc, err := c.ExportPipeline(ctx, id)
	if err != nil {
		return reportError(stderr, err)
	}
	if *out == "" {
		_, _ = stdout.Write(doc)
		return 0
	}
	// 0644: a pipeline document carries no secrets (only secretRefs), so
	// it is safe to write with ordinary file permissions.
	if err := os.WriteFile(*out, doc, 0o644); err != nil {
		fmt.Fprintln(stderr, "error: write file:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Wrote %s (%d bytes)\n", *out, len(doc))
	return 0
}

func cmdPipelinesImport(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pipelines import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "", "path to a pipeline YAML document (- for stdin)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "usage: cookerctl pipelines import -f <file.yaml>")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	var (
		doc []byte
		err error
	)
	if *file == "-" {
		doc, err = io.ReadAll(os.Stdin)
	} else {
		doc, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error: read file:", err)
		return 1
	}
	c := newClient(cfg.server, cfg.token)
	p, err := c.ImportPipeline(ctx, doc)
	if err != nil {
		return reportError(stderr, err)
	}
	if cfg.json {
		return printJSON(stdout, stderr, p)
	}
	fmt.Fprintf(stdout, "Imported pipeline %q\nID: %s\n", p.Name, p.ID)
	return 0
}

func cmdPipelinesRun(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pipelines run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	follow := fs.Bool("follow", false, "poll run status and print stage transitions until the run finishes")
	idemKey := fs.String("idempotency-key", "", "override the generated Idempotency-Key sent with the run trigger")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: cookerctl pipelines run <id> [--follow] [--idempotency-key KEY]")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	pipelineID := pos[0]
	key := *idemKey
	if key == "" {
		// A fresh key per invocation makes a retried `run` (e.g. after a
		// transient network error) safe: the server replays the original
		// 202 instead of spawning a duplicate run.
		key = uuid.NewString()
	}

	c := newClient(cfg.server, cfg.token)
	runRec, err := c.RunPipeline(ctx, pipelineID, runOptions{idempotencyKey: key})
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Run %s started (status: %s)\n", runRec.ID, runRec.Status)

	if !*follow {
		if cfg.json {
			return printJSON(stdout, stderr, runRec)
		}
		fmt.Fprintf(stdout, "Follow with: cookerctl runs logs %s --pipeline %s\n", runRec.ID, pipelineID)
		return 0
	}
	return followRun(ctx, c, cfg, pipelineID, runRec.ID, stdout, stderr)
}
