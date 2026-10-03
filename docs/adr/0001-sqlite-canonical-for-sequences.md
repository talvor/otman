---
status: superseded by ADR-0003
---

# SQLite in the vault is canonical for sequences and the project registry

otman keeps a SQLite database at `.otman/otman.db` inside the Vault. It is the source of truth for each Project's numbering sequence and for the project registry, so Item numbers can be allocated under SQLite's transactional locking. Everything else (status, labels, assignee, parent/child, blocking, comments) stays canonical in the markdown files and their frontmatter, so a human can still edit an Item in Obsidian and Bases/Dataview views stay accurate.

## Considered Options

- **Derived index only:** files canonical for everything, SQLite rebuildable. This was rejected because numbering would still need a separate lock or a scan, and the registry would have no single home.
- **SQLite canonical for all metadata:** this was rejected because frontmatter edits made in Obsidian would be ignored and views would show stale data.
- **Database stored per machine, outside the vault:** this was rejected in favour of syncing it with the vault, so every device sees the same sequences and registry.

## Consequences

- Projects can't be created by hand in Obsidian; they're created with `otman` commands.
- A live SQLite file synced through Obsidian Sync, iCloud or git can be corrupted, or lose writes when two devices change it concurrently. Cross-device number safety is still an open question on the wayfinder map.
