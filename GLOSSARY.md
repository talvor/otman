# otman

otman (Obsidian Task Manager) treats an Obsidian vault as an issue tracker for multiple projects, usable by both humans in Obsidian and agents via the CLI.

## Language

**Vault**:
The single Obsidian vault that otman manages; a folder of plain markdown files with YAML frontmatter.
_Avoid_: Workspace, repository, database

**Project**:
A named partition of the Vault with its own independent numbering sequence. A Project exists because its folder exists, and it is described by its Project note.
_Avoid_: Board, space, repo

**Project note**:
The note that describes a Project, such as its display name, and that serves as the Project's home page in Obsidian.
_Avoid_: Project file, project config

**High-water mark**:
The highest Item number a Project has ever issued, including numbers of Items since deleted. New Items are numbered above it, so numbers are never reused.
_Avoid_: Counter, max ID

**Item**:
A numbered record in a Project, such as `OTM-12`. Every Item has exactly one Kind and takes the next number in its Project's single sequence, whatever its Kind.
_Avoid_: Ticket, issue (when the Kind isn't meant), task

**Kind**:
What sort of Item it is: issue, PRD, or spec. An Item of any Kind can be the parent of other Items.
_Avoid_: Type, category

**Renumber**:
Giving an Item a new number because another Item in the same Project already holds its number. The Item created earlier keeps the number.
_Avoid_: Re-ID, reassign

**Author**:
Who created an Item: the person or agent acting when it was created. An Item may have no Author. Each comment also has its own Author.
_Avoid_: Reporter, creator, owner

**Tracker template**:
The `issue-tracker-otman.md` doc that tells the Matt Pocock skills how to perform tracker operations through otman.
_Avoid_: Adapter, integration, plugin

**Claim**:
An Item's assignment to the person or agent taking responsibility for it. An Item without an assignee is unclaimed.
_Avoid_: Lock, lease

**Frontier**:
The open, unclaimed Items with no open blockers within the selected scope. A parent's Frontier contains its direct children that meet those conditions.
_Avoid_: Backlog, all open Items
