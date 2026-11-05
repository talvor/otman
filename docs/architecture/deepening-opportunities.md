# Architecture survey: deepening opportunities

Survey of otman at commit `423d106`, using the codebase-design vocabulary (module, interface, depth, seam, adapter, leverage, locality). Notes only; no code was changed. Domain terms follow `GLOSSARY.md`; existing decisions in `docs/adr/` (0001–0006) are treated as settled and not reopened.

## Shape today

| Package | Lines (non-test) | Role |
| --- | --- | --- |
| `internal/cli` | ~9.8k | cobra commands, flag handling, Drift detection, relation graph, doctor planning, rendering |
| `internal/item` | ~1.4k | Item file bytes: ids, filenames, Render, Parse, edits, comments, link retargeting |
| `internal/vault` | ~1.7k | Vault on disk: lock, sqlite db, scan, allocate, write, snapshots, rename journal |
| `internal/frontmatter`, `wikilink`, `fsutil`, `config`, `output` | | leaf helpers |
| `internal/docs` | 18 | tracker template text |

Already deep and worth keeping as-is: `frontmatter` (byte-splicing behind a small Edit interface, ADR-0006), `vault.RenameItem` (journaled multi-file rename and link rewrite behind one call), `vault.Open` (lock + bootstrap + journal resume), `fsutil.WriteFile`. The CLI is also tested through one real seam, `cli.Run(Options)`, where `Env`, `Now`, `Stdin/Stdout` and `LockTimeout` are injected (`internal/cli/cli.go:26`), backed by golden files. That seam is good and should be preserved.

The weakness is concentration: the domain rules that make otman otman (Drift, relations, Frontier, repair planning) sit in `cli` rather than behind an interface in a domain module, so they are reachable only by building cobra commands.

Ranking is by value (leverage × locality gained per unit of risk).

---

## 1. A single "change an Item" module (the read→mutate→write→summarize pipeline is copied per command)

**Evidence.** Every mutating command hand-assembles the same sequence against three modules:

- `status_cmd.go:72-108` (`setStatus`): `resolveRef` → `v.ReadItemFile` → `item.Parse` → `item.SetStatus(data, f.Derived(), …, a.opts.Now())` → `v.WriteItemFile` → `summarize` → human text → `a.emit`.
- `edit_cmd.go:133-200`: same, plus `if-rev` check, label warnings, and a branch choosing `v.RenameItem` vs `v.WriteItemFile`.
- `claim_cmd.go:66-97`, `comment_cmd.go:49-61`, `relation_cmd.go:90-124` (`changeRelations`), `label_cmd.go:88-104`, `doctor_cmd.go:575-624` (`applyDoctor`), `duplicates.go:161-184` (`renumber`) repeat it with variations.
- Error translation of the write step is repeated: `writeError` (`status_cmd.go:116`), `renameError` (`edit_cmd.go:209`), `ioError(err)` at every read/write.

