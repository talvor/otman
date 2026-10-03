# Item filenames carry the title, and otman rewrites links on rename

Item files are named `<KEY>-<n> <Title>.md` and live under `Projects/<KEY>/{Issues,PRDs,Specs}/`. Links use the full filename (`[[OTM-4 Allocate item numbers]]`), so the file tree and raw links read well in Obsidian. The cost is that a title change becomes a rename, and Obsidian only rewrites links for renames made inside the app. So when a title changes, `otman` renames the file and atomically rewrites every link to it across the Vault. The filename is a sanitised projection of `title`: characters Obsidian can't link to or filesystems reject are removed, whitespace is collapsed, and the title part is truncated to 60 characters at a word boundary. `title` keeps the exact text.

## Considered Options

- **Immutable ID-only filenames (`OTM-4.md`), title in `title` and `aliases`.** The prior-art research recommended this because no writer can ever break a link. It was rejected because the file explorer and raw links show bare IDs, which are unreadable when browsing the Vault by hand.
- **Title frozen into the filename at creation.** Rejected: filenames would drift from titles and mislead readers.
- **Retitling only through Obsidian's rename.** Rejected: agents work headless and must be able to retitle.

## Consequences

- Link rewriting across the whole Vault is a core otman responsibility, not an optimisation. It has to be atomic enough that an interrupted retitle doesn't leave links half rewritten.
- A rename made outside Obsidian and otman (git, a file manager, an agent editing files directly), or a hand edit of `title:`, can leave filename and title out of step or leave links dangling. Detecting and repairing that is open on the wayfinder map under "Hand edits and drift".
- Changing an Item's Kind moves its file to another subfolder. Links survive the move because they resolve by filename.
