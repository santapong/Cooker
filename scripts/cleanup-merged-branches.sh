#!/usr/bin/env bash
# Delete remote branches whose work is already on the default branch.
#
# Usage:
#   scripts/cleanup-merged-branches.sh [--delete] [--keep <glob>]... [--remote <name>]
#
# Run this LOCALLY with git + an authenticated gh CLI — the half of
# backlog item W6.4 that a sandboxed agent cannot do (the sandbox proxy
# refuses branch-deleting pushes).
#
# This repo squash-merges, so a merged branch's head is NOT an ancestor
# of main and `git branch --merged` misses nearly everything. A branch
# is therefore safe to delete when either:
#   A. it was the head of a MERGED pull request (per `gh pr list`), or
#   B. its head is an ancestor of the default branch (true merges, or
#      branches that never diverged).
# Everything else — no PR, or a closed-but-unmerged PR — is printed as
# "needs manual review" and never deleted automatically.
#
# Default is a dry run that prints both lists. --delete asks once, then
# deletes the safe list in batches. --keep excludes branches matching a
# shell glob (repeatable). Exits 0 on success, 1 on abort, 2 on misuse.

set -euo pipefail

usage() { sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; }

remote="origin"
do_delete=0
keep=()

while [ $# -gt 0 ]; do
  case "$1" in
    --delete) do_delete=1 ;;
    --keep)   shift; [ $# -gt 0 ] || { echo "--keep needs a glob" >&2; exit 2; };  keep+=("$1") ;;
    --remote) shift; [ $# -gt 0 ] || { echo "--remote needs a name" >&2; exit 2; }; remote="$1" ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

command -v gh >/dev/null 2>&1 || { echo "gh CLI is required — https://cli.github.com" >&2; exit 2; }

root="$(git rev-parse --show-toplevel)"
cd "$root"

git fetch "$remote" --prune --quiet

default_branch="$(git symbolic-ref --quiet --short "refs/remotes/$remote/HEAD" 2>/dev/null | sed "s|^$remote/||" || true)"
[ -n "$default_branch" ] || default_branch="main"

mapfile -t branches < <(git for-each-ref "refs/remotes/$remote" --format='%(refname:lstrip=3)' \
  | grep -vx -e 'HEAD' -e "$default_branch" | sort)

# Signal A: branches that were the head of a merged PR.
mapfile -t merged_pr_heads < <(gh pr list --state merged --limit 1000 --json headRefName --jq '.[].headRefName' | sort -u)

is_merged_pr_head() {
  local b
  for b in ${merged_pr_heads[@]+"${merged_pr_heads[@]}"}; do
    [ "$b" = "$1" ] && return 0
  done
  return 1
}

matches_keep() {
  local p
  for p in ${keep[@]+"${keep[@]}"}; do
    # shellcheck disable=SC2254  # unquoted on purpose: $p is a glob pattern
    case "$1" in $p) return 0 ;; esac
  done
  return 1
}

deletable=()
review=()
for b in ${branches[@]+"${branches[@]}"}; do
  if matches_keep "$b"; then
    continue
  fi
  if is_merged_pr_head "$b" \
     || git merge-base --is-ancestor "refs/remotes/$remote/$b" "refs/remotes/$remote/$default_branch"; then
    deletable+=("$b")
  else
    review+=("$b")
  fi
done

echo "== safe to delete (${#deletable[@]}) — merged-PR head, or ancestor of $default_branch =="
printf '  %s\n' ${deletable[@]+"${deletable[@]}"}
echo
echo "== needs manual review (${#review[@]}) — no merged PR; may carry unmerged work =="
for b in ${review[@]+"${review[@]}"}; do
  printf '  %s  (last commit %s)\n' "$b" "$(git log -1 --format=%cs "refs/remotes/$remote/$b")"
done

if [ "$do_delete" -eq 0 ]; then
  echo
  echo "Dry run — re-run with --delete to delete the safe list."
  exit 0
fi

if [ "${#deletable[@]}" -eq 0 ]; then
  echo
  echo "Nothing to delete."
  exit 0
fi

echo
printf 'Delete %d branch(es) from %s? [y/N] ' "${#deletable[@]}" "$remote"
read -r answer
case "$answer" in
  y|Y|yes|YES) ;;
  *) echo "Aborted."; exit 1 ;;
esac

i=0
while [ "$i" -lt "${#deletable[@]}" ]; do
  batch=("${deletable[@]:i:20}")
  git push "$remote" --delete "${batch[@]}"
  i=$((i + 20))
done
echo "Deleted ${#deletable[@]} branch(es) from $remote."
