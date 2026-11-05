# Deepening opportunities

An architecture review of otman using the `codebase-design` vocabulary (module, interface, depth, seam, adapter, leverage, locality) and the domain language of `GLOSSARY.md`. Notes only: no code was changed. Candidates are ranked by value. None of them contradicts an ADR; ADRs 0003, 0005 and 0006 are constraints each design must keep.

## Where the code stands

| Package | Non-test lines | Direct tests |
| --- | --- | --- |
| `internal/cli` | ~5,250 | golden transcripts through `cli.Run` (`golden_test.go`; ~9.7k test lines across the repo) |
| `internal/item` | ~1,340 | none |
| `internal/vault` | ~1,720 | none |
| `internal/frontmatter`, `wikilink`, `config`, `fsutil` | ~1,200 | yes |

The leaf modules (`frontmatter`, `wikilink`, `fsutil`) are deep and tested at their interface. The two modules that hold the domain, `item` and `vault`, are tested only through the CLI, and `internal/cli` has absorbed domain logic that belongs behind them. That shape drives candidates 1 to 3. Recent history agrees: `internal/item/item.go` (16 touches in the last 60 commits), `cli/cli.go` (12), `vault/item.go` (11), `vault/journal.go` (10) and `cli/list_cmd.go` (10) are the hot spots.

---

## 1. An Item-mutation module (Strong)

**Evidence.** Every command that writes an Item repeats the same sequence: `resolveRef` → `v.ReadItemFile` → pure `item.*` transform → `writeError` → `v.WriteItemFile` → `summarize` → `emit`. See `setStatus` (`cli/status_cmd.go:50`), `comment` (`cli/comment_cmd.go:32`), `setClaim` (`cli/claim_cmd.go:53`), and the same shape in `edit_cmd.go`, `label_cmd.go`, `relation_cmd.go`, `create_cmd.go`. `ReadItemFile`, `WriteItemFile`, `summarize` or `writeError` appear in 14 files of `cli`. Cross-cutting helpers are parked in whichever command file came first: `resolveRef` lives in `view_cmd.go:250`, `withVault` in `project_cmd.go:113`, `writeError` in `status_cmd.go:116`.

**Problem.** The `item` functions are a good pure seam, but the interface a command has to learn is the whole protocol around them: what to read, when to treat a no-op as "unchanged", how a failed splice maps to `unsafe_write`, when to refresh the snapshot, how to build the result. Each command re-decides these, and `setClaim` already differs from `setStatus` in subtle ways (it applies first "so a file otman could not rewrite is refused with `unsafe_write`", then reads `before`). `vault.WriteItemFile` also records the snapshot as a side effect (`vault/snapshot.go:74`), so the rule "every write keeps the snapshot honest" is spread over callers' discipline. The deletion test says this is concentrating: delete `setClaim`'s plumbing and the same plumbing reappears in seven callers.

**Proposed deeper design.** One module behind a small interface, for example `Mutate(ref, change) (Outcome, error)` where `change` is the pure `item` transform (`SetStatus`, `Apply`, `AppendComment`, relation edits) plus a precondition callback over the parsed "before" state (claim conflict, not-open). `Mutate` owns resolve, read, apply, error classification, write, snapshot, re-summarise and the changed/unchanged decision, and returns an `Outcome{Summary, Changed, Before, Warnings}`. Commands shrink to flag parsing, the precondition and the human sentence. It sits in or beside `vault`, so the CLI keeps only presentation. The error classification (`writeError`) becomes a typed error from the module that `cli` renders, not code `cli` must know to call.

**Benefit.** Locality: the unsafe-write contract of ADR 0005 and 0006 is enforced in one place. Leverage: a new command (assign, set-kind) is a transform and a sentence. Tests: the mutation interface can be tested against a temp Vault with no cobra, no golden transcript, and the golden suite keeps covering rendering only.

---

## 2. Move domain logic out of `cli` into an Item-graph module (Strong)

**Evidence.** `internal/cli` holds logic that is not presentation:

- Relation rules: `relations.go` (314 lines): `checkParentCycle`, `checkBlockingCycle`, `resolveLinks`, `matchingLinks`.
- Link resolution: `itemLinks` in `item_json.go:23`, an adapter over `wikilink.Index` plus a `byPath` map, rebuilt by `newItemLinks(files)` in `list_cmd.go`, `doctor_cmd.go` and `relations.go`.
- Frontier rules: `openByPath.isOpen` in `frontier_cmd.go:38` and `clearOfBlockers` / `related` in `list_cmd.go:290-328`, written as methods on the list query, so Frontier (a glossary term) is a flag combination rather than a concept.
- Duplicates and Renumber: `duplicates.go` (`planRenumbers`, `duplicateGroups`).
- Label vocabulary: `labels.go` (`labelsInUse`, `closest`, `editDistance`).

