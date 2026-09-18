# Session-ID hardening: follow-up design

**Date:** 2026-09-18
**Base:** PR #2405 (`codex/session-id-hardening-20260912`) at `8a70a5ef6`
**Status:** design, revised after adversarial review

## Why this document exists

PR #2405 fixes a reported path-traversal class in agent session IDs. A multi-lens review of
it at `a79ba75cc`, an adversarial validation pass over that review's own findings, an
independent re-review of the three commits since, and an adversarial review of this document
together produced a set of defects wider than one PR should carry. This document assigns
every one of them to a unit of work, says what each unit changes, and records the decisions
that are already settled so they are not relitigated.

Every defect below is tagged with how far it was taken — **executed**, **traced**, or **not
yet checked**.

**Items carrying an unresolved decision**, none of which may be assumed during implementation:
1.3 (forward or refuse an out-of-store ref with no `RepoPath`), 1.4 (whether to chmod
already-created directories), 1.8 (contested severity), 1.10 (protocol-doc audit), 1.12
(scoping of the removed model-extraction guard), Unit 2's `/./` and MSYS rows, Unit 3's cursor
reachability, and open question 4 — plus open questions 1–3, which are unresolved by
construction rather than by omission. Several of these are coupled — 1.3 governs Unit 2's
`get-session-dir` row, and 1.1's `:228` arm is conditional on Unit 2's `/./` row — so they
cannot be decided one at a time in isolation.

Findings that were **refuted** during validation are recorded in "Corrections" rather than
silently dropped, because two of them are currently encoded in shipped documentation and one
in a guard test that inherited the mistake.

## Units

Six units, in the order they should land.

| # | Unit | Target | Blocking for #2405? |
|---|---|---|---|
| 1 | Fixes to PR #2405 | stacked on #2405 | **yes** |
| 2 | External-preflight edges | stacked on #2405 | no |
| 3 | Sibling `.`/`..` validators | against `main` | no |
| 4 | `CLAUDE_CONFIG_DIR` + `clean --all` | against `main` | no |
| 5 | `SessionRef` containment | its own PR | no |
| 6 | Session-state namespace collisions | against `main` | no |

Units 2, 3, 4 and 6 are independent of each other. **Unit 5 depends on Unit 4.1 landing
first** — without `CLAUDE_CONFIG_DIR` honoured, the admission gate refuses the repo's own
real-agent e2e suite. Unit 1 is the only one gating #2405.

---

## Unit 1 — fixes to PR #2405

Stacked on #2405, or folded into it (open question 2). Twelve items.

**Acceptance bar: no NEW failure relative to the same command on the item's base commit** —
*not* an absolute count, and not "green". Measured 2026-09-18 at `8a70a5ef6`:
`go test ./cmd/entire/cli/ ./cmd/entire/cli/strategy/` produces **15 `--- FAIL` lines across 14
distinct top-level tests**, every one fetch/remote or checkpoint-remote
(`TestFetchAndRebase_*` ×8, `TestEnsurePrimaryRef_*` ×2, `TestEnableCheckpointPushRemote_Picker`
plus its `existing_explicit` subtest, `TestEnableCmd_BareEnableHealsEmptyOrphanFromCheckpointRemote`,
`TestFetchMetadataBranch_DisconnectedPreservesLocalCheckpoint`,
`TestGetBranchCheckpoints_HydratesRemoteDiscoveredStub`).

**This bar is about *test* regressions only.** `mise run check` — fmt, lint, `test:ci` — is
still required before every commit per CLAUDE.md, and CI enforces it. The comparison exists
because `check` cannot be green on this baseline, not because it is optional.

Earlier passes reported this set as "13" and "~14" — the spread is a counting artefact
(top-level tests vs `--- FAIL` lines vs subtests), which is precisely why the bar is a
comparison rather than a number. Two caveats for whoever re-measures: the failures are
environment-sensitive (this machine currently has a broken SSH agent), and they were previously
observed identically on `a79ba75cc` and on `main`, so they are pre-existing rather than
branch-induced. Re-measure on your own base commit before starting, and compare against that.

### 1.1 `ValidateExternalSessionRef` misreports containment — CONFIRMED (executed, twice)

`agent/session_store.go:228` and `:234` return `ErrOutsideSessionStore` for refs that are
demonstrably *inside* the store:

```
ref = <sessionDir>/./sess.jsonl
ValidateExternalSessionRef -> "path is outside the agent's session directory: ... contains a dot path component"
store.Name(ref)            -> "sess.jsonl", err=<nil>          # it is inside
store.ValidateExternalWriteRef(ref) -> <nil>                    # the store accepts it
errors.Is(err, ErrUnsafeSessionName) -> false
```

`f5aa52adc` introduced `ErrUnsafeSessionName` — "reporting `ErrOutsideSessionStore` for a
merely malformed ID names the wrong problem" — and in the *same commit* wrote
`ValidateExternalSessionRef`'s dot-component and rooted arms against the older sentinel. The
inconsistency is internal to one commit, not a regression across two.
(`git log -S ErrUnsafeSessionName -- cmd/entire/cli/agent/session_store.go` returns only
`f5aa52adc`.)

