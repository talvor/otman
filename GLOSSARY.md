# otman

otman (Obsidian Task Manager) treats an Obsidian vault as an issue tracker for multiple projects, usable by both humans in Obsidian and agents via the CLI.

## Language

**Vault**:
The single Obsidian vault that otman manages; a folder of plain markdown files with YAML frontmatter.
_Avoid_: Workspace, repository, database

**Project**:
A named partition of the Vault with its own independent numbering sequence.
_Avoid_: Board, space, repo

**Item**:
A numbered record in a Project, such as `OTM-12`. Every Item has exactly one Kind and takes the next number in its Project's single sequence, whatever its Kind.
_Avoid_: Ticket, issue (when the Kind isn't meant), task

**Kind**:
What sort of Item it is: issue, PRD, or spec. An Item of any Kind can be the parent of other Items.
_Avoid_: Type, category

**Tracker template**:
The `issue-tracker-otman.md` doc that tells the Matt Pocock skills how to perform tracker operations through otman.
_Avoid_: Adapter, integration, plugin
