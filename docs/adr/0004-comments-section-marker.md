# An HTML comment marks where an Item's comments begin

An Item file is frontmatter, then a free-form Markdown body, then the otman-managed comments section. The boundary between body and comments is the exact line `<!-- otman:comments -->`, placed immediately before the `## Comments` heading. otman splits the file at that marker, not at the heading, so a body (whether hand-written, from a template or passed with `--body`) may contain its own `## Comments` heading. Only the marker line is reserved: create, edit and templates that contain it are rejected with exit 2 (`reserved_marker`, or `invalid_template` for a template file).

## Considered Options

- **Reserve the `## Comments` heading** and reject bodies containing it. Rejected because it forbids a heading people might genuinely write.
- **Treat the last `## Comments` heading as the boundary.** Rejected because a body ending in its own `## Comments` section would swallow, or be swallowed by, the real one.
- **Obsidian's `%% … %%` comment syntax.** It hides better in Live Preview, but it is Obsidian-only. `<!-- -->` is plain Markdown that renders invisibly on GitHub and in other editors, which fits the plain-files principle.

## Consequences

- Obsidian's reading view hides the marker, while Live Preview and Source mode show it as a faint line. A human editing in Obsidian can delete it. Detecting and repairing a missing marker (falling back to the last `## Comments` heading with a warning, and `doctor --fix` restoring it) belongs to hand-edit and drift handling.
- Individual comments carry no markers in v1. They are delimited by their `### <timestamp> · <author>` headings. Per-comment markers can be added if comment editing or deletion is ever introduced.
- Item bodies no longer start with a `# Title` H1 (a change from the vault-layout prototype). The title lives only in `title` and the filename, and Obsidian's inline title displays it.