**Problem.** The domain is split across `item` (one Item's bytes), `vault` (files and db) and `cli` (everything about a Project's Items together). Understanding "which Items does this Item block, transitively, and are they open" means reading `item_json.go`, `relations.go`, `list_cmd.go` and `frontier_cmd.go`. Because it lives in `cli`, these rules can be exercised only by running a whole command and diffing its transcript, so the module that has the most branching logic has no tests at its own interface. `listProject` (`list_cmd.go:360`) also threads three accumulators (`inUse`, `drift`, `warnings`) through one function to get several unrelated outputs from a single pass.

**Proposed deeper design.** A `Project`-scoped Item graph module, loaded once from `vault.ItemFiles(key)` and the file bytes: `Load(v, key) → Graph`, with `Graph.Resolve(link)`, `Graph.Parent(i)`, `Graph.Blockers(i)`, `Graph.WouldCycle(...)`, `Graph.Frontier(scope)`, `Graph.Duplicates()` and `Graph.Labels()`. It owns `itemLinks`, the read-once caching of Item bytes (today `openByPath` re-reads and re-parses blockers on demand), and the Frontier definition. Commands ask it questions and render the answer. The seam sits between the Vault (files) and the CLI (rendering).

**Benefit.** Locality for Frontier and relation rules (the files most recently changed in `cli`: `list_cmd.go`, `relations.go`, `frontier_cmd.go`), one place where a Project's links are resolved instead of three rebuilds, and tests of cycle detection and Frontier against in-memory Items. Compare with candidate 1: this deepens the read side, candidate 1 the write side; they share the same loaded Graph.

---

## 3. One Drift module instead of three views of Drift (Worth exploring)

**Evidence.** Drift (a glossary term) is described three times:

- `item.Parse` collects `BadValue` and `BadRelation` (`item/item.go:374-392`).
- `cli/drift.go` turns them into read-time warnings: `driftProblems`, `misplacedItem`, `markerProblem`, `invalidStatus`, `invalidLabels`, `idMismatch`.
- `cli/doctor_cmd.go` (771 lines) re-detects the same conditions in `planDoctor` (`doctor_cmd.go:371`, about 175 lines), this time paired with the repair. Codes such as `missing_comments_marker` appear in both `drift.go` and `doctor_cmd.go`.
- `item/repair.go` and `healEdits` / `healLabels` in `item` hold the heal that writes perform.

**Problem.** A new kind of Drift needs a coordinated change in four places: parse, the read warning, the doctor finding and the doctor repair, plus the "does a write heal it" rule from ADR 0005. The test `drift_test.go` covers read warnings, `doctor_test.go` covers findings, and nothing asserts that the two agree. `planDoctor` takes seven parameters (file, bytes, Project files, snapshot, `prefer`, `vacating` map) and returns a plan with six independent flags, which signals a missing abstraction.

**Proposed deeper design.** A single `Inspect(file, parsed, context) []Finding` where a Finding carries its code, message, whether and how it repairs (`none` / `auto` / `choice`) and the edit that repairs it. `driftProblems` becomes "render Findings as warnings"; `doctor` becomes "render Findings and, with `--fix`, apply their edits". Write-time healing (`healEdits`) consumes the same Findings filtered to the lossless ones. The snapshot comparison (`sideOf`) stays internal to the module.

**Benefit.** Locality: adding or changing a Drift is one change. Tests: one table of "file bytes → findings, repairs", replacing golden transcripts as the primary check; ADR 0005's promise that reads, writes and doctor agree becomes testable. Risk: `planDoctor` includes file-move and link-rewrite orchestration, so only the detection and the edit plan should move; the journaled rename stays in `vault`.

---

## 4. Split `item/item.go` along its real concerns (Worth exploring)

**Evidence.** `internal/item/item.go` is 1,091 lines with about 65 functions and ~60 exported names, mixing: Kind and folder mapping, filename and ID parsing (`Filename`, `ParseID`, `FilenameTitle`), frontmatter rendering (`Render` and the `yaml.Node` helpers), parsing (`Parse`, `readFrontmatter`), the comments section (`locateComments`, `commentsSection`, `appendComment`, `restoreMarker`), mutation (`Apply`, `rewrite`, `SetStatus`), and cross-note link retargeting (`RetargetLinks`).

