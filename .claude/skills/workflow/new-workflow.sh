#!/usr/bin/env bash
# Scaffold a new Cooker workflow script.
#
# Usage:
#   .claude/skills/workflow/new-workflow.sh <slug>
#
# Writes .claude/workflows/<slug>.js with a pure-literal meta block and
# a pipeline-by-default skeleton (review -> adversarial verify ->
# synthesize), then prints the path for the caller to edit.
#
# The skeleton encodes the house rules so the first draft is already
# correct: pipeline over parallel, schema-validated agent output, and
# no Date.now()/Math.random() (both throw in workflow scripts).

set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: new-workflow.sh <slug>" >&2
  echo "       e.g.  new-workflow.sh cooker-perf-sweep" >&2
  exit 2
fi

# Sanitise to a filename-safe, hook-name-safe slug.
slug="$(echo "$1" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' \
        | sed 's/--*/-/g; s/^-//; s/-$//')"
if [ -z "$slug" ]; then
  echo "error: empty slug after sanitisation" >&2
  exit 2
fi

root="$(git rev-parse --show-toplevel)"
dir="$root/.claude/workflows"
out="$dir/${slug}.js"

mkdir -p "$dir"
if [ -e "$out" ]; then
  echo "error: $out already exists" >&2
  exit 1
fi

cat >"$out" <<EOF
// ${slug} — Cooker workflow.
//
// Run:  Workflow({ name: "${slug}" })
// Docs: .claude/skills/workflow/SKILL.md
//
// House rules baked in below: pipeline (not a parallel barrier),
// schema-validated agent output, adversarial verify, and NO
// Date.now()/Math.random() (they throw in workflow scripts).

export const meta = {
  name: '${slug}',
  description: 'TODO: one line shown in the permission dialog.',
  whenToUse: 'TODO: when a maintainer should reach for this workflow.',
  phases: [
    { title: 'Review' },
    { title: 'Verify' },
    { title: 'Synthesize' },
  ],
}

// TODO: define the dimensions/finders this workflow fans out over.
const DIMENSIONS = [
  { key: 'example', prompt: 'TODO: a Cooker-specific finder prompt. Demand file:line citations.' },
]

const FINDINGS_SCHEMA = {
  type: 'object',
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          title:    { type: 'string' },
          file:     { type: 'string' },
          severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
          detail:   { type: 'string' },
        },
        required: ['title', 'file', 'severity', 'detail'],
      },
    },
  },
  required: ['findings'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    isReal:    { type: 'boolean' },
    reasoning: { type: 'string' },
  },
  required: ['isReal', 'reasoning'],
}

phase('Review')

// pipeline: each dimension's findings verify the moment that review
// completes — no waiting on the slowest reviewer.
const reviewed = await pipeline(
  DIMENSIONS,
  d => agent(d.prompt, { label: \`review:\${d.key}\`, phase: 'Review', schema: FINDINGS_SCHEMA }),
  (review, d) => parallel((review?.findings ?? []).map(f => () =>
    agent(
      \`Adversarially verify this \${d.key} finding against the actual Cooker code. \` +
      \`Default isReal=false if you cannot confirm it from the cited file.\\n\\n\` +
      \`Title: \${f.title}\\nFile: \${f.file}\\nDetail: \${f.detail}\`,
      { label: \`verify:\${d.key}:\${f.file}\`, phase: 'Verify', schema: VERDICT_SCHEMA },
    ).then(v => ({ ...f, dimension: d.key, verdict: v }))
  )),
)

const confirmed = reviewed.flat().filter(Boolean).filter(f => f.verdict?.isReal)

phase('Synthesize')

const summary = await agent(
  \`Synthesize a report from these confirmed Cooker findings. Group by severity, \` +
  \`then dimension; give file:line and a one-line fix sketch for each.\\n\\n\` +
  JSON.stringify(confirmed, null, 2),
  { label: 'synthesize', phase: 'Synthesize' },
)

log(\`${slug}: \${confirmed.length} confirmed finding(s)\`)
return { confirmed, summary }
EOF

echo "$out"
