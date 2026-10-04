# Hand edits: lenient reads, safe writes, explicit repair

Obsidian, git and file managers can all change Item files behind otman's back, so otman treats Drift as normal rather than as corruption. **Reads are lenient:** they accept drift and warn. **Writes are safe:** a write never discards content it doesn't understand. It refuses (exit 4, `unsafe_write`) when it would have to guess at hand-edited content, and it heals only drift that is lossless and only in the Item it touches. **Repair is explicit:** `doctor` reports every finding without changing anything, `doctor --fix` applies only deterministic fixes, and any fix that needs a choice needs a flag (`--prefer frontmatter|file`). An Item's identity is its filename prefix `<KEY>-n`, the thing links, discovery and Obsidian's own renames already agree on. Frontmatter `id` is a copy that follows it.

## Considered Options

- **Strict:** any drift makes commands on that Item fail until it is repaired. Rejected because it would make Obsidian a second-class editor and stall agents on harmless edits.
- **Self-healing:** every command silently repairs whatever it can. Rejected because a command could rewrite what a person just typed, and the guesses (status, the body/comments boundary, which side of a rename was intended) can be wrong.
- **Frontmatter `id` as identity,** with filenames repaired to match. Rejected because links and Obsidian follow the file, so the filename would win in practice anyway and leave two sources of truth.

## Consequences

- otman preserves YAML it doesn't own: unknown keys and their values, key order and YAML comments. It rewrites only the keys it changes, so diffs stay minimal.
- The per-device db keeps a snapshot of the filename, title, Kind and folder otman last wrote for each Item. That lets `doctor --fix` tell which side of a title/filename or Kind/folder disagreement changed. Without a snapshot (a rebuilt db) or when both sides changed, the user must choose with `--prefer`.
- Multi-file rewrites (retitle, relation rewrites) go through a roll-forward journal in `.otman/journal/`. Every command first finishes any pending journal under the lock and warns `resumed_operation`, so an interrupted rename completes rather than half-applying.
- Some drift is report-only by design: unparseable frontmatter, duplicate comments markers, ambiguous links, and a missing marker whose trailing text doesn't parse as comments. `doctor --fix` leaves that last case alone, but `comment` and `close`/`reopen --comment` restore the marker before the last `## Comments` heading.