**Problem.** The package interface is the union of all of these, so a reader changing comment handling still sees everything. Several concerns are small deep modules waiting to be separated: the comments section (ADR 0004: marker, heading, parse, append, restore) has a clear internal invariant and interface of about four functions; the filename projection (ADR 0002) likewise. `Same[T]` and `TruncateText` are generic helpers parked here.

**Proposed deeper design.** Two internal packages, `item/comments` (Section: Split, Append, Restore, Parse) and `item/name` (Filename, ParseFilename, ParseID, FilenameTitle, Kind folders), with `item` keeping Parse, Apply and Render. No behaviour change.

**Benefit.** Locality for the two ADR-backed behaviours and direct unit tests where `item` has none. Lower than 1 to 3 because it reorganises rather than removes cross-package coupling.

---

## 5. The Vault's scan cache and `Scanned()` side channel (Worth exploring)

**Evidence.** `Vault` carries three kinds of state: the lock/db handles, a scan cache (`v.items`, "kept until the Vault changes the files under Projects/") and `scanned map[string]bool`, which records which Projects `ItemFiles` was called for. `Scanned()` has exactly one caller, `duplicateWarnings` (`cli/duplicates.go:41`), which warns about duplicates in "the Projects this command looked at".

**Problem.** The duplicate-number warning depends on call history: what a command happened to read decides what it warns about. That is a hidden temporal coupling across a seam, and the cache invalidation is by convention (each mutating Vault method must remember to reset `v.items`). `Vault` also has a test-only `Fault` field and a `Fault` option threaded from `cli.Options`.

**Proposed deeper design.** Make scanning an explicit, immutable snapshot, `v.Scan() Tree`, with queries on the Tree (`Items(key)`, `Strays(key)`, `Duplicates(key)`), invalidated by returning a new Tree from mutating operations. Duplicates then derive from the Tree's contents, and `Scanned()` goes away. Fault injection becomes an adapter on the journal's step runner rather than a field of every Vault. This can be folded into candidate 2.

**Benefit.** Removes ordering-dependent output and the manual cache reset; makes the Vault's interface read as "scan, then ask".

---

## 6. A typed error/problem vocabulary (Speculative)

**Evidence.** Error construction is scattered: `&Error{Exit: ExitConflict, Code: "claim_conflict", Message: ..., Details: map[string]any{...}, Hint: ...}` appears inline in `claim_cmd.go:86`, `status_cmd.go:133` and elsewhere; `output.Warning(code, msg, details, hint)` takes stringly-typed details; hints name other commands as raw strings (`"run 'otman reopen " + f.ID() + "' first"`).

**Problem.** Codes, exit classes, detail keys and hint wording are an interface to agents (the CLI is built for agents) and are only checked by golden files. A typo in a code or a missing detail key surfaces as a golden diff, not a compile error.

**Proposed deeper design.** Constructors per error code in one module (`ErrClaimConflict(f, assignee, actor)`), each owning code, exit, details and hint, possibly generated into the tracker-template docs. Lower priority because the golden suite already pins the output and the cost is wide.

**Benefit.** Locality for the error contract; a single list of codes to document.

---

## Top recommendation

Start with candidate 1, the Item-mutation module. It is the smallest change with the most repeated code removed (seven commands), it finally gives `item` + `vault` an interface that is tested directly, and it is the natural first step toward candidate 2, because both need a loaded, resolved view of a Project's Items. Take candidate 2 next; candidate 3 is best done once the Graph and Mutation interfaces exist, since the Drift module needs both a Project's files (context) and the write path (repairs).

## Method and limits

- Explored by reading the directory structure, `GLOSSARY.md`, ADRs 0001 to 0006, the git history, and the code of `item`, `vault`, and the `cli` files named above. `doctor_cmd.go`, `edit_cmd.go`, `label_cmd.go`, `relation_cmd.go`, `create_cmd.go` and `journal.go` were skimmed by signature rather than read in full; their line counts and the shared helpers are verified, their step-by-step similarity to `setStatus` is inferred for edit, label and relation.
- The skill's HTML report step was skipped: this is a headless planning worker whose deliverable is a notes file, so there is no browser to open. The interview step (grilling) on a chosen candidate goes to the user as a decision request.
- Line counts are approximate and exclude tests.