**Problem.** The interface of the Vault for a caller is "five calls in the right order": read bytes, parse, compute `Derived` from the `ItemFile`, call the right `item.*` mutator with a clock, then choose write vs rename (a title or Kind change must rename; nothing enforces that other commands don't). Invariants (rename if the filename projection changes, record a snapshot, bump `updated`, heal only where nothing is lost) are enforced by convention in each command. `vault` and `item` are individually reasonable but the *seam between them is in the wrong place*: it is crossed ~10 times in `cli`.

**Deeper design.** Add one module at the vault/item seam, e.g. `vault.Item(ref)` returning a handle (or `v.Change(f, item.Update|StatusChange|CommentChange|RelationChange, now)`), whose interface is "apply this change to this Item and tell me what happened":

```
result, err := v.Apply(f, change)   // result: new ItemFile, bytes, Changed, Renamed
```

Behind it: read, `If-rev` check (as an option on the change), mutate, decide write vs `RenameItem`, record snapshot, return typed errors (see #4). `item.Update` already is a change value; widen it (or add a sum type) to cover status, comment, claim, relation edits, so `item.SetStatus/AppendComment/Apply` collapse into one `Apply` entry.

**Benefit.** Leverage: ~10 call sites shrink to “build change, apply, render”. Locality: the rename-on-retitle/Kind-move rule, the snapshot rule and the write-error classification live in one place; a bug fixed once is fixed for status, edit, claim, comment and doctor. Tests: new behaviour tests go at `Apply` using a temp Vault (local-substitutable dependency: the filesystem and sqlite already run in tests); per-command tests keep only flag parsing and rendering.

---

## 2. Move Drift detection and repair planning out of `cli` into a domain module

**Evidence.**
- `cli/drift.go` (275 lines): `driftProblems`, `itemProblems`, `idMismatch`, `markerProblem`, `invalidLabels`, `misplacedItem`: each takes `vault.ItemFile` + `item.Parsed` and returns `output.Problem` (a presentation type with message and hint text).
- `cli/doctor_cmd.go` (771 lines): `planDoctor` (`:371`, ~170 lines, 7 parameters), `applyDoctor`, `keepBaseline` (`:649`, which calls `v.SetSnapshot` directly), `sideOf`, `repointTarget`; `doctorPlan` flags `keepTitle/keepKind/settleTitle/settleKind` encode snapshot bookkeeping in the CLI.
- `cli/duplicates.go`: `duplicateGroups`, `planRenumbers` (reads and parses files itself, `:100-104`), `renumber`.
- `cli/relations.go:37-110`: `linkProblem`, `badRelation`, `relationProblems`.
- Layering inversion: `vault` imports `output` (`vault/vault.go`, `project.go`, `journal.go`, `db.go`) just to return `[]output.Problem`, so the domain depends on the presentation vocabulary.

**Problem.** Drift is a headline domain term (`GLOSSARY.md`) and ADR-0005 defines its policy (lenient read, safe write, doctor repairs the rest). The policy is spread across four CLI files interleaved with message wording, and can only be exercised through `otman doctor` golden tests (`doctor_test.go`, 30+ `doctor-*.golden`). `doctor` cannot be reused by anything but the cobra command, and planning is untestable without a Vault because `planDoctor` takes `*vault.Vault` just to answer `movable`.

**Deeper design.** A `drift` (or `vault.Audit`) module with a small interface:

```
findings := drift.Inspect(itemFiles, readFn)           // []Finding{Item, Kind, Detail, Repair: None|Auto|Choice}
plan     := drift.PlanRepair(findings, prefer)         // pure: bytes/paths in, intended edits out
```

Findings are typed (`IDMismatch{…}`, `KindFolder{…}`, `MissingMarker{…}`); `cli` maps findings to `output.Problem` text and doctor output. The snapshot baseline decision (`keepBaseline`, `sideOf`) moves inside with the plan, so `cli` no longer calls `SetSnapshot`. `vault` returns its own warning type and `cli` adapts it, removing the `vault → output` import.

**Benefit.** Locality: ADR-0005 is implemented in one package. Testability: `Inspect`/`PlanRepair` are pure in-process computation (dependency category 1), tested by table with byte fixtures instead of building a Vault and invoking the CLI; the golden tests then only guard wording. Deletion test: removing `drift.go`+`planDoctor` scatters complexity into every reader (`list`, `view`, `frontier` all call `driftProblems`), so the module earns its place once it has a clean interface.

---

## 3. A Project graph module (relations, cycles, Frontier) instead of `itemLinks` + helpers in `cli`

**Evidence.**
- `itemLinks` (`cli/item_json.go:26-67`) resolves wikilinks in one Project; its use is threaded as a parameter through `itemProblems`, `relationProblems`, `relatedTo`, `matchingLinks`, `followLinks`, `resolveLinks`, `checkParentCycle`, `checkBlockingCycle`, `repointTarget`, `repointable`, `itemQuery.related`, `itemQuery.clearOfBlockers`, `planParentSet`, `planBlock`… (see `grep itemLinks internal/cli`).
- Graph walking re-reads and re-parses files per call: `followLinks` (`relations.go:206-211`) does `v.ReadItemFile` + `item.Parse` per hop; `relatedTo` (`item_json.go:155-162`) does the same; `listProject` (`list_cmd.go:360-403`) parses every file, then `clearOfBlockers` consults `openByPath` which reads blockers again.
- Frontier (a glossary term) is implemented as a filter flag, `q.unblocked`, inside the generic list query (`list_cmd.go:311`), with `frontier_cmd.go` (55 lines) as a thin wrapper.
- `listProject` mixes five concerns in one loop: parse, malformed handling, label tally (`inUse.add`), query matching, drift collection into an out-param map (`drift[f.Path] = …`).

**Problem.** The interface to relations is a bag of free functions that each demand `(v, links, f, p, key)`; callers must know which combination to pass, that `links` is per-Project, and that the vault must be held open. Parsing is repeated with no shared cache, and `listProject`'s out-parameters (`inUse`, `drift`) make it hard to call or test in isolation.

**Deeper design.** `graph := vault.Graph(key)` (or `project.Load(v, key)`) parses each Item once and exposes: `Parent(f)`, `Children(f)`, `Blockers(f)`, `Open(f)`, `WouldCycle(kind, a, b)`, `Frontier(scope)`, `Resolve(ref)`. Parsed bytes are held by the graph (one read per Item per command), and `list`/`frontier`/`view` become queries over it returning a result struct (`items`, `warnings`, `labelsInUse`) instead of mutating out-params.

**Benefit.** Leverage: relation rules (ambiguous links fail rather than guess; unreadable blockers are never taken as closed) live once. Locality: cycle checks, repoint and frontier share the same link resolution. Performance: read each file once per run. Tests: graph tests with in-memory file sets (in-process once files are supplied by a small `Files` interface; the vault adapter and a map-backed adapter make that a real two-adapter seam).

---

## 4. One error-translation seam (domain errors → `cli.Error`)

**Evidence.** `&Error{…}` literals are scattered: `relations.go` (7), `project_cmd.go` (3), `errors.go` (3), `edit_cmd.go` (2), `view_cmd.go` (2), `claim_cmd.go` (2), `status_cmd.go`, `doctor_cmd.go`, `vault.go`. Exit codes, machine codes (`stale_item`, `move_target_exists`, `unsafe_write`, `lock_timeout`) and hint strings are decided at each site. `writeError` and `renameError` reproduce overlapping `errors.Is/As` ladders over `frontmatter.UnsafeError`, `item.ErrBlockersNotList`, `item.ErrNoMarker`, `item.ErrDuplicateMarkers`, `vault.TargetExistsError`, `vault.JournalConflictError`; `openVault` (`vault.go:23-46`) does the same for open errors.

**Problem.** The CLI's error contract (exit code, code, hint) is part of its public interface for agents (it is emitted as JSON), but it is defined in many places; adding a new domain failure means updating several ladders and risks inconsistent hints. Meanwhile domain packages return a mix of typed errors, sentinels and `fmt.Errorf` (e.g. `vault/item.go:236`), so there is no stable error vocabulary to map from.

**Deeper design.** Give `item`/`vault` a closed set of typed errors (a small `vault.Failure{Kind, Path, …}` or sentinels with `errors.As` targets) and a single `classify(err, ctx) *Error` function in `cli/errors.go`. Commands call `return classify(err, f)` and never build `Error` for domain failures. CLI-input errors (`invalid(...)`) stay local.

**Benefit.** Locality for the machine-readable contract; one table-driven test enumerates every failure → code/exit/hint. Shallow `writeError`/`renameError` disappear (they are pass-through ladders; the deletion test shows the ladder reappears nowhere else once centralised).

---

## 5. `item` package: split naming/IDs from the document editor, and hide `Derived`+clock behind a document value

**Evidence.** `item/item.go` is 1091 lines holding: Kind and folder mapping, ID/filename projection (`Filename`, `ParseFilename`, `FilenameTitle`), `Render`, `Parse` (`Parsed` with ~12 fields), comment section location and parsing, `SetStatus`, `AppendComment`, `Apply`, link retargeting (`RetargetLinks`, `retargetRelations`), and YAML node helpers (`str`, `optional`, `quoted`, …). Every mutator takes `(file []byte, d Derived, …, now time.Time)` (`SetStatus` `:541`, `AppendComment` `:555`, `Apply` `:601`, `Repair` `repair.go:46`, `Renumber` `repair.go:82`); callers derive `d` from `vault.ItemFile.Derived()` each time (`vault/item.go:50`).

**Problem.** Callers must know that `Derived` carries expected title/kind/id for healing, and pass the right clock; `[]byte` in/out means each caller re-parses (`before := item.Parse(data)` precedes every mutation in `status_cmd`, `edit_cmd`, `claim_cmd`). The package is cohesive in topic but its interface is wide and the file is hard to navigate (an AI-navigability cost).

**Deeper design.** (a) Split by responsibility within the package: `naming.go` (Kind, ID, Filename), `render.go`, `parse.go`, `comments.go`, `edit.go`, `links.go`. (b) Introduce `item.Document` created once from `(bytes, Derived)` exposing `Parsed()`, `Rev()`, and `Apply(change, now)` returning a new `Document` plus `Changed`. This is the handle that #1 consumes.

**Benefit.** Fewer parameters per call, one parse per Document, and obvious files per concern. Lower priority than #1–#4 because it is mostly interface tidying; do it as part of #1 rather than separately.

---

## 6. Vault internals: implicit caches, test hooks on the production type, and bookkeeping leaking to callers

**Evidence.**
- `v.items = nil` is set by hand in `CreateItem` (`vault/item.go:229`), `writeFile` (`:291`) and presumably in rename paths: a cache invalidated by convention. A missed reset returns a stale scan (`ItemFiles`, `Scanned`, `scanProject`).
- `Vault.Fault func(step int) error` (`vault/vault.go:64`) is a test fault injector on the production struct, wired through `Options.Fault` and `openVault` (`cli/vault.go:47`).
- Snapshot management is split: `writeFile` records snapshots, but `doctor` calls `Snapshot`/`SetSnapshot` directly (`doctor_cmd.go:652,676`), so the invariant "snapshot = what otman last wrote" is maintained from two layers.
- `Vault` is the single type for locking, sqlite, Project registry, scanning, allocation, journaling (see `vault/` file list), so everything is reachable from `*Vault`.

**Problem.** These are small, but each is a place where locality is lost: a new writer can forget the cache reset or the snapshot.

**Deeper design.** Route all item writes through one private method that owns cache reset + snapshot (it mostly exists as `writeFile`; make `RenameItem`, `CreateItem` and doctor's baseline go through it or a `SettleSnapshot(f, bytes)` method that hides the prev/cur logic of `keepBaseline`). Keep `Fault` but make it an internal journal option (`vault.OpenOptions`) rather than a public mutable field. Optionally split `Vault` into `Store` (files+snapshots+items) and `Registry` (projects+high-water) behind the same `Open`, only if #1 and #2 land and the type still grows.

**Benefit.** Locality for the two invariants; a safer extension surface. Low risk, small effort.

---

## 7. Test strategy follows the structure (replace, don't layer)

**Evidence.** `internal/cli` holds ~9.8k lines of code and almost all the behavioural tests (`*_test.go`, 70+ golden files under `testdata/golden`), run through `cli.Run`. `item`, `vault` have little direct test coverage in this tree (no `item_test.go`, `vault/*_test.go` listed; `frontmatter`, `wikilink`, `fsutil`, `config` do have unit tests).

**Recommendation.** As #1–#3 land, put new tests at the new interfaces (Vault `Apply`, `drift.Inspect/PlanRepair`, `graph` queries) and keep golden tests only for output wording and exit codes. Delete CLI tests that merely re-prove a domain rule once a test at the deep interface covers it. The `cli.Run` seam stays as the end-to-end layer.

---

## 8. Minor: `internal/docs` and `vault/template.go`

`docs.TrackerTemplate(key)` (`internal/docs/docs.go`, 18 lines) is a one-function module wrapping an embedded string, called from `tracker_template_cmd.go` only. By the deletion test it is a pass-through; folding it into the `cli` command (or into a `templates` package that also owns `vault/template.go`'s Kind templates) removes a package without moving complexity. Low value, no urgency; mentioned for completeness.

---

## Suggested order

1. #4 error vocabulary (unblocks the rest by giving domain modules typed errors; small).
2. #1 (+#5b) Item change module.
3. #2 Drift/repair module (also removes `vault → output`).
4. #3 Project graph.
5. #6, #7 as follow-through.

## Gaps in this analysis

- Based on reading and grep of the code; the build and test suite were not run, and no coverage measurement was made. The claim that `item` and `vault` have no direct unit tests rests on the file listing only.
- Read in full or near-full: `cli.go`, `vault.go` (cli), `status_cmd.go`, `edit_cmd.go`, `list_cmd.go` (query half), `vault/item.go` (write half). Skimmed by symbol list only: `view_cmd.go`, `project_cmd.go`, `claim_cmd.go`, `label_cmd.go`, `journal.go`, `snapshot.go`, `output/*`, `config/*`, `wikilink`. Claims about them (error-literal counts, repeated pipeline) come from grep, not line-by-line reading.
- No design-it-twice exploration of alternative interfaces was done; the interface sketches are first proposals.
- Line counts are `wc -l` of the tree at the base commit.