**Change:** `ErrUnsafeSessionName` at `:228` and `:234`. Keep `ErrOutsideSessionStore` at `:237`
("escapes its relative base") — that one is a genuine containment statement.
**`:228` is conditional on Unit 2's `/./` disposition** — if that row relaxes the dot-component
check, this arm is *retired* rather than re-sentinelled. Decide both together; `:234` is
unconditional. Unit 1 lands first, so an implementer working this item top-to-bottom would
otherwise never see the caveat.
**Test:** `errors.Is(err, ErrUnsafeSessionName) && !errors.Is(err, ErrOutsideSessionStore)`
for a dot-component ref and a rooted ref; the converse for an escaping ref. **Pin the message
text too** — sentinel and format string are separate here, so swapping the sentinel while
leaving "path is outside…" in the text would pass an `errors.Is`-only assertion.

### 1.2 The `ResolveSessionFile` guard under-covers, and CLAUDE.md asserts it doesn't — CONFIRMED (executed)

`agent/resolve_session_file_guard_test.go:57` sets a `cmd/**/*.go` pathspec.
`e2e/testutil/session_paths.go:45` calls `ag.ResolveSessionFile(sessionDir, meta.SessionID)`
with an ID read from checkpoint metadata, outside that pathspec and outside the allowlist.
The call is benign in itself, but it sits in exactly the copy-paste zone the guard's own
rationale names, and new agent E2E runners live under `e2e/`.

This is a defect inherited from the review that recommended the guard: that review's grep was
scoped to `cmd/` and reported "exactly 4 hits". **CLAUDE.md:1109** now states as repo-wide
fact that "the only sanctioned callers are `SessionFile` itself and `external`'s pure
delegation." That is false.

A repo-wide grep finds exactly three non-test callers: `agent/session_store.go`,
`agent/external/capabilities.go`, `e2e/testutil/session_paths.go:45`. Widening the pathspec
adds exactly one allowlist entry.

**Change:** pathspec to `:(glob)**/*.go`; add `e2e/testutil/session_paths.go` to
`resolveSessionFileCallers` with its reason; correct CLAUDE.md:1109.
**Test:** the guard already fails on a planted violation and on zero matches; add the new
entry's staleness direction.

> Everything else about the guard checks out: it delegates `--untracked`, `--no-color` and
> repo-selector scrubbing to `testutil.GitGrepGuard`, restricts to `*.go` with a fatal
> unparseable branch, and enforces both directions.

### 1.3 The preflight's `RepoPath`-independence is both untested and untrue — CONFIRMED (executed)

Two distinct problems behind one doc comment.

**Untested.** All nine cases in `TestWriteSession_RejectsUnsafeReferenceBeforeSubprocess`
(`agent/external/external_test.go:279-396`) pass `RepoPath: t.TempDir()`. The doc comment on
`ValidateExternalSessionRef` says the independence exists because "gating them on a field both
current callers happen to set is a check that disappears for the next caller that does not" —
and a refactor that re-gated on `RepoPath` would pass the entire suite.

**Untrue.** Executed: `ValidateExternalSessionRef("/foo/bar")` returns
`(filesystemPath=true, err=nil)` — it defers containment to the store half, and
`external.go:215` then skips that half entirely when `RepoPath == ""`. So the containment half
is still precisely "a check that disappears for the next caller that does not set `RepoPath`".
The lexical half survives; the containment half does not.

**Change:** decide and implement one of — forward an absolute out-of-store ref when no
`RepoPath` is available (and say so in the doc comment), or refuse it. Either way the doc
comment and `external-agent-protocol.md` must match the code; the protocol doc currently
promises the store check unconditionally.
**Test:** two cases with `RepoPath: ""` — a relative `../outside.jsonl` (lexical half, must
refuse) **and** an absolute ref outside the store (containment half, asserting whichever
behaviour is chosen).

### 1.4 `0o700` does not reach the directory that holds transcripts — CONFIRMED (executed)

Under `umask 022`:

```
outer                   0700    <- fixed by 979b28a5a
outer/inner (store)     0700    <- fixed by 979b28a5a
outer/inner/sub         0750    <- NOT fixed
outer/inner/sub/a.jsonl 0600
```

`agent/session_store.go:325` still creates nested directories at `0o750`. For every agent that
nests — Copilot (`<dir>/<id>/events.jsonl`), Gemini (project hash), Pi (encoded repo path) —
that is the directory actually holding the transcript. Separately, on the common path the
*agent* creates the store root, so the `MkdirAll` is a no-op and the `0700` never applies; the
fix only covers "Entire got there first", which is the `resume`-onto-a-fresh-machine case.

Severity low: all transcripts are written `0o600` (verified at all eight `WriteSessionFile`
call sites), so the exposure is directory listing — session IDs — not content.

**Change:** `osroot.MkdirAllNoSymlink(root, dir, 0o700)` at `session_store.go:325`. Also
correct the CLAUDE.md sentence `8a70a5ef6` added — "The directory is created `0700`, not
`0750`: it holds session transcripts" — which is true only of the root, and only when Entire
created it.
**Decision needed:** an already-created `0750` nested directory is not chmod'd by this change,
so the exposure persists for every existing user. Either state that explicitly as accepted, or
add a chmod-on-open. **Recommend stating it** — a chmod-on-open mutates a directory the agent
owns.
**Test:** extend the existing `//go:build !windows`, non-parallel, `syscall.Umask(0o022)` test
to assert the nested level too.

### 1.5 `logging.WithAgent` still dropped — CONFIRMED (executed)

`resume.go:904` is `logging.WithComponent(ctx, "resume")`; main had
`logging.WithAgent(logging.WithComponent(ctx, "resume"), ag.Name())`. Resume debug lines lose
the `agent` attribute. None of the three new commits addressed it.

