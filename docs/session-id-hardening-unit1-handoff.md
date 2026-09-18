# Session-ID hardening Unit 1 — session handoff

**Written:** 2026-09-18, at the end of a working session, so the work can be resumed on another machine from Git state alone.

**Branch:** `fix/session-id-hardening-followup`, based on `8a70a5ef6` (the tip of PR #2405, `codex/session-id-hardening-20260912`).

**Why this file exists.** The execution ran under a workspace at
`.superpowers/sdd/session-id-hardening-unit1-implementation-plan/`, which is **gitignored**.
That directory holds the ledger, per-task briefs, per-task implementer reports and every
review diff — none of which survive a clone. This document carries forward everything from
it that a reader needs. The per-task reports are gone; their conclusions are here.

## State

| | |
|---|---|
| Plan | `docs/session-id-hardening-unit1-implementation-plan.md` (10 tasks) |
| Spec (binding authority) | `docs/session-id-hardening-followup-plan.md` |
| Tasks complete | **10 of 10** |
| Code commits | 11 (`b151c9003..1c010d698`) |
| Doc commits | 3 (`e731852f7`, `e21a1aff0`, `a33fb477f`) |
| Fix rounds | 1 (Task 4, round 1 of 5) |
| `mise run fmt && mise run lint` | clean |
| `GOOS=windows go build ./...` | clean |
| Test baseline | 15 `--- FAIL` lines / 14 top-level tests, all fetch/remote or checkpoint-remote, **identical roster to the base commit**; environment-sensitive |

The acceptance bar throughout was *no new failure relative to the base commit*, never an
absolute count. Re-measure on your own base before comparing.

## The eleven code commits

```
1c010d698  fix(agent): the ResolveSessionFile guard covers the whole repo
5d831e791  fix(resume): scan stored session IDs whenever the fallback runs
0d7122cb0  fix(strategy): say when the checkpoint's agent is assumed
0a7ffc121  fix(resume): pass logCtx to the fallback's restoreSingleSession call
1b4a3d634  fix(resume): skip agent resolution too when the top-level session ID is unsafe
d1e6a62ed  fix(resume): log the agent name, not the agent type
fe9210c6a  fix(agent): Name refuses a rooted relative name
1e9b39463  refactor(agent): unexport sessionRefIsFilesystemPath
41aa15cd4  fix(agent): create nested store directories 0700
00b509721  fix(external): containment no longer disappears without a repo path
b151c9003  fix(agent): name the actual problem on the lexical ref arms
```

PR text for this branch: `docs/session-id-hardening-unit1-pr-description.md`.

## UNFINISHED — read this first

1. **The final whole-branch review was dispatched and its result was never captured.** It was
   running over `8a70a5ef6..1c010d698` when the session ended. Nothing in this branch reflects
   its findings. **Re-run it before merging.** It was asked to do four things no task-scoped
   review could: triage the eight deferred minors below, judge the four consequential rulings,
   verify `afe62c4c5`'s no-instantiation property survived, and look for defects visible only
   from the whole diff.
2. **The three doc commits are unsigned.** `e731852f7`, `e21a1aff0` and `a33fb477f` were created
   with `-c commit.gpgsign=false`; all eleven code commits are signed. Re-signing them is a local
   rewrite and was offered but never decided.
3. **Units 2–6 of the spec are not started.** Unit 1 is the only one gating PR #2405. Of the rest,
   Unit 4.1 (`CLAUDE_CONFIG_DIR` unhonoured in `claudecode.GetSessionDir`/`GetSessionBaseDir`)
   is the one to do next: it silently restores transcripts where `claude --resume` will never
   find them, and it is a precondition for Unit 5's containment gate, which would otherwise
   refuse the repo's own e2e suite.

## Decisions taken without the author present

Each was recorded as it was made, with what it costs if wrong. The first four are the
consequential ones.

### Consequential

- **Refuse, don't forward, a filesystem-shaped `session_ref` when `RepoPath` is empty.**
  A behaviour change for third-party plugins — the containment check previously vanished when
  `RepoPath` was unset. Both in-tree callers already set it. *Cost if wrong:* a plugin that
  legitimately has no repo context can no longer write an absolute ref.
- **Keep the dot-component refusal** in `ValidateExternalSessionRef` rather than relaxing it;
  both lexical arms re-sentinelled to `ErrUnsafeSessionName`. The code documents the refusal as
  deliberate ("a dot component survives no round trip through a plugin that joins or normalizes
  it"). *Cost if wrong:* a plugin doing naive `dir + "/./" + id` concatenation stays refused.
- **Do not chmod already-created `0750` session directories.** New nested ones are `0700`;
  existing ones keep the agent's mode, because the agent owns that directory, not Entire.
  Transcripts are `0600` regardless. *Cost if wrong:* directory listing — session IDs, not
  content — stays exposed for existing users.
- **The `ExtractModelFromTranscript` guard's deletion is documented, not reverted.** It keyed on
  the session ID while the read takes a path, so it never guarded what it appeared to. The
  unconfined read belongs to Unit 5. *Cost if wrong:* an unconfined read stays unconfined one
  release longer, with the reason stated rather than silent.

### Process and plan-defect rulings

- **Ruling 1 (Task 4):** the plan's test seam `logging.WithHandlerForTesting` does not exist.
  Implementer chose the mechanism, bounded by: must fail before the fix and pass after, and must
  distinguish agent NAME from agent TYPE.
- **Ruling 2 (Task 5):** plan named fixtures that don't exist (`newRestoreLogsEnv`,
  `withPerSessionAgent`); real ones are `writeCommittedCheckpoint` and `restoreLogsOnlyAgent`.
  Variable is `sessionAgentName`, not `sessionAgentType`.
- **Ruling 3 (Task 7):** test seam goes in a new `cmd/entire/cli/agent/export_test.go`
  (`package agent`), not an exported `ForTesting` function in production code.
- **Ruling 4 (Task 8):** no `breakResumeCheckpointTranscript` helper; build the `CheckpointInfo`
  by hand with an absent `CheckpointID`, a safe `SessionID`, and an unsafe entry in `SessionIDs`.
- **Ruling 5 (Task 4 × 8):** Task 4 rewrote the region Task 8 targets, so Task 8 re-read the
  function rather than applying the plan's quoted context.
- **Ruling 6 (batching):** Tasks 6 and 7 dispatched and reviewed as one unit — same file, same
  area, and a separate cycle for a two-line rename is overhead.
- **Ruling 7 (plan defect #2):** Task 7's brief contradicted itself — its Interfaces line gave
  `cleanRelativeName` a new parameter while its code samples defined a separate
  `rootedRelativeName` and left `cleanRelativeName` alone. Code samples govern.
- **Ruling 8 (plan defect #3, SECURITY-RELEVANT):** Task 4's brief resolved the agent
  unconditionally at function entry, which reintroduces a regression `afe62c4c5` deliberately
  closed — instantiating agents for unsafe/tampered session IDs — and broke
  `TestRestoreResumeSessions_RejectsUnsafeModernSessionsWithoutLegacyFallback`. Resolution was
  moved after the unsafe-ID gate (commit `1b4a3d634`).
- **Ruling 9 (plan defect, test double):** the brief's chosen double `recordingResumeAgent`
  returns the same string from `Name()` and `Type()`, so the brief's own test could not have
  distinguished the bug from the fix. A `distinctAgentNameTypeAgent` double was added.
- **Ruling 10 (plan defect #4):** the brief's Task 5 sample would have printed `"assuming "` with
  an empty agent name in the both-empty case. The implementation gates on a non-empty checkpoint
  agent.
- **Ruling 11 (Task 10 review):** Task 10 produced no commit, so its task review was folded into
  the final whole-branch review — which then never reported. See UNFINISHED #1.
- **Task 1 ⚠️ resolved:** the rooted arm's sentinel had no Unix coverage. Carried into Task 7,
  whose separator-predicate seam now covers both `Name`'s and `ValidateExternalSessionRef`'s
  rooted arms. **Closed.**
- **Task 4 ⚠️ resolved:** `"resume session started"` (`resume.go:906`) fires before the resolution
  block, so spec item 1.5 is only partially delivered. Accepted as the correct trade — the
  placement that would cover it is the one that reverts `afe62c4c5`.

## Eight deferred minors — none triaged

The final review was to decide block/fix/leave for each. It never reported, so **all eight are
still untriaged**.

1. `session_store_test.go` — `wantMsg` unused in the `windowsOnly` early-return branch.
2. `ValidateExternalSessionRef`'s "is rooted" arm is untestable outside Windows (pre-existing).
3. `external.go:220-223` — refusal message restates the sentinel's own text.
4. `external.go:216-219` — comment duplicates the doc comment on `ValidateExternalSessionRef`.
5. `session_store.go:184-193` — `rootedRelativeName`'s doc comment doesn't note that
   `ValidateExternalSessionRef` shares it.
6. `resume.go:981-988` — re-runs the registry scan on a path guaranteed to fail; could capture
   the first attempt's error.
7. Commit `1b4a3d634` — suppresses the agent log attribute in one case where the agent has
   already been instantiated, so nothing is prevented.
8. `manual_commit_pending.go` — `sessionAgentNameFor`'s doc comment repeats the call-site comment.

## Four defects in the plan itself, found during execution

Recorded because they say something about how this plan was built, not just about these tasks.
Every one was caught by an implementer or reviewer reading the code instead of trusting its
brief: the stale `cleanRelativeName` signature (Ruling 7), the agent-resolution placement that
reverts a security commit (Ruling 8), the test double that cannot distinguish bug from fix
(Ruling 9), and the empty-agent-name warning (Ruling 10).

The pre-flight scan that opened execution checked tasks against each other and against the
plan's own text. It never checked them against **the branch's existing commits** — which is
exactly where Ruling 8's defect was hiding. A future plan against an active branch should scan
that axis too.

## Resuming on another machine

```bash
git clone https://github.com/hiddeco/cli.git && cd cli
git checkout fix/session-id-hardening-followup
go build ./... && mise run fmt && mise run lint
go test ./cmd/entire/cli/ ./cmd/entire/cli/strategy/ 2>&1 | grep -cE '^\s*--- FAIL'   # expect 15
```

Then, in order: re-run the final whole-branch review over `8a70a5ef6..HEAD` (UNFINISHED #1),
triage the eight minors, decide on the unsigned doc commits, and only then consider integration.

Read `docs/session-id-hardening-followup-plan.md` before making any judgement call — especially
its **Corrections** table, which records ten claims made during review that turned out to be
false. Two of them were encoded in shipped documentation and have been corrected on this branch;
do not resurrect them.
