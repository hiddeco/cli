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

1. **The final whole-branch review DID report, just after this file was first written and
   pushed. Its verdict: _ready with fixes_.** Its findings are in "Final review" below and are
   **not yet applied** — no fix wave was run, because the session ended. That section is the
   top of the next session's queue.

   What it settled: the `afe62c4c5` no-instantiation property is **intact** (traced through all
   three subtests, including the one that breaks if resolution moves up — it is real, not
   incidental); the eight deferred minors are triaged and **none blocks merge**; and all four
   consequential rulings are **sound**.
2. **The three doc commits are unsigned.** `e731852f7`, `e21a1aff0` and `a33fb477f` were created
   with `-c commit.gpgsign=false`; all eleven code commits are signed. Re-signing them is a local
   rewrite and was offered but never decided.
3. **Units 2–6 of the spec are not started.** Unit 1 is the only one gating PR #2405. Of the rest,
   Unit 4.1 (`CLAUDE_CONFIG_DIR` unhonoured in `claudecode.GetSessionDir`/`GetSessionBaseDir`)
   is the one to do next: it silently restores transcripts where `claude --resume` will never
   find them, and it is a precondition for Unit 5's containment gate, which would otherwise
   refuse the repo's own e2e suite.

   **Unit 2 is partly decided already — do not re-derive it.** Its full design, with a
   fix/document disposition on each of its six rows, is `## Unit 2` in
   `docs/session-id-hardening-followup-plan.md`. But **two of those six rows were settled during
   Unit 1's execution**, and that resolution lives in an unobvious place — line 35 of
   `docs/session-id-hardening-unit1-implementation-plan.md`:

   - the **`/./` row** is now **document**, not fix. Unit 1 kept the dot-component refusal (the
     code documents it as deliberate) and re-sentinelled both arms, so relaxing it here would
     retire an arm Unit 1 just corrected.
   - the **`get-session-dir` errors row** is governed by Unit 1's item 1.3, which chose *refuse*
     over *forward*. "Skip the preflight" is the same choice as "forward it", so this row must
     follow that decision rather than pick independently.

   Unit 2's remaining live rows are therefore MSYS-form normalisation (which needs
   `paths.normalizeMSYSPath` exported or lifted — not a free call), the `d:`-volume ambiguity
   (document), the legacy-ID hard fail (document), and the device-name regex extension (fix, and
   **re-run the exhaustive false-positive sweep afterwards** — the existing zero-false-positive
   result was measured against the *current* regex and does not carry over).

   One more Unit 2 coupling, from the final review's Minor 6: spec item 1.10 tells the
   protocol-doc author not to describe "the ones Unit 2 will later remove". Since two rows are
   now *document* rather than *fix*, that list is shorter than the spec assumed when it was
   written.

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

## Final review — findings, NOT yet applied

Verdict **ready with fixes**. The eleven commits compose cleanly; both new seams
(`rootedRelativeName`, `sessionAgentNameFor`) are consumed by both intended call sites rather
than left single-purpose.

### Important 1 — `CLAUDE.md:1295` states something false, and it propagated to five places

"Copilot, Gemini and Pi all nest" is **wrong for Gemini and Pi**. Both put their per-project
component in `GetSessionDir` (the store *root*), not in `ResolveSessionFile`:
`geminicli/gemini.go:113` → `~/.gemini/tmp/<hash>/chats`; `pi/pi.go:139` →
`<home>/sessions/<encoded-repo>`. For both, `filepath.Dir(name) == "."`, so `WriteFile`'s
`MkdirAllNoSymlink` branch never fires — their directory was already covered by
`openRootForWrite`'s `MkdirAll(s.dir, 0o700)`. The agents that genuinely nest inside the store
are **Copilot** (`<id>/events.jsonl`), **Codex** (`YYYY/MM/DD/rollout-*.jsonl`,
`codex.go:711`) and **Cursor** (`cursor.go:83`) — the last two are missing from the sentence.

