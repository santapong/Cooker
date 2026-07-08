package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// ------------------------------------------------------------------ runs

func dispatchRuns(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "error: runs requires a subcommand: list | logs")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdRunsList(ctx, cfg, rest, stdout, stderr)
	case "logs":
		return cmdRunsLogs(ctx, cfg, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown runs subcommand %q\n", sub)
		return 2
	}
}

func cmdRunsList(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: cookerctl runs list <pipelineId>")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	c := newClient(cfg.server, cfg.token)
	runs, err := c.ListRuns(ctx, args[0])
	if err != nil {
		return reportError(stderr, err)
	}
	if cfg.json {
		return printJSON(stdout, stderr, runs)
	}
	if len(runs) == 0 {
		fmt.Fprintln(stdout, "No runs.")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN ID\tSTATUS\tSTAGES\tCREATED")
	for _, r := range runs {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
			r.ID, r.Status, len(r.StageRuns), r.CreatedAt.Format(time.RFC3339))
	}
	return flushErr(tw, stderr)
}

func cmdRunsLogs(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runs logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pipelineID := fs.String("pipeline", "", "pipeline id the run belongs to (required)")
	stage := fs.String("stage", "", "limit to a single stage id (default: all stages)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || *pipelineID == "" {
		fmt.Fprintln(stderr, "usage: cookerctl runs logs <runId> --pipeline <id> [--stage <stageId>]")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	runID := pos[0]
	c := newClient(cfg.server, cfg.token)

	// Resolve the run to know its stages and to build readable names.
	run, err := c.GetRun(ctx, *pipelineID, runID)
	if err != nil {
		return reportError(stderr, err)
	}
	names := map[string]string{}
	if p, perr := c.GetPipeline(ctx, *pipelineID); perr == nil {
		for _, s := range p.Stages {
			names[s.ID] = s.Name
		}
	}

	stageIDs := make([]string, 0, len(run.StageRuns))
	if *stage != "" {
		stageIDs = append(stageIDs, *stage)
	} else {
		for _, sr := range run.StageRuns {
			stageIDs = append(stageIDs, sr.StageID)
		}
	}

	if cfg.json {
		out := make(map[string]string, len(stageIDs))
		for _, sid := range stageIDs {
			logs, lerr := c.StageLogs(ctx, *pipelineID, runID, sid)
			if lerr != nil {
				return reportError(stderr, lerr)
			}
			out[sid] = logs
		}
		return printJSON(stdout, stderr, out)
	}

	for _, sid := range stageIDs {
		logs, lerr := c.StageLogs(ctx, *pipelineID, runID, sid)
		if lerr != nil {
			return reportError(stderr, lerr)
		}
		display := names[sid]
		if display == "" {
			display = sid
		}
		fmt.Fprintf(stdout, "=== %s (%s) ===\n", display, sid)
		if strings.TrimSpace(logs) != "" {
			fmt.Fprintln(stdout, strings.TrimRight(logs, "\n"))
		}
	}
	return 0
}
