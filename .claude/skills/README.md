# Cooker — skill roster

Three groups live in this directory. The full division of labour is in
[`docs/engineering/harness-engineering.md`](../../docs/engineering/harness-engineering.md) § "The skill library".

| Group | Skills | Origin |
|---|---|---|
| `cooker-*` (7) | find, audit, fix-bug, improve, new-feature, ci-debug, weekly | Repo-authored — Cooker's project protocol |
| `loop-*` (12) | engine, review, design, orchestrate, research, audit, test, debug, docs, scout, harness, autopilot | Vendored from TheLoopSkill (below) |
| `ponytail-*` (6) | ponytail, review, audit, debt, gain, help | Vendored (see `ponytail/VENDORED_FROM.md`) |

## Vendored: TheLoopSkill

- Source: https://github.com/santapong/TheLoopSkill
- Version: 0.4.0
- Commit: 9f03ad109160c85bb645ca7efcbc410bc3414bb8
- Method: vendored per its INSTALL.md Option B (copied, not a submodule) — the
  path that works in Claude Code web sessions, which only see committed files.
- License: MIT

**Keep the copies verbatim.** Do not edit files under `loop-*/` — project-specific
additions go in a `cooker-*` skill, a framework file (see below), or the docs.
The one intentional local addition rides *alongside* the vendored files, not inside them:
`loop-engine/frameworks/Cooker-AIDLC.md` + its row in `loop-engine/frameworks/README.md`
(frameworks are loop-engine's designed extension point, discovered from that directory).

**Re-sync procedure** (bump the pin in the same PR):

```bash
git clone --depth 1 https://github.com/santapong/TheLoopSkill /tmp/tls
for s in /tmp/tls/.claude/skills/loop-*; do
  n=$(basename "$s")
  rm -rf ".claude/skills/$n" && cp -r "$s" ".claude/skills/$n"
done
# re-apply the local addition checks:
ls .claude/skills/loop-engine/frameworks/Cooker-AIDLC.md          # must still exist
grep -q Cooker-AIDLC .claude/skills/loop-engine/frameworks/README.md || echo "re-add the Cooker-AIDLC table row"
# then update Version/Commit above and note the bump in CHANGELOG.md
```

After a re-sync, re-run `scripts/check-doc-links.sh` and skim the upstream
CHANGELOG for renamed/removed skills — the `cooker-*` routing notes and
`docs/engineering/harness-engineering.md` reference `loop-*` names by string.

**Plugin overlap:** if you also have the TheLoopSkill *plugin* installed in your
Claude Code (via `/plugin install theloopskill@theloopskill`), disable it for
this repo — the vendored copy here is canonical, and two sources exposing the
same skill names is avoidable ambiguity.