**The obvious fix is wrong.** `CheckpointInfo.Agent` is a `types.AgentType`
(`manual_commit_types.go:39` — "Human-readable agent name (e.g., \"Claude Code\")") and
`WithAgent` takes a `types.AgentName` (`logging/context.go:93`). `AgentTypeClaudeCode` is
`"Claude Code"`; `AgentNameClaudeCode` is `"claude-code"`. So `types.AgentName(metadata.Agent)`
compiles and logs `agent="Claude Code"` — a value no other log line in the tree emits, and not
what main logged.

**Change:** restore the attribute from the resolved agent's `ag.Name()`. Since `8a70a5ef6`
moved `ResolveAgentForResume` into the fallback branch, resolve it once at the top of
`restoreResumeSessions` for the log context and reuse it in the fallback — and a failure to
resolve must not become fatal on the path that no longer needs the agent.
**Test:** capturing `slog` handler asserts `agent="claude-code"` (the name, not the type) on
the resume path.

### 1.6 `SessionRefIsFilesystemPath` is exported with no outside caller — CONFIRMED

`agent/session_store.go:201`. Widens the package API for nothing.

**Change:** unexport. **Test:** compilation.

### 1.7 `\` is rejected on Unix, where it is a legal filename byte — CONFIRMED (executed)

```
store.WriteFile(`weird\name.jsonl`, ...) ->
  validate session file name: unsafe session file name:
  invalid file name component "weird\name.jsonl": contains path separators
```

`validateWriteName` splits on `/` only (`filepath.ToSlash` is identity on Unix), so a
backslash stays inside a component and is then rejected. Reachable only through an external
plugin's `session_ref` on Linux/macOS.

