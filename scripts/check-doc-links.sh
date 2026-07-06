#!/usr/bin/env bash
# Check that relative markdown links resolve to existing files.
#
# Usage:
#   scripts/check-doc-links.sh [pathspec ...]
#
# Scans git-tracked *.md files (all of them, or only those matching the
# given git pathspecs) and verifies that every relative [text](target)
# link points at a file or directory that exists. Skipped on purpose:
# external links (http/https/mailto/data), pure #anchor links, anything
# inside fenced code blocks or inline code spans, targets containing
# {{template}} variables, *.draft.md files (unpublished drafts), and
# skill templates/ dirs (scaffolding placeholders by design). No network
# access — this catches the "file moved/deleted but a doc still points
# at it" class of rot, not dead external URLs.
#
# Output is parseable: one "<file>: <target>" per line for each broken
# link. Exits 1 if any link is broken, 0 otherwise.

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

# Untracked-but-not-ignored files are included so a pre-`git add` run
# can't silently skip a brand-new doc.
if [ $# -gt 0 ]; then
  mapfile -t files < <({ git ls-files -- "$@"; git ls-files --others --exclude-standard -- "$@"; } | grep -E '\.md$' | sort -u || true)
else
  mapfile -t files < <({ git ls-files -- '*.md'; git ls-files --others --exclude-standard -- '*.md'; } | sort -u)
fi

broken=0
for f in "${files[@]}"; do
  case "$f" in
    *.draft.md) continue ;;                  # unpublished drafts carry placeholder links
    .claude/skills/*/templates/*) continue ;; # scaffolds contain placeholders by design
  esac
  dir=$(dirname "$f")
  while IFS= read -r target; do
    t="$target"
    t="${t#<}"; t="${t%>}"          # <target>
    t="${t%% \"*}"                   # trailing "title"
    t="${t%%#*}"                     # #fragment
    case "$t" in
      ''|http://*|https://*|mailto:*|data:*|ftp://*) continue ;;
      *'{{'*) continue ;;            # template variable, not a real path
    esac
    if [ "${t#/}" != "$t" ]; then
      resolved="$root$t"             # /path = repo-root relative (GitHub semantics)
    else
      resolved="$dir/$t"
    fi
    if [ ! -e "$resolved" ]; then
      echo "$f: $target"
      broken=$((broken + 1))
    fi
  done < <(awk '
    /^ *(```|~~~)/ { fence = !fence; next }
    fence { next }
    {
      line = $0
      gsub(/`[^`]*`/, "", line)       # drop inline code spans
      while (match(line, /\]\([^()]*\)/)) {
        print substr(line, RSTART + 2, RLENGTH - 3)
        line = substr(line, RSTART + RLENGTH)
      }
    }
  ' "$f")
done

if [ "$broken" -gt 0 ]; then
  echo "FAIL: $broken broken relative link(s)" >&2
  exit 1
fi
echo "OK: all relative markdown links resolve"