The code change is correct; only the justification is wrong. **It appears in five places and
nobody re-derived it:** spec §1.4, plan Task 3, commit `41aa15cd4`'s message, `CLAUDE.md`, and
`docs/session-id-hardening-unit1-pr-description.md`. Fix all five — the spec especially, since
Units 2–6 will be planned from it.

This is the same defect class the branch exists to remove: a plausible claim about the codebase,
written once and copied forward without checking.

### Important 2 — `external/external.go:221-222` uses the sentinel that names the wrong problem

The new refusal wraps `ErrOutsideSessionStore`, rendering as *"path is outside the agent's
session directory: … is filesystem-shaped but no repo path was supplied"* — asserting
containment **failed** when the truth is it could not be **evaluated**. Commit `b151c9003`, on
this same branch, exists to stop exactly this. Use `ErrUnsafeSessionName` or a dedicated
sentinel, and update the two `require.ErrorIs` in `TestWriteSession_ContainmentDoesNotDependOnRepoPath`.
No non-test code does `errors.Is` on either sentinel, so this is message quality only.
**Missed by the same sweep:** `session_store.go:164` still returns
`ErrOutsideSessionStore: empty path` — an empty name is malformed, not outside anything.

### Minor (final review)

3. `session_store.go:158-160` — `Name`'s doc comment names only `ErrOutsideSessionStore`; it now
   also returns `ErrUnsafeSessionName` (`:176`).
4. `resume.go:905-909` — `"resume session started"` carries no `agent`; move it below `:996` or
   say so in the comment, since the commit claims to restore what main logged.
5. `manual_commit_pending.go:318` — the `assuming <agent>` warning fires for every
   *single-session* legacy checkpoint, where the assumption cannot be wrong. `totalSessions` is
   in scope at `:380`; gate on `totalSessions > 1` to keep the spec's intent without noise on the
   commonest resume.
6. Spec 1.10's "then audit the rest" of `external-agent-protocol.md` was never scheduled into a
   task. The reviewer spot-audited it and found no further contradiction, so it is closable —
   but record that someone did it.
7. Spec 1.7's golden-message test was consciously dropped;
   `TestValidateFileNameComponentRejectsSeparators` asserts no message, so "rejection is
   intended rather than incidental" is unpinned. One `errMsg` field fixes it.
8. `resume.go:988-991` overclaims — the `sessionIDErr == nil` gate is framed partly as safety,
   but in the only case it changes the agent is already instantiated. It is no-op avoidance.

### `docs/session-id-hardening-unit1-pr-description.md` — three factual errors

That file is committed and pushed, so these are live:
- The `b151c9003` paragraph claims `SessionStore.Name` also had a dot-component arm converted to
  `ErrUnsafeSessionName`. It didn't — `b151c9003` touches only `ValidateExternalSessionRef`;
  `Name`'s rooted check was **added** by `fe9210c6a`, as `ErrUnsafeSessionName` from birth. The
  document's own `fe9210c6a` paragraph contradicts this one.
- The `d1e6a62ed` paragraph inverts the bug: the `WithAgent` call had been **dropped entirely**;
  `types.AgentName(metadata.Agent)` was the *tempting wrong fix*, never the shipped behaviour.
- The `41aa15cd4` paragraph repeats the Gemini/Pi nesting error.
- Smaller: "eleven smaller defects" vs the spec's twelve Unit 1 items (1.12 produced no commit);
  and point 3's closing parenthetical reads as though component checks now run without a repo
  path — they don't, the ref is refused.

### Also flagged

The spec's *Test strategy* section committed to pinning resume shapes A, B, C and "all sessions
lack agent metadata" as a table test A/B'd against main. That remains proven only by throwaway
probes — the one written commitment in the spec this branch leaves open. Say so in the PR rather
than leaving it silent; it is the same evidentiary weakness the spec itself flagged.

## Eight deferred minors — triaged by the final review, none blocks merge

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