**Decision:** keep the rejection, fix the justification. It is fail-closed, consistent with
`ValidateSessionID`'s long-standing unconditional `ContainsAny(id, "/\\")`, and it matches the
settled decision that session IDs travel in checkpoints to Windows readers. What is wrong is
the doc comment, which justifies it as "a separator is rejected rather than split on" — on
Unix `\` is not a separator.

**Change:** rewrite the comment to state the cross-platform rationale.
**Test:** golden message for a backslash-bearing component, asserting rejection is intended
rather than incidental.

### 1.8 `SessionStore.Name` still returns a rooted string on Windows — TRACED; severity CONTESTED

`Name("\foo")` takes the relative branch, cleans to `/foo`, and
`paths.IsRelativeTraversal("/foo")` is false, so a rooted string is returned as a "name inside
the store". `f5aa52adc` refactored these lines into `cleanRelativeName` / `relativeNameEscapes`
without changing the behaviour. Confirmed on Unix that the downstream coverage holds:
`validateWriteName("/foo")` → "file name component cannot be empty".

**The residual is contested and must be settled before this item is implemented.** Two readings
exist and they disagree about direction and severity:

- *Cosmetic:* the wart flips a `SessionEnd` into a `TurnEnd`; no I/O follows.
- *Not cosmetic:* the wart makes `isSubagentAgentStop` return `true` where it should return
  `false`, flipping a `TurnEnd` into a **`SessionEnd`** — and `handleLifecycleSessionEnd` ends
  the session and runs the eager condense.

This also sits in tension with the Corrections row calling the same residual unreachable.

**Separate the two questions.** The *direction* is a pure reading of `buildAgentStop` and needs
no runner: the wart makes `Name` succeed, which makes `actualName != expectedName` reachable,
which returns `true`, which yields a `SessionEnd`. Settle that by reading. Only *reachability* —
whether a real Copilot payload ever carries a drive-relative `transcriptPath` — needs execution
under `GOOS=windows`, and that is the half that decides severity.

**Change:** reject `os.IsPathSeparator(p[0])` before the relative branch's `Clean`.
**Test:** this item currently has **no runnable test**, and that must be fixed as part of it.
On a Unix host both `Name` and `cleanRelativeName` treat `\` as an ordinary byte, and CI only
*cross-compiles* for Windows. Either refactor `cleanRelativeName` to take the separator
predicate as a parameter so it is platform-parameterisable, or state that the item ships
test-only under a Windows runner. Name the chosen helper in the plan.

### 1.9 `RestoreLogsOnly`'s new `point.Agent` fallback — PLAUSIBLE, not confirmed

`strategy/manual_commit_pending.go:398-401`. A multi-session checkpoint where session *i*
records no agent but was produced by a *different* agent than the checkpoint's top-level one
now has its transcript written into the checkpoint agent's directory, and `entire resume`
prints that agent's resume command for it. Previously it was skipped with a warning.

The fallback is net-positive — it is what made resume shape C better than main — and no shipped
code path was found that produces a mixed-agent checkpoint with a partially-empty per-session
agent field.

**Change:** keep the fallback; emit a stderr warning when it is used, naming the assumed agent,
so a mixed-agent checkpoint is visible rather than silent.
**Test:** a two-session checkpoint where one session lacks agent metadata asserts both the
restore and the warning.

### 1.10 Verify the protocol doc matches the relaxed preflight — NOT YET CHECKED, one known defect

`8a70a5ef6` edited `docs/architecture/external-agent-protocol.md` (15 changes) as part of
"document what this branch changed". Nobody has audited the document as a whole against the
preflight's actual behaviour.

**One concrete defect is already known**, which makes this item closable rather than
open-ended: the doc says a filesystem-shaped ref must resolve inside the directory the plugin
itself reported from `get-session-dir`, unconditionally — but `external.go:215` skips the store
half entirely when `RepoPath == ""` (see 1.3). Either the doc or the code is wrong, and 1.3
decides which.

**Change:** fix that contradiction, then audit the rest. The doc must describe the preflight
**as it behaves after Unit 1**, per branch — not as it may behave after Unit 2. Today's actual
behaviour, verified at `8a70a5ef6`:

- *Opaque relative refs:* rooted is fatal; escaping its own base is fatal; a non-escaping dot
  segment (`tenant/../session-key`) is forwarded. Nothing else is checked — no store, no
  components.
- *Filesystem-shaped refs* (absolute, or carrying a volume): a `.` or `..` component is fatal;
  a component with a volume separator, a Windows device name, a control character, or a
  trailing period/space is fatal; a symlinked component or leaf is fatal; resolving outside the
  store is fatal — **but only when `RepoPath` is set**, which is 1.3's open decision and must be
  stated either way. An empty `session_dir` and a failing `get-session-dir` are **both fatal
  today**.

> **Do not describe Unit 2's relaxations here.** An earlier draft of this item listed empty
> `session_dir`, `get-session-dir` errors, `./` components and POSIX-legal colons/device names
> as "not fatal". That is the *settled policy target*, not the current code — all five are fatal
> at `8a70a5ef6` (the colon case is already pinned by `external_test.go:355-359`, and the
> device-name regex matches `com1.jsonl` through its `(?:\..*)?` tail on every platform). Unit 1
> ships first, so writing the target state into the doc would replace a **true** sentence at
> `external-agent-protocol.md:533` with a false one. It would also contradict Unit 2 directly:
> the `/./` row is "decide, then fix or document", and the device-name row *narrows* rather than
> relaxes. Revisit this section when Unit 2 lands, not before.

**Test:** the 1.3 test pins whichever behaviour is chosen; the rest is a read.

### 1.11 The resume unsafe-ID guard is gated on `restoreErr == nil`, defeating its purpose — CONFIRMED (traced)

`resume.go`'s `metadata.SessionIDs` scan runs only when `restoreErr == nil && len(sessions) == 0`.
Its comment says falling back "would quietly restore the top-level session instead of reporting
that" — but when `RestoreLogsOnly` returns an error, the scan is skipped and the fallback runs
anyway, provided the top-level `sessionID` is safe. A tampered checkpoint that *also* produces a
restore error therefore gets exactly the silent top-level restore the guard exists to prevent.

**Change:** run the `SessionIDs` scan whenever the fallback is about to be taken, not only on the
no-error path.
**Test:** a tampered checkpoint that both errors and carries an unsafe stored ID must report the
unsafe ID rather than silently restoring the top-level session.

### 1.12 `8a70a5ef6` removed the `sessionIDIsSafe` gate on `ExtractModelFromTranscript` — NOT YET ASSESSED

The branch deleted the guard on `ExtractModelFromTranscript(ctx, env.TranscriptPath)` in
copilot's `buildAgentStop`. The deletion is probably right — the guard keyed on the wrong field,
which is why the original review flagged it — but the read it guarded is an unconfined
`os.ReadFile` of a payload-supplied path, and nothing replaced it. A reviewer of this branch
will ask, and this document must answer.

**Change:** none, if the position is "the guard was incoherent and the read belongs to Unit 5".
State that position explicitly in the PR description rather than leaving a silent deletion. If
instead the read should be confined now, it is a Unit 5 item, not a Unit 1 one.
**Test:** none; this is a documentation and scoping decision.

---

## Unit 2 — external-preflight edges

Stacked on #2405, non-blocking. The author triaged these as "edges this PR introduced, none
blocking"; the re-review executed four of the six against the delta and agreed — unchanged, all
fail-closed, none reachable without a cooperating plugin.

Every row has a disposition, not just a description.

| edge | detail | disposition |
|---|---|---|
| MSYS form rejected | `/c/Users/u/agentx/s.jsonl` rejected as rooted on Windows, a shape `paths.normalizeMSYSPath` compensates for elsewhere | **fix** — normalise the MSYS form before the rooted check. `paths.normalizeMSYSPath` is unexported (`paths/paths.go:352`) and used only by `ToRelativePath`; exporting it, or lifting it to a shared helper, is part of this row's work, not a free call. Settle first whether `ValidateExternalSessionRef` should be doing Windows shell-form normalisation at all. |
| `d:opaque-key` misrouted | single-char volume prefix routes an opaque DB key into the filesystem branch; `db:key` unaffected | **document** — the ambiguity is inherent; note it in the protocol doc |
| `/./` rejected | naive `dir + "/./" + id` concatenation refused | **decide, then fix or document** — the check is deliberate. `session_store.go:224-225`: "A dot component survives no round trip through a plugin that joins or normalizes it, so it is refused rather than cleaned away here." Cleaning first is exactly what that comment declines to do. Relaxing it also retires the `:228` arm that 1.1 re-sentinels, so either 1.1 skips `:228` or this row is "document". **These two items must be decided together.** |
| `get-session-dir` errors fatal | a plugin whose `get-session-dir` fails hard-fails a write that previously never invoked it | **fix, but bound by 1.3** — "skip the preflight" *is* "forward it", which is the same question 1.3 decides for `RepoPath == ""`. Whatever 1.3 decides governs both; this row must not pick independently. |
| legacy-ID hard fail | `resume.go`; theoretical, no shipped agent emits such IDs | **document** — accept, note in the PR |
| device-name regex | `COM0`/`LPT0` uncovered, plus the `aux.js` caveat | **fix** — extend the regex; `COM0`/`LPT0` are reserved per Microsoft's naming rules, so this is fail-closed narrowing only. **Re-run the exhaustive false-positive sweep afterwards** — the existing zero-false-positive result was measured against the current regex and does not carry over. |

**Test:** stub plugin binaries driving `external.Agent.WriteSession`, one case per fixed row,
asserting the subprocess marker is present where the edge is relaxed and absent where it is not.
Three of the fixed rows are Windows-only behaviours (MSYS, `d:`-volume, device names), and CI
cross-compiles rather than runs Windows (see 1.8). State in the plan how those are exercised —
a separator/volume predicate parameterised into the helper, or a `GOOS=windows` runner.

---

## Unit 3 — sibling `.`/`..` validators

Against `main`. The same exact-match `name == "." || name == ".."` weakness PR #2405 fixed in
`ValidateSessionID`, in two validators it did not touch.

**`plugin_store.go:169 validatePluginName`** accepts `...`, `.. `, `. `, `foo.`, `C:foo`, `com1`.
Not exploitable — `PluginDataDir`'s only caller is reached after `resolvePlugin` found a real
executable on `$PATH`, so the attacker already has code execution as the user, and all CLI-side
I/O goes through `pluginRoot`'s `os.Root`.
**Change:** call `validation.ValidateFileNameComponent` **in addition to** the rules
`validatePluginName` already has — it supplies the four missing ones (`...`, `. ` and `foo.` via
the trailing period/space rules, `C:foo` via the volume separator, `com1` via the device-name
regex) and replaces only the exact-match `name == "." || name == ".."`. **Keep** the leading-`-`,
`agent-` prefix and `hasTerminalControlChars` checks: the first two are dispatcher and
protocol rules the component validator knows nothing about — dropping `agent-` would let a plugin
claim a name the external-agent protocol reserves — and the third rejects Bidi controls, which
`unsafeFileNameComponentReason`'s `r <= '\x1f' || r == '\x7f'` test cannot reach because every
Bidi control is above U+007F. See the anti-spoofing rationale at `plugin_store.go:129-142`, whose
whole subject is spoofing the `[official]` marker in the install confirmation. Do not hand-roll a
second copy of the component rules.
**Test:** golden reject table for the six inputs above, **plus** regression cases for `-foo`,
`agent-foo` and a Bidi-bearing name, so the merge cannot silently drop them.

**`plugin_store.go:116-117`'s "guarantees containment" comment.** Downgraded from "provably
false" to **overstated** — see open question 4; none of the named inputs escapes after
`filepath.Join`+`Clean`, Windows included.
**Change:** rewrite to say the `os.Root` is what contains this, not the check. **Test:** none.

**`cursor/images.go:127 sessionIDFromTranscriptPath`** — `"/x/.. .jsonl"` yields `".. "` (the
`.`/`..` check is at `:133`), which reaches `findStoreDBs` and is joined as a literal path
component at `:165`. Contained by `filepath.Base`, read-only, size-capped, magic-byte filtered,
Windows-only.
**Reachability must be re-verified first, and has two answers.** At `a79ba75cc` it was reachable
because `cursor/lifecycle.go` returned the payload transcript path verbatim. `f5aa52adc` changed
that to resolve through `SessionFile` — but `f5aa52adc` is **not in main**, which is this unit's
target. So: against main today it is reachable; after #2405 lands it may not be, unless the route
still reads `state.TranscriptPath` from a pre-upgrade state file.
**Change:** replace the ad-hoc check with `validation.ValidateSessionID`.
**Test:** `/x/.. .jsonl` → `""` (the `".. "` case this item is actually about), `-x.jsonl` → `""`
(leading dash), `com1.jsonl` → `""` (device name). Note that `../x.jsonl` yields `"x"` and **must
keep doing so** — `filepath.Base` strips the traversal before the check ever sees it, so it is not
a case this check is for.

**Explicitly NOT in scope:** `checkpoint/parse_tree.go:401-412`. It splits on `/` and rejects
`.`/`..`/empty/absolute/`.git`/`git~1` per segment — the claim that it shares this weakness was
refuted.

---

## Unit 4 — `CLAUDE_CONFIG_DIR` and `clean --all`

Against `main`. Two pre-existing live bugs, unrelated to session-ID hardening, both confirmed by
execution twice.

### 4.1 `CLAUDE_CONFIG_DIR` unhonoured — CONFIRMED, high for affected users

`agent/claudecode/claude.go:94-108` (`GetSessionDir`) and `:113-119` (`GetSessionBaseDir`)
hardcode `~/.claude/projects/…`. Only `generate.go:115` reads the variable. Demonstrated on a
machine where `CLAUDE_CONFIG_DIR=/Users/h/.claude-work`, `~/.claude/projects` does not exist, and
`~/.claude-work/projects/<sanitized>` does:

- `entire attach` reports `transcript not found for agent "claude-code" with session <id>; is the
  session ID correct?` while the transcript is on disk and the ID is correct.
- `entire resume` **silently restores to the wrong place** — creates
  `~/.claude/projects/<sanitized>/`, writes the transcript, reports success, and `claude --resume`
  never sees it. This is the worst failure mode in the whole set.
- `AgentForTranscriptPath` returns `ok=false`, silently disabling the #1262 forwarded-hook guard
  and removing the strongest signal from `resolveSessionAgentType` / `correctSessionAgentType`.

Not affected: hook capture and checkpointing, which re-resolve from stored `state.TranscriptPath`.

**Change:** one `claudeHomeDir()` helper honouring `CLAUDE_CONFIG_DIR`, used by both functions, by
`generate.go:115`, and by the four `~/.claude` joins in `discovery.go:42-52` (lines 42, 50, 51,
52) which have the same defect for skills, commands and agents.
**Test:** `t.Setenv("CLAUDE_CONFIG_DIR", …)` and assert `GetSessionDir`, `GetSessionBaseDir` and
`AgentForTranscriptPath` all resolve under it.

### 4.2 `listAllTempFiles` basename bug — CONFIRMED, medium

`clean.go:549-556` discards the walk's root-relative name and appends `d.Name()`;
`removeTempFileNoSymlinks` (`:616`) then rebuilds `.entire/tmp/<basename>`.
`osroot.WalkDirNoSymlinks` recurses, so nested files are counted but keyed wrongly:

```
listAllTempFiles -> ["pi-active-session", "top.json"]
deleteTempFiles  -> deleted=["top.json"] failed=[]
BUG: .entire/tmp/pi/pi-active-session still exists
```

Real producer: `agent/pi/lifecycle.go:332` writes `.entire/tmp/pi/pi-active-session` on every pi
hook. The ENOENT is swallowed at `:591-596`, so the file is reported as neither deleted nor failed
and survives every `clean --all` forever. A basename collision additionally double-counts in the
"Delete N items?" prompt.

**Refuted:** no *wrong* file is deleted — a collapsed basename can only name a file already in the
deletion set.

**Change:** append `strings.TrimPrefix(name, entireTmpName+"/")`. `osroot.OpenParentNoSymlinks`
already walks multi-segment names component-by-component, so `removeTempFileNoSymlinks` needs no
change. `listOrphanAgentTemps` at `:491-497` already does this correctly and is the model.
**Test:** a nested temp file is listed with its relative path and actually deleted.

---

## Unit 5 — `SessionRef` containment

Its own PR. This is the only item in the set that crosses a machine boundary.

### The hole

`event.SessionRef` is never validated anywhere. `lifecycle.go` takes it verbatim →
`ag.ReadTranscript` → `os.ReadFile` → `.entire/metadata/<sid>/full.jsonl` → condensation →
committed and pushed. Demonstrated end-to-end: a canary planted at `/tmp`, fed in as
`transcript_path` to `entire hooks claude-code stop`, reached a real remote under **both**
checkpoint backends. Redaction is **not** a mitigation — an SSH private key and `/etc/passwd`
lines shipped verbatim; only an AWS-key line was replaced.

> **Evidence note.** This demonstration was performed once, during review, against a scratch bare
> remote, and no artifact was preserved. It is the highest-stakes claim in this document.
> **Reproduce it before B1 is written** and attach the transcript to the PR.

`entire hooks <agent> <verb>` has no caller authentication; the gates are "inside a git repo" and
"Entire enabled".

Pre-existing on `main`, and tracked: `agent/transcript_read_guard_test.go` is a ratchet over
exactly this gap, with a written deferral rationale.

### What B1 actually closes — and what it does not

The gate closes the **confused-deputy** case: an attacker who controls only `transcript_path`.

It does **not** close the case where the attacker can also write files, because every admissible
root is writable by the agent — `~/.claude/projects/<hash>/` and `<repoRoot>/.entire/tmp` are
ordinary filesystem locations. Against a prompt-injected agent with file-write,
`cp ~/.ssh/id_rsa ~/.claude/projects/<hash>/<uuid>.jsonl` followed by the same hook invocation
reaches the same remote, admitted.

That is still a real reduction — it removes the one-step read of `~/.ssh/id_rsa`, `/etc/passwd` or
`.env` without touching the filesystem, and it makes exfiltration leave evidence on disk. But
**B1's PR description must not claim closure**, and its threat model must say which attacker it
addresses.

### The design — settled

Admissible roots are the **union**, not a fallback chain, of:

1. `ag.GetSessionDir(repoPath)`
2. `ag.GetSessionBaseDir()` where the agent provides one
3. an agent-declared extra root via a new optional `TranscriptRootsProvider`, implemented only by
   OpenCode (`<repoRoot>/.entire/tmp`)

The union is mandatory, not stylistic. `GetSessionBaseDir` deliberately ignores
`ENTIRE_TEST_CLAUDE_PROJECT_DIR` — the comment at `claudecode/claude.go:111-112` says so — so a
"base dir else session dir" ordering refuses every ref in the integration and e2e harnesses for
Claude Code, Cursor, Gemini, Droid and Pi. Tier 3 is required, not defensive:
`opencode/lifecycle.go:182-194 sessionTranscriptPath` returns `<repoRoot>/.entire/tmp/<id>.json`
while its `GetSessionDir` returns a temp directory it no longer uses.

Comparison is `pathHasDirPrefix` (already Windows case-folding) over **resolved** paths on both
sides: walk up to the longest existing prefix, `EvalSymlinks` that, re-append the remaining
lexical components. This handles macOS `/var` ↔ `/private/var`, handles a leaf OpenCode has not
created yet, and refuses a symlink planted inside the store pointing at `~/.ssh/id_rsa`.

`TranscriptRoots` returning `(nil, nil)` means "no opinion" and admits — the settled
external-plugin policy applied verbatim.

### Commits

| # | contents | if it stalls |
|---|---|---|
| B1 | `agent.AdmitTranscriptRef`, its call sites, **fail-soft** refusal, telemetry, `status`/`doctor` surfacing | confused-deputy case already closed |
| B2 | turn-end copy reads through the admitting `os.Root`; ratchet entries drop | gate remains a check; nothing reopens |
| B3 | in-tree cleanup; protocol removal split out — see below | the unconfined reads survive, correctly counted |

B2 and B3 are separable by construction.

### B3 is a protocol break, not an API cleanup

`read-session` (`external-agent-protocol.md:124`) and `get-session-id` (`:85`) are documented
subcommands of a cross-process contract with third-party binaries; `HookInput` is at `:467`.
Removing them needs a protocol-version bump, a note in that document, and a decision about
existing plugins. "Nothing in-tree uses them" is true only of non-test code: there is **1**
non-test `.ReadSession(` call site and **35** in tests, ~12 of them in
`cmd/entire/cli/integration_test/`.

**Change:** split the protocol removal out of B3 and let B3 be the in-tree cleanup only.
**Recommend splitting** — a protocol break should not ride along with a containment fix.

> The count of unconfined reads B3 leaves behind is **9 by one measure, 7 by another**: 7 direct
> `os.ReadFile(input.SessionRef)` (cursor, claudecode, codex, factoryaidroid, geminicli,
> copilotcli, vogon) plus 2 via `agent.ReadTranscriptFile(input.SessionRef)` (opencode, pi). State
> which measure the ratchet uses when the entry is written.

### Settled decisions

- **Fail-soft**, not fail-hard. The cost is stated plainly rather than buried: a mis-modelled
  agent stops checkpointing, and the loudest symptom is a line in `entire status`. The degraded
  marker and telemetry are part of B1, not an afterthought.

### Constraints any implementation must not break

Measured, not assumed:

- Claude Code launched from a repo **subdirectory** already returns `ok=false` from
  `AgentForTranscriptPath`.
- **Every** opencode session returns `ok=false` by construction.
- macOS `/var` vs `/private/var` never matches — `pathHasDirPrefix` does no symlink resolution.
- Hooks are fail-open by design; a hard failure breaks the user's agent turn.

Unit 4.1 is a **precondition**: without `CLAUDE_CONFIG_DIR` honoured, the gate refuses the repo's
own real-agent e2e suite.

**Test:** a table over all nine built-in agents plus an external stub, asserting each agent's own
real transcript path is admitted — the test that would have caught the `GetSessionBaseDir`
ordering error.

---

## Unit 6 — session-state namespace collisions

Against `main`. `.git/entire-sessions/` mixes two namespaces: session state files
(`<sessionID>.json`) share a flat directory with control files written by other subsystems —
`review-pending.json` (`review/marker_fallback.go:36`), `.warn-stale-ended`, `<sessionID>.model`,
`<sessionID>.trail-scope.json`.

A session ID of literally `review-pending` passes `ValidateSessionID` — alphanumerics and a
hyphen — and `Save` writes to the same path, replacing the marker via atomic rename. The marker's
`Prompt` field is consumed as `opts.ReviewPromptOverride` for a permission-bypassed agent. A
case-folded variant collides on APFS/NTFS.

`.entire/tmp/` is a second instance of the same class: Entire keys state there by **prefix**
(`pre-prompt-<sessionID>.json`, `pre-task-<toolUseID>.json`, with `FindActivePreTaskFile` scanning
and stripping affixes back into a tool-use ID) while OpenCode writes a bare `<sessionID>.json`
into the same directory.

**Correction carried forward:** an earlier analysis claimed `List`/`Load`/`IsStale` deletes
`review-pending.json` immediately because `StartedAt` is never backfilled. That is wrong —
`State.StartedAt` (`session/state.go:154`) and `PendingReviewMarker.StartedAt`
(`review/marker_fallback.go:56`) share the json tag `started_at`, so the marker's timestamp is
unmarshalled and the threshold is `StaleSessionThreshold` = 7 days.

**This unit has a direction, not a design, and must not be planned from this document.** The
direction is: a reserved subdirectory for control files, plus a reserved-name rule in
`ValidateSessionID`, with migration for existing on-disk markers. It is a storage-layout change
with a migration and deserves its own brainstorm — see open question 3.

---

## Corrections — claims that were made and are wrong

Recorded because two are currently encoded in shipped documentation and one in a guard test.

| Claim | Status |
|---|---|
| "An `Lstat`-based `SameFile` pin only stops a race, not a pre-planted symlink" | **False, but the original correction named the wrong mechanism.** A pre-planted symlink is rejected by the explicit `before.Mode()&os.ModeSymlink != 0` check (`osroot.go:609`, `:670`), not by `SameFile`, which compares a second `Lstat` against `root.Stat(".")` in `validateOpenedRoot`. And the follow-on — "it rejects a dotfile-managed `~/.claude` by the same mechanism" — concerns a path that does not use these helpers at all: the session store deliberately uses plain `os.OpenRoot(s.dir)` and follows the link (`session_store.go:116-135`). **Conclusion stands** — no mechanism-level middle ground exists, and a real one must be policy — but not for the reason first given. |
| "Copilot's `GetSessionDir` uses the hook payload's CWD" | **False.** Signature is `GetSessionDir(_ string)`. |
| "`normalizeGitTreePath` shares the exact-match `.`/`..` weakness" | **False.** Per-segment (`parse_tree.go:401-412`). |
| "One unsafe ID fails the whole resume" | **Overstated — and the first correction was also wrong.** `RestoreLogsOnly` skips and continues, so one unsafe ID among several that restore is harmless. But the hard error is gated on *zero sessions restored*, not on *all IDs unsafe*: any mix of skips that leaves the count at zero, plus one unsafe ID anywhere in `metadata.SessionIDs`, produces it. See also 1.11. |
| "Moving the `:` check into `unsafeFileNameComponentReason` is byte-identical" | **False** for messages, but a differential fuzz of 3,633 inputs shows **zero** accept/reject changes for `ValidateSessionID`. The reorder is safe. The fuzz is not reproducible from this document. |
| "`external.go` duplicates `SessionStore.Name` and the copy is removable" | **False.** Different empty-ref handling and a rooted check `Name` has no counterpart for. `f5aa52adc` collapsed it correctly anyway. |
| "The copilot `isSubagentAgentStop` residual is a live misclassification" | **Probably false, but the stated reason was wrong.** `DispatchLifecycleEvent` validates `SessionID` (`lifecycle.go:67-71`), but the flip is driven by `env.TranscriptPath`, which dispatch never validates. The conclusion likely still holds because a real Copilot payload never carries a drive-relative `transcriptPath` — but that is the argument, not the one first given, and 1.8 marks the severity as contested. |
| "Exactly 4 `ResolveSessionFile` callers" | **False.** The grep was scoped to `cmd/`; `e2e/testutil/session_paths.go` is a fifth. Now encoded in CLAUDE.md:1109 and the new guard test — see 1.2. |
| "The `.git` session ID poisons the shared checkpoint branch" | **False.** `Tree.Encode` → `ValidTreePath` makes it a hard write failure. |
| "Agent session stores have no sensitive sibling" | **False.** See Unit 6. |
| "`ErrUnsafeSessionName` was introduced by `979b28a5a`, and `f5aa52adc` moved the lines without fixing them" | **False.** `f5aa52adc` introduced both, in one commit. See 1.1. |
| "`plugin_store.go`'s 'guarantees containment' is provably false" | **Overstated.** See Unit 3 and open question 4. |
| "The `SessionRef` exploit closes at B1" | **False as stated.** B1 closes the confused-deputy case only; every admissible root is agent-writable. See Unit 5. |

## Confirmed clean — do not relitigate

- The Windows device-name regex has **zero** false positives. Exhaustive over all hex 3- and
  4-character prefixes, 2M UUIDs, 2M ULIDs, and all `[a-zA-Z0-9_-]` strings of length ≤ 4
  (rejected set is exactly 176 strings, every one a device-name case variant). No repo fixture is
  rejected. **This result is scoped to the current regex** — Unit 2 widens it for `COM0`/`LPT0`,
  and the sweep must be re-run afterwards rather than carried over.
- The checkpoint-metadata → path vector is properly defended. A crafted checkpoint with
  `session_id: "../../../../../../../../tmp/pwned"` was rejected at two independent gates and wrote
  nothing; attempts via the agent's own layout and via `RestoredSessionPathResolver` also failed.
- Build, vet, lint, gofmt and `GOOS=windows` cross-compile are clean on the branch.

**Not in this list, deliberately:** the three resume regression shapes. They are fixed, but they
were proven by throwaway probes, not by committed tests — a weaker evidentiary class than the
device-name exhaustion above. Pinning them is a test-strategy item, not a settled fact.

## Test strategy

- **Resume shapes A, B, C and "all sessions lack agent metadata"** get pinned as a table test,
  A/B'd against `main` semantics. Currently proven only by throwaway probes.
- **Permissions** (1.4) need `//go:build !windows`, non-parallel, `syscall.Umask(0o022)`, asserting
  both the store root and a nested level.
- **Sentinels** (1.1) are asserted with `errors.Is` in both directions **and** by message, since
  the two are separate strings.
- **The guard test** (1.2) is verified by planting an untracked decoy, which is how the existing
  coverage gap was found.
- **`RepoPath: ""`** (1.3) needs both a relative and an absolute case.
- **1.8 has no runnable test today** and the item is not done until it does.
- Unit 5's admission predicate needs a table over all nine built-in agents plus an external stub.

## Open questions

1. **Base strategy.** The agreed plan was to rebase #2405 onto `main` and stack. That was decided
   when the tip was `a79ba75cc`, which did not touch CLAUDE.md and rebased cleanly. `8a70a5ef6`
   edits CLAUDE.md, which has 13 commits of churn on `main`, so the rebase now conflicts there.
   Options: resolve the conflict during the rebase (rewrites the author's doc commit), or merge
   `main` into the follow-up branch instead (leaves #2405's history alone, one merge commit).
   **Recommendation: merge.**
2. **Unit 1 placement.** Folded into #2405, or stacked as its own PR? Folding keeps the PR
   self-consistent; stacking keeps #2405 reviewable against what has already been reviewed.
3. **Unit 6 scope.** This document gives a direction, not a design. It needs its own brainstorm
   before it is planned.
4. **`plugin_store`'s "guarantees" claim.** Downgraded to "overstated" because no supplied input
   actually escapes after `filepath.Join`+`Clean`. If the intended proof is `com1`/`aux` resolving
   to a Windows *device* rather than a file, that is a different statement and the comment should
   say it. Decide which, or drop the item to a comment fix only.
