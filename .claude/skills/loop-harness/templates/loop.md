<!--
Default prompt for a bare `/loop` in this project.
Copy to `.claude/loop.md` (project) or `~/.claude/loop.md` (personal).
When you run `/loop` with no prompt, Claude Code uses this text on a dynamic interval.
Edit it to match what "keep an eye on things" means for THIS project.
-->

Do one focused maintenance pass, then wait for the next iteration:

1. Check the open pull request for this branch. If CI is failing, diagnose the
   failure and push a fix; if it's a flaky/infra failure, re-run it.
2. Address any new review comments that are unambiguous and low-risk. For
   anything ambiguous or architecturally significant, stop and ask instead.
3. If nothing is actionable, do nothing and end the pass quietly — do not invent
   work or comment on the PR just to have acted.

Stop the loop once the PR is merged or closed.
