# How sync tools treat a live SQLite file inside a vault

Research for [talvor/otman#3](https://github.com/talvor/otman/issues/3), which feeds the cross-device number-safety decision and revisits ADR 0001 (`.otman/otman.db` canonical for sequences and the project registry).

Researched 2026-10-03. Every claim cites the primary source that owns it. Where no primary source was found, the claim is marked **(unverified)** or **(secondary)**.

## TL;DR

1. **Obsidian Sync will not sync `.otman/` at all.** Files and folders whose names start with `.` are excluded; the only exception is `.obsidian`. That holds even with "Sync all other types" on. As designed, ADR 0001's database would never leave the device on Obsidian Sync, the sync tool Obsidian users are most likely to have.
2. **No file-sync tool can give cross-device mutual exclusion.** SQLite's locks are local OS locks (POSIX advisory locks, OFD locks, `LockFileEx`), and each device locks only its own copy. Two devices can each allocate "the next number" from the same starting state. Obsidian Sync then keeps one copy wholesale ("last modified wins" for non-Markdown files), git flags a binary conflict, and iCloud keeps a conflict copy. The other device's allocation is lost, so **duplicate Item numbers are possible by design**, whatever journal mode is used.
3. **Corruption risk comes from copying, not from locking.** SQLite says that copying a database while a transaction is in progress, or separating the database from its `-journal`/`-wal`, can corrupt it or lose committed transactions. A sync tool that uploads `otman.db` without its sidecar, or replaces the file under an open connection, is doing exactly that.
4. **The safest settings for a synced directory are rollback journal (`journal_mode=DELETE`, the default) plus `synchronous=FULL` (the default for rollback mode), with short transactions and the connection closed promptly.** Avoid WAL. It adds two sidecars (`-wal`, `-shm`), the WAL file holds committed data that a sync tool can separate from the main file, and WAL needs shared memory on one host. These settings reduce corruption risk. They do nothing for lost updates.
5. **Use a pure-Go driver.** `modernc.org/sqlite` (transpiled C, cgo-free) and `ncruces/go-sqlite3` (Wasm via wazero) both build static binaries with `CGO_ENABLED=0` and lock files compatibly with C SQLite. `mattn/go-sqlite3` needs cgo and gcc, and a static or cross build needs a C toolchain such as musl. The driver choice has no bearing on sync safety.

## Red flags for ADR 0001

- **Obsidian Sync drops `.otman/`.** ADR 0001 rejected a per-machine database so that "every device sees the same sequences and registry". On Obsidian Sync the database is per-machine anyway, silently. Options: move the db out of a dot-folder (for example `otman/otman.db`; this is still subject to the next point and needs "Sync all other types" turned on), or stop relying on the sync tool to carry it.
- **"Allocated under SQLite's transactional locking" holds only within one device.** The ADR's stated rationale ("so Item numbers can be allocated under SQLite's transactional locking") gives no cross-device guarantee on any of the three sync tools. The Consequences section already flags this as open. The research confirms it is a real hazard, not just a theoretical one: lost updates mean duplicate numbers.
- **A whole-file binary replacement also wipes out project registry changes**, not just sequence bumps. Projects created on device B can vanish when device A's copy "wins".
- **Apple says outright not to put a SQLite store in iCloud.** Its iCloud Design Guide says: "a SQLite database's store file must never be stored in iCloud".

The cross-device decision should treat the synced db as a cache of eventually-consistent state, not as a lock. Approaches worth evaluating (out of scope here): derive the next number from the max existing Item number in the markdown files at allocation time (the files sync per-file and survive), device-scoped allocation, or detecting and renumbering collisions after sync.

## Obsidian Sync

| Question | Answer | Source |
|---|---|---|
| Dot-folders? | **Not synced.** "Files and folders beginning with a `.` are treated as hidden and excluded from sync. The only exception is the vault's configuration folder (`.obsidian`), which does sync." | [Sync settings](https://obsidian.md/help/sync/settings) |
| Binary `.db` files? | Not by default. By default only notes plus images, audio, video and PDF sync. Other extensions sync only if **"Sync all other types"** is enabled, and that setting is per device. | [Sync settings](https://obsidian.md/help/sync/settings) |
| `-wal` / `-journal` / `-shm` sidecars? | Treated as "other types" if outside a dot-folder, so they sync independently of the db file. Nothing keeps a db and its sidecar together in one atomic unit. | Inference from [Sync settings](https://obsidian.md/help/sync/settings) **(no explicit statement found)** |
| Conflict resolution | Markdown: merged with diff-match-patch. "For all other files, including canvases, Obsidian uses a 'last modified wins' approach. The most recently modified version replaces earlier versions." Since 1.9.7 you can choose "Create conflict file" instead of merging. That is per device, and the documented conflict-file pattern is for notes (`… (Conflicted copy device-name YYYYMMDDHHMM).md`). | [Troubleshoot Obsidian Sync](https://obsidian.md/help/sync/troubleshoot) |
| Size limit | 100 MB per file (secondary search summary of the plans page; not material for a small db). | **(secondary)** search summary pointing at Obsidian plans/limits help |

Implication: if the db is moved out of a dot-folder and "Sync all other types" is on, concurrent writes on two devices resolve as **last-modified-wins on the whole binary file**, which is a guaranteed lost update.

## iCloud Drive

- Apple's iCloud Design Guide lists what to store in iCloud and adds the parenthetical "(a SQLite database's store file must never be stored in iCloud)". Apple's own pattern (Core Data + iCloud, now deprecated) kept the SQLite file local and synced only change logs. Sources: [iCloud Design Guide: iCloud Fundamentals](https://developer.apple.com/library/archive/documentation/General/Conceptual/iCloudDesignGuide/Chapters/iCloudFundametals.html), [Using the SQLite Store with iCloud](https://developer.apple.com/library/archive/documentation/DataManagement/Conceptual/UsingCoreDataWithiCloudPG/UsingSQLiteStoragewithiCloud/UsingSQLiteStoragewithiCloud.html) (marked deprecated).
- Apple says iCloud file operations should go through file coordination (`NSFileCoordinator`), and that for plain files "you manually resolve conflicts using file presenters" ([iCloud Fundamentals](https://developer.apple.com/library/archive/documentation/General/Conceptual/iCloudDesignGuide/Chapters/iCloudFundametals.html)). A Go CLI writing through POSIX I/O takes part in neither, so iCloud may read or replace the file mid-transaction.
- With "Optimize Mac Storage", files can be moved to cloud-only storage when space is needed ([Apple Support: Store files in iCloud Drive](https://support.apple.com/guide/mac-help/store-files-in-icloud-drive-mchle5a61431/mac)). An evicted ("dataless") db must be downloaded before SQLite can open it ([eclecticlight: FileProvider and eviction](https://eclecticlight.co/2023/11/21/icloud-drive-in-sonoma-fileprovider-and-eviction/), **secondary**).
- Names ending `.nosync` are reportedly excluded from iCloud Drive (**unverified**: no Apple primary source found).
- iCloud Drive's conflict behaviour for arbitrary files (keeping both versions as a numbered copy) has **no Apple primary source found** for non-document apps. Assume one side wins and the other appears as a conflict copy that otman would ignore.

## git

- A SQLite db is binary. The built-in `binary` macro is `-diff -merge -text`, and with `-merge` git will "take the version from the current branch as the tentative merge result, and declare that the merge has conflicts" ([gitattributes](https://git-scm.com/docs/gitattributes)). So concurrent allocations on two clones always conflict, and resolving the conflict means picking one side: a lost update unless someone rebuilds the merge by hand.
- `git checkout`, `pull` or `merge` rewrite the working-tree file. If an otman process has it open at that moment, that is a "database file overwritten while open" scenario (see the next section).
- `-wal`, `-shm` and `-journal` should be in `.gitignore`. They only exist transiently in rollback mode, but committing a hot journal paired with the wrong db version is a documented corruption cause.

## SQLite: corruption and lost-update mechanics

From [How To Corrupt An SQLite Database File](https://www.sqlite.org/howtocorrupt.html):

- §1.2: "Systems that run automatic backups in the background might try to make a backup copy of an SQLite database file while it is in the middle of a transaction. The backup copy then might contain some old and some new content, and thus be corrupt." Copying is safe "as long as there are no transactions in progress while the copy is taking place". A sync tool is a background copier.
- §1.3: if hot journals are "moved, deleted, or renamed after a crash or power failure, then automatic recovery will not work and the database may go corrupt."
- §1.4 lists corruption causes that a sync tool can trigger: "Copying a database file without also copying its journal", and "Overwriting a database file with another without also deleting any hot journal associated with the original database".
- §2.1/§2.2: locking relies on the OS. POSIX advisory locks are dropped by any `close()` on the same file within the process.
- §3.2: "SQLite should always be run with its default synchronous setting of FULL."

From [Write-Ahead Logging](https://www.sqlite.org/wal.html):

- "All processes using a database must be on the same host computer; WAL does not work over a network filesystem."
- "The WAL file is part of the persistent state of the database and should be kept with the database if the database is copied or moved. If a database file is separated from its WAL file, then transactions that were previously committed to the database might be lost, or the database file might become corrupted."
- The WAL file is "usually" deleted when the last connection closes, but it is kept after an unclean exit. `journal_mode=WAL` is persistent across opens.

From [File Locking And Concurrency in SQLite v3](https://www.sqlite.org/lockingv3.html):

- A hot journal is one that "needs to be rolled back in order to restore the integrity of its database" (created when a write is interrupted). Renaming the db without its journal, or opening it via a different alias, means the journal "will not be found".
- "POSIX advisory locking is known to be buggy or even unimplemented on many NFS implementations … Your best defense is to not use SQLite for files on a network filesystem."

From [SQLite Over a Network](https://www.sqlite.org/useovernet.html):

- Issues "can be mitigated, completely or to an acceptable degree, by using SQLite in rollback mode." WAL is acceptable only if every reader and writer is on the machine that stores the file. Otherwise SQLite recommends a client/server database, and network use is "at the user's risk".

Note: Obsidian Sync, iCloud Drive and git are **not** network filesystems. Each device has a local file with working locks. So the network-filesystem warnings apply only by analogy. The real hazards are (a) the sync agent copying the file mid-transaction or without its sidecar, (b) the sync agent replacing the file under an open connection, and (c) no cross-device lock at all, which causes lost updates.

### Recommended settings if the db stays in a synced folder

- `PRAGMA journal_mode=DELETE` (the default; do not enable WAL). The rollback journal exists only during a write and is deleted on commit, so between commands the directory holds just `otman.db`.
- `PRAGMA synchronous=FULL` (the default in rollback mode).
- `BEGIN IMMEDIATE` for the allocate-number transaction, kept as short as possible, and the connection closed when the command exits. A CLI that runs per command already minimises the window in which a sync tool sees a mid-transaction file.
- Keep a single connection (`db.SetMaxOpenConns(1)`) and never open the db file with plain file I/O in the same process (because of the POSIX `close()` lock-drop issue, §2.2).
- To snapshot or back up the db, use `VACUUM INTO` or the backup API, never a raw copy (§1.2).
- Optionally run `PRAGMA integrity_check` (or `quick_check`) on open after a sync, to detect a corrupted db early.

None of these settings prevents two-device lost updates.

## Go drivers

| Driver | cgo | Static binary / cross-compile | Locking |
|---|---|---|---|
| [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) | No ("pure-Go SQLite, no cgo"; SQLite C transpiled to Go). Driver name `"sqlite"`. | `CGO_ENABLED=0` works. Supports linux (386/amd64/arm/arm64/loong64/ppc64le/riscv64/s390x), darwin amd64/arm64, windows 386/amd64/arm64, and the BSDs. About 1.3–2x slower on CPU-bound work. | Same unix VFS as C SQLite (POSIX advisory locks). OFD locks on Linux are opt-in via `OFDLocking()` or `MODERNC_SQLITE_OFD_LOCK`, which avoids the `close()` lock-drop problem. |
| [`github.com/ncruces/go-sqlite3`](https://github.com/ncruces/go-sqlite3/blob/main/vfs/README.md) | No (SQLite compiled to Wasm, run with wazero). | `CGO_ENABLED=0` works. | Its own Go VFS. Uses OFD locks on Linux (3.15+) and macOS, BSD `flock` on BSD/illumos, and `LockFileEx` on Windows. "The default configuration of this package is compatible with the standard Unix and Windows SQLite VFSes." WAL uses `mmap` shared memory on Unix. Build tags: `sqlite3_flock`, `sqlite3_dotlk`. |
| [`github.com/mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3) | **Yes.** "you are required to set the environment variable `CGO_ENABLED=1` and have a `gcc` compiler present". | Cross-compiling or static linking needs a C toolchain, for example `CC=x86_64-linux-musl-gcc … -ldflags "-linkmode external -extldflags -static"`, or xgo. | Stock C SQLite unix VFS (POSIX advisory locks). |

All three interoperate on the same file with other SQLite processes on one host (for example a `sqlite3` CLI). None changes the cross-device picture. For a single-binary CLI distributed through `go install` or goreleaser, a pure-Go driver avoids the cgo toolchain. `modernc.org/sqlite` is the more conventional choice, and `ncruces` has the stronger locking defaults (OFD by default).

## Open / unverified

- Whether Obsidian Sync uploads a file while it is being written, or debounces until it is idle. No primary source found.
- iCloud Drive's conflict-copy behaviour for non-coordinated writers: no Apple primary source found.
- The exact iCloud `.nosync` exclusion behaviour: no Apple primary source found.
