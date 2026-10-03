# Prior art: Obsidian conventions and markdown-tracker CLIs

Research for [#2](https://github.com/talvor/otman/issues/2). It feeds the vault-layout prototype (#4) and the CLI-surface prototype (#6).
Researched 2026-10-03. Each claim cites its primary source. Lines marked **Inference** are my conclusions, not something a source states.

## TL;DR: recommendations

**File format (vault layout)**

1. **One Item = one note, named by its ID** (`OTM-12.md`), inside a folder per Project. The title goes in a `title` property (plus an `aliases` entry so Obsidian's link autocomplete finds it by title). A filename that never changes means otman never has to rename a file, so it never has to rewrite links. Renaming outside Obsidian does not update links (see §2.3).
2. **Relationships are wikilinks in frontmatter, always quoted:** `parent: "[[OTM-3]]"` (Text) and `blocked_by: ["[[OTM-7]]", "[[OTM-9]]"]` (List). Obsidian then treats them as real links: they show as backlinks and in the graph, they are rewritten when a file is renamed in Obsidian, Bases sees them as Link objects, and Dataview sees them as links. Store only the forward edge. Derive `children` and `blocks` at query time (`parent == this`, `blocked_by.contains(this)`).
3. **Keep frontmatter flat and to the built-in property types:** Text, List, Number, Checkbox, Date, Date & time, Tags. Obsidian does not support nested properties. Don't use dots in property names, because a dot breaks link updating on rename. Avoid Markdown in values.
4. **`status` is a plain Text property with a small fixed vocabulary.** Bases' core **Kanban view** groups by a property and rewrites it when a card is dragged, so that view is the human board. The Kanban *community plugin* is a poor fit because it stores a whole board in one file.
5. **Ship `.base` files with the vault** (e.g. per-Project `Board.base`, plus a sidebar "Relations" base using `this`). That's how people get views in Obsidian without needing Dataview. Dataview is optional: it reads the same frontmatter, but it is in maintenance mode (last release April 2025).
6. **Don't use Tasks-plugin checkbox lines as Items.** Tasks works per line, and its `🆔`/`⛔` dependency IDs live in a different namespace. The checkbox vocabulary (`[ ] [/] [x] [-]`, i.e. TODO / IN_PROGRESS / DONE / CANCELLED) is still a good model for the `status` values.

**CLI surface**

7. **Model the shape on Backlog.md, beads and tasks-axi:** `otman` with no arguments shows a dashboard, then `add/new`, `list`, `show`, `update`, `close/reopen`, `block --by`/`unblock`, `ready` (unblocked open work), and `--json` on every command. Follow the AXI principles: TOON or compact default output, 3–4 fields in a list, truncated bodies with `--full`, idempotent mutations that report `already`, structured errors, no prompts, next-step hints, and an explicit empty state.
8. **IDs:** use a Project prefix plus a sequential integer (`OTM-12`, like Backlog.md's `BACK-549`) and resolve them case-insensitively. Backlog.md and beads both give children dotted IDs (`BACK-4.3`, `bd-a3f8.1`). otman's GLOSSARY says every Item takes the Project's single sequence whatever its Kind, so **don't** copy dotted IDs. Keep `parent` as a property only. Note that beads dropped sequential IDs for hash IDs because concurrent creators collided. That is the same risk ADR-0001 and #5 are about.
9. **Edit the files directly. Don't drive the Obsidian CLI.** The official Obsidian CLI (`obsidian property:set`, `base:query`, `rename`) needs the desktop app running. That is fine as an optional accelerator (e.g. `base:query format=json`), but it can't be otman's foundation.

## 1. Obsidian Properties (frontmatter)

- Properties are YAML frontmatter. The types are **Text, List, Number, Checkbox, Date, Date & time, Tags**. "Once a property type is assigned to a property name, all properties with that name across your vault will use the same type." ([Properties](https://help.obsidian.md/properties), source: `obsidianmd/obsidian-help` `en/Editing and formatting/Properties.md`)
- Formats: Date `2020-08-21`; Date & time `2020-08-21T10:30:00`; Number must be a literal; Checkbox is `true/false`. (same)
- **Links in properties:** "[[Internal links]] in text properties must be surrounded with quotes." In list properties: `- "[[Link]]"`. (same)
- `tags` is the only property that can have the Tags type. The other default properties are `aliases` and `cssclasses`. `tag`/`alias`/`cssclass` stop being supported in 1.9. (same)
- **Not supported:** nested properties, bulk-editing, and Markdown in properties (an intentional limitation). (same)
- JSON frontmatter is accepted but is "read, interpreted, and saved as YAML". (same) **Inference:** otman should write YAML so it round-trips cleanly when a human edits in Obsidian.
- Obsidian stores the type assignments in `.obsidian/types.json`. This is observed app behaviour, not covered in the help docs. **Inference:** otman could seed it at vault init so `blocked_by` is typed List and `created` is typed Date, without anyone clicking through the UI.
- Plugin API: `CachedMetadata.frontmatterLinks: FrontmatterLinkCache[]` (since 1.4.0, with a `key` per link) and `FileManager.processFrontMatter()`. ([obsidian.d.ts](https://github.com/obsidianmd/obsidian-api/blob/master/obsidian.d.ts)) This confirms Obsidian indexes frontmatter links per property key.

## 2. Wikilinks as relationship properties (`parent`, `blocked_by`)

### 2.1 Idiomatic encoding

```yaml
---
id: OTM-12
title: Parse vault config
kind: issue
status: open
parent: "[[OTM-3]]"
blocked_by:
  - "[[OTM-7]]"
  - "[[OTM-9]]"
labels: [cli]
created: 2026-10-03
---
```

The quoted wikilink form is what the Properties docs prescribe (§1). Dataview's docs agree: "you need to quote it, as so: `key: "[[Link]]"` … Unquoted links lead to a invalid YAML frontmatter." ([Dataview: types of metadata](https://blacksmithgu.github.io/obsidian-dataview/annotation/types-of-metadata/#link))

### 2.2 Resolution

- Wikilinks resolve by file name. A folder path is needed only to disambiguate (`[[Projects/Three laws of motion]]`). The "New link format" setting picks "Shortest path when possible", "Relative path" or "Absolute path in vault". ([Internal links](https://help.obsidian.md/links), [Settings](https://help.obsidian.md/settings))
- `# | ^ : %% [[ ]]` "may not work as a link", so keep them out of filenames. (Internal links)
- **Inference:** if filenames are IDs whose Project prefix is unique across the vault, `[[OTM-12]]` is always unambiguous at the shortest path. otman can resolve it as `<project-folder>/OTM-12.md` without asking Obsidian. A title-bearing filename (Backlog.md's style, `back-549 - Title.md`) would make every title edit a rename.

### 2.3 Rename behaviour

- "Obsidian can automatically update internal links in your vault when you rename a file" (setting: *Automatically update internal links*). ([Internal links](https://help.obsidian.md/links))
- Links in properties are included. A bug where property links weren't updated was acknowledged by staff ("Will be fixed 1.5.7"). ([forum](https://forum.obsidian.md/t/links-in-properties-are-not-renamed-automatically-after-renaming-a-linked-note/77249)) 1.11 added Markdown-style links in Text/List properties: "internal links are automatically updated when the destination file is moved or renamed". ([1.11.4 changelog](https://obsidian.md/changelog/2026-01-12-desktop-v1.11.4/))
- **Known gap:** link updating fails for properties with **dots in the name** (e.g. `Person.name`). It was reported and still unfixed in 1.12.5. ([forum](https://forum.obsidian.md/t/property-names-with-dots-break-link-updating-on-note-rename/102860)) Use `blocked_by`, not `blocked.by`.
- The Dataview docs warn that a frontmatter link "won't show up in the outgoing links … and won't be updated on i.e. a rename". **That note predates Properties (1.4) and is stale** for quoted wikilinks. Bases' docs say `file.links` includes frontmatter links. (§3)
- **Inference:** link rewriting on rename is done by the Obsidian app. Renames made by otman or by git on disk look to Obsidian like a delete plus a create, and they leave dangling links. That's one more reason for immutable ID filenames.

### 2.4 Querying relations

- **Bases:** wikilinks in frontmatter "are automatically recognized as Link objects"; "Links can be compared to files such as `file` or `this` … `author == this`"; "`authors.contains(this)`". In a sidebar base, `this` is the active note. ([Bases syntax](https://help.obsidian.md/bases/syntax)) So:
  - children of the open Item: `parent == this`
  - Items this one blocks: `blocked_by.contains(this)`
  - open blockers (**untested**): `blocked_by.filter(value.asFile().properties.status != "done")`, using `list.filter`, `link.asFile()` and `file.properties` ([Functions](https://help.obsidian.md/bases/functions)). Caution: `file.properties` and `file.backlinks` "do not automatically refresh". Prefer forward lookups.
- **Dataview:** all frontmatter becomes fields. Sources: `FROM [[note]]` (pages linking to it), `FROM outgoing([[note]])`, `[[]]` = the current file. `file.inlinks`/`file.outlinks` are available. ([sources](https://blacksmithgu.github.io/obsidian-dataview/reference/sources/), [page metadata](https://blacksmithgu.github.io/obsidian-dataview/annotation/metadata-pages/)) Example: `TABLE status FROM "Projects/OTM" WHERE contains(blocked_by, this.file.link)`.

## 3. Obsidian Bases (`.base`)

- A `.base` file is YAML with `filters` (recursive `and`/`or`/`not` of expression strings), `formulas`, `properties` (display config), `summaries`, and `views` (each with `type`, `name`, `limit`, `groupBy`, `filters`, `order`, `summaries`). It can also be embedded in a code block. "By default a base includes every file in the vault. There is no `from` … like in SQL or Dataview." ([Bases syntax](https://help.obsidian.md/bases/syntax))
- Property namespaces are `note.x` (or bare `x`), `file.*` (`name, path, folder, ext, ctime, mtime, links, backlinks, tags, properties`), and `formula.x`. Use `file.inFolder("Projects/OTM")` to scope to a Project. (same)
- Layouts: Table, Cards, List, Map, and **Kanban**. Kanban "requires a property to group results by". "Drag a card to another column to update the grouped property in that note", and "+" in a column creates a note with that value. **It needs Obsidian 1.14, which is in early access** at the time of writing. Grouping by a formula or file property disables drag. ([Kanban view](https://help.obsidian.md/bases/views/kanban))
- **Inference:** `status` must be a real note property, not a formula, so the board is editable. A human dragging a card writes `status` straight into frontmatter. That matches ADR-0001's decision that status is canonical in markdown.

Sketch of a shipped `Projects/OTM/Board.base`:

```yaml
filters:
  and:
    - file.inFolder("Projects/OTM")
    - file.ext == "md"
views:
  - type: kanban        # 1.14+; fall back to table
    name: Board
    groupBy: { property: status, direction: ASC }
    order: [title, kind, file.name]
  - type: table
    name: Open
    filters: { and: ['status != "done"', 'status != "cancelled"'] }
    order: [file.name, title, kind, status, parent, blocked_by]
```

## 4. Dataview

- Treats the vault as a database. It has DQL (`TABLE/LIST/TASK … FROM … WHERE`), inline `key:: value` fields and a JS API. All YAML frontmatter fields are automatically available. ([README](https://github.com/blacksmithgu/obsidian-dataview), [add metadata](https://blacksmithgu.github.io/obsidian-dataview/annotation/add-metadata/))
- Last release is 0.5.70 (2025-04-07, beta); last push 2025-11 (GitHub API). **Inference:** Bases is the core replacement for most table use. otman should target Bases first and stay Dataview-compatible for free by using plain frontmatter, not inline `::` fields.

## 5. Tasks plugin

- Works per **checkbox line**, not per note. Statuses have types `TODO, IN_PROGRESS, ON_HOLD, DONE, CANCELLED, NON_TASK`; `done` matches DONE/CANCELLED/NON_TASK. ([Statuses](https://publish.obsidian.md/tasks/Getting+Started/Statuses))
- Dependencies (6.1.0+): `🆔 abcdef` on the blocker and `⛔ abcdef` on the dependent, or Dataview format `[id:: …]` / `[dependsOn:: …]`. IDs are `[A-Za-z0-9_-]+`. Finish-to-start only. ([Task Dependencies](https://publish.obsidian.md/tasks/Getting+Started/Task+Dependencies))
- 7.7.0+ can read the file's Obsidian Properties: `task.file.property('name')`, and `hasProperty`. Names are case-insensitive. ([Obsidian Properties](https://publish.obsidian.md/tasks/Getting+Started/Obsidian+Properties)) Latest release is 8.4.0 (2026-08-25).
- **Inference:** these don't fit as Item storage. They are useful as a vocabulary for `status` and for "blocked_by points at the blocker", and an Item body may contain Tasks checklists (acceptance criteria) that otman leaves alone.

## 6. Kanban community plugin

- A board is **one markdown file** with frontmatter `kanban-plugin: board`. Lanes are headings, cards are list items, and settings go in a trailing `%% kanban:settings` block. (source: [`mgmeyers/obsidian-kanban` `src/parsers/common.ts`](https://github.com/mgmeyers/obsidian-kanban/blob/main/src/parsers/common.ts))
- Last release is 2.0.51 (2024-05-31).
- **Inference:** this conflicts with one-file-per-Item. Use the Bases Kanban view (§3) instead. If otman ever emits a plugin board, it should be a generated, read-only artifact.

## 7. Obsidian CLI (official)

- `obsidian` needs the 1.12 installer, and "Obsidian app must be running". Commands include `create`, `read`, `rename` (updates links if that setting is on), `move`, `property:set name= value= type=text|list|number|checkbox|date|datetime`, `property:read`, `properties format=json`, `backlinks format=json`, `unresolved`, `bases`, `base:query view= format=json|csv|tsv|md|paths`, `tasks`, and `search`. `file=<name>` resolves "using the same link resolution as wikilinks"; `path=` is exact. ([Obsidian CLI](https://help.obsidian.md/cli))
- **Inference:** it's useful prior art for flag style (`format=json|tsv`, `file=` vs `path=` targeting), and it could be an optional backend for rename-with-link-update or `base:query`. It can't be required, because agents run headless.

## 8. Markdown and graph issue-tracker CLIs

| Tool | Storage | IDs | Relations | Output modes |
|---|---|---|---|---|
| **Backlog.md** ([repo](https://github.com/MrLesk/Backlog.md)) | One `.md` per task in `backlog/tasks/`, filename `back-549 - Title-slug.md`; completed/archive folders | Configurable prefix + sequential int (`BACK-549`); optional `zeroPaddedIds`; subtasks `BACK-4.3` | Frontmatter `dependencies: [task-4.2]`, `parent_task_id: task-4` (bare IDs, not wikilinks) | TUI board, web UI, MCP, `--json` on `task list/view/search` ("stable, versioned JSON"), `--watch` |
| **beads** `bd` ([repo](https://github.com/gastownhall/beads)) | Dolt SQL DB in `.beads/`; `issues.jsonl` export | Hash IDs `bd-a1b2`, hierarchical `bd-a3f8.1`; [moved off sequential IDs](https://github.com/gastownhall/beads/blob/main/docs/core-concepts/hash-ids.md) because "multiple agents create issues simultaneously" | `bd dep add` (blocks, related, parent-child); `bd ready` = no open blockers; `update --claim` atomic | `--json` |
| **git-bug** ([repo](https://github.com/git-bug/git-bug)) | Git objects, "no files are added in your project" | Hash IDs, prefix-matched | Labels/comments; bridges to GitHub/GitLab/Jira | `--format default,plain,id,json,org-mode`; TUI, web UI, GraphQL |
| **tasks-axi** 0.2.6 (local, `tasks-axi --help`, README) | One hand-editable `backlog.md`; sections `## In flight/Queued/Done` carry state; byte-exact round-trip; advisory lockfile + atomic temp-rename | Caller-supplied slug or `--mint` | Inline `blocked-by: <id>`, `parent:`, `discovered-from:`; `block --by` requires the blocker to exist; `rm` refuses if it still blocks | TOON by default, `--json` on mutations, `ok:` line + next-step `help[]` hints, `already: true` on no-ops |
| **gh-axi** 0.1.35 (local) | GitHub API | GitHub numbers | `issue subissue add/list` | TOON (`issues[7]{number,title,state,author,created}:`), `--fields`, truncated bodies + `--full`, `help[]` hints |

The common CLI vocabulary across these tools is `add/create`, `list` (filters: state, label, assignee; `--fields`, `--limit`), `show/view` (`--full`), `update/edit`, `close/done`, `reopen`, `block --by`/`unblock` or `dep add`, `ready`, and `--json`.

AXI's ten principles ([kunchenguid/axi](https://github.com/kunchenguid/axi)): token-efficient TOON output; minimal default schemas (3–4 fields); content truncation with `--full`; pre-computed aggregates; definitive empty states; structured errors and exit codes with idempotent mutations, no prompts, and failure on unknown flags; ambient context (session hooks); content first (no-args shows live data); contextual next-step suggestions; and consistent per-subcommand help.

**Takeaways for otman**

- Backlog.md is the closest analogue: a folder of frontmatter files plus sequential prefixed IDs. otman differs in using wikilinks (so Obsidian sees the graph), not putting titles in filenames, and allocating numbers from SQLite (ADR-0001) instead of scanning files.
- tasks-axi shows how to make concurrent hand-edits safe: a lockfile, atomic rename, re-reading on every invocation, and rewriting only the touched record. otman should do the same per Item file, and preserve unknown frontmatter keys and the body byte-for-byte.
- beads and git-bug both chose hash IDs to avoid coordination. otman's sequential numbers depend on #3/#5 solving cross-device allocation. Keeping a hash-ID or device-suffix fallback in mind is cheap.

## Sources

- Obsidian Help (git: `obsidianmd/obsidian-help`, read 2026-10-03): Properties, Internal links, Settings, Bases syntax, Functions, Kanban view, Obsidian CLI
- Obsidian changelog 1.11.4; Obsidian forum threads 77249 and 102860
- `obsidianmd/obsidian-api` `obsidian.d.ts`
- `blacksmithgu/obsidian-dataview` docs; `obsidian-tasks-group/obsidian-tasks` docs; `mgmeyers/obsidian-kanban` source
- `MrLesk/Backlog.md` README and `backlog/` task files; `gastownhall/beads` README and docs/core-concepts/hash-ids.md; `git-bug/git-bug` README and `doc/md/git-bug_bug.md`
- `tasks-axi` 0.2.6 and `gh-axi` 0.1.35 (`--help` output, installed README); `kunchenguid/axi` README
