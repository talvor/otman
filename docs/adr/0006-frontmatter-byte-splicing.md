# Frontmatter is rewritten by byte splicing, not re-encoding

ADR 0005 promises that otman rewrites only the frontmatter keys it changes. Re-encoding a parsed YAML tree can't keep that promise, because it normalises blank lines, indentation and comment placement across the whole block. So otman parses frontmatter with `go.yaml.in/yaml/v3` Nodes **only to read values and locate keys**, and writes by **splicing**: it re-serialises just the changed key in its own style and replaces that key's byte span in the original text. Frontmatter is flat, so a key's span runs from its line to the line before the next top-level key, minus any trailing blank or comment lines. A new key is inserted just before the closing `---`, and a removed key's span is deleted. The body is never re-encoded.

## Considered Options

- **Round-trip through yaml.v3 Nodes:** parse, edit the tree, re-encode the whole block. It is the simplest option and always valid, but the first write to a hand-edited file normalises the whole block (blank lines lost, comments drifting). ADR 0005 would have needed weakening to "stable after the first write". Rejected.
- **goccy/go-yaml's token-preserving AST:** close to byte-preserving with no custom code, but fidelity rests on how goccy prints its tokens, which is less proven in edge cases than yaml.v3. Rejected without a prototype, since splicing gives the guarantee directly.

## Consequences

- **Safeguard:** after splicing, otman re-parses the result and checks that every other key is semantically unchanged before writing. If the check fails, the write is refused with `unsafe_write` instead of producing a bad file.
- **Flow-style frontmatter** (for example `{id: OTM-4, title: …}` on one line) can't be spliced. Writes refuse it with `unsafe_write`, and `doctor` reports it.
- A rewritten key loses its own same-line trailing comment. Every other comment, blank line, quoting choice and unowned key is kept byte for byte. Existing line endings survive; new files use LF.
- The round-trip test asserts byte-identical output: parsing and writing a fixture with no change reproduces its exact bytes, including hand-edited YAML. The splicing module also gets a fuzz test.
- Editing a property through Obsidian's Properties UI still rewrites the frontmatter in Obsidian's own style. Byte preservation protects YAML typed by hand or written by other tools, and keeps git history clean.
