# SQLite is a per-device high-water mark reconciled from markdown

Supersedes [ADR 0001](0001-sqlite-canonical-for-sequences.md). ADR 0001 assumed `.otman/otman.db` would sync with the Vault so every device saw the same sequences and registry. Research showed that premise is false. Obsidian Sync never syncs dot-folders, and no sync tool offers a cross-device lock, so a synced db loses updates and can be corrupted. The db is now **per device**. On every run otman reconciles it against the markdown: a Project's next number is `max(db sequence, highest Item number in its files) + 1`, and the db is raised to match. The db never goes down, so it is a high-water mark and numbers of deleted Items are never reused. Projects are adopted from `Projects/<KEY>/` folders, and their metadata lives in a Project note (`Projects/<KEY>/<KEY>.md`) that syncs. v1 assumes a single device. Duplicate numbers are detected rather than prevented, and `otman doctor --fix` repairs them.

## Considered Options

- **Markdown canonical for everything, no db role for sequences.** This was rejected because the db is kept as the high-water mark that remembers deleted numbers, which the files can't do.
- **Keep ADR 0001 and move the db out of the dot-folder so it syncs.** This was rejected because it still loses updates under concurrency and still risks corruption.
- **Avoid collisions by construction (per-device ranges or suffixes).** This was rejected because it makes IDs ugly and needs per-device config, for a case v1 doesn't target.

## Consequences

- The db can be thrown away. If it is missing or fails `quick_check`, otman rebuilds it from the markdown, losing only the memory of deleted numbers above the highest surviving file.
- `.otman/` stays in the Vault. otman writes `.otman/.gitignore` containing `*`, and the docs tell users to exclude it in Syncthing and iCloud.
- Writers on one device are serialised with an OS advisory lock (`flock`) on `.otman/lock`, held across the scan, the allocation and the write.
- DB settings: `journal_mode=DELETE` (no WAL), `synchronous=FULL`, short `BEGIN IMMEDIATE` transactions, and one connection opened and closed per command. The driver is `modernc.org/sqlite`, which is cgo-free and keeps the static binary.
- Duplicates (from a second device, a copied file or a git merge) produce a warning on every command that touches the Project. A bare ambiguous ID is an error, while a full filename still resolves. `otman doctor --fix` keeps the number on the Item with the earlier `created` timestamp (ties go by filename order) and renumbers the other through the normal allocator, renaming its file and rewriting its links. It then appends a "Renumbered from …" comment and prints the old → new mapping.
- A duplicate whose title is also the same on both devices collapses into one filename in the sync tool and can't be detected. This is accepted under the single-device assumption.
