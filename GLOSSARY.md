# otman

otman (Obsidian Task Manager) treats an Obsidian vault as an issue tracker for multiple projects, usable by both humans in Obsidian and agents via the CLI.

## Language

**Vault**:
The single Obsidian vault that otman manages; a folder of plain markdown files with YAML frontmatter.
_Avoid_: Workspace, repository, database

**Project**:
A named partition of the Vault with its own independent numbering sequence. A Project exists because its folder exists, and it is described by its Project note. Its key, such as `OTM`, is an uppercase letter followed by up to 15 uppercase letters or digits.
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
What sort of Item it is: issue, PRD, or spec. An Item of any Kind can be the parent of other Items. A **PRD** says why and what: the problem, who it's for, goals, non-goals and success measures. A **spec** says how: the solution and the decisions needed to build and test it, often as the child of a PRD. An **issue** is a single piece of work, such as a ticket or bug.
_Avoid_: Type, category

**Template**:
The starting body a new Item of a given Kind receives when no body is supplied.
_Avoid_: Scaffold, boilerplate, skeleton

**Drift**:
A disagreement, introduced outside otman, between parts of an Item that otman keeps consistent, such as filename vs title, folder vs Kind, or a missing comments marker. Drift is expected, warned about on read and repaired by `doctor`.
_Avoid_: Corruption, inconsistency

**Renumber**:
Giving an Item a new number because another Item in the same Project already holds its number. The Item created earlier keeps the number.
_Avoid_: Re-ID, reassign

**Actor**:
The configured identity of whoever is running otman, from `--actor`, else `OTM_ACTOR`, else the user config. `@me` resolves to it, and comments, Claims and Authors carry it. It is never derived from git or the OS.
_Avoid_: User, identity, whoami

**Author**:
Who created an Item: the person or agent acting when it was created. An Item may have no Author. Each comment also has its own Author.
_Avoid_: Reporter, creator, owner

**Label**:
A free-form, lowercase marker on an Item, used for triage roles and grouping. A label exists as long as some Item carries it; there is no declared vocabulary.
_Avoid_: Tag (Obsidian tags are a different property), category

**Tracker template**:
The `issue-tracker-otman.md` doc that tells the Matt Pocock skills how to perform tracker operations through otman.
_Avoid_: Adapter, integration, plugin

**Claim**:
An Item's assignment to the person or agent taking responsibility for it. An Item without an assignee is unclaimed.
_Avoid_: Lock, lease

**Frontier**:
The open, unclaimed Items with no open blockers within the selected scope. A parent's Frontier contains its direct children that meet those conditions.
_Avoid_: Backlog, all open Items
