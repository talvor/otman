#!/usr/bin/env python3
"""PROTOTYPE, throwaway. Generates three sample otman vaults for the
'Vault layout prototype' wayfinder ticket (talvor/otman#4).
Run: python3 generate.py   -> writes variant-a/, variant-b/, variant-c/ (wiped each run)."""
import os, shutil, textwrap

HERE = os.path.dirname(os.path.abspath(__file__))

# Shared sample data: two Projects, Items of every Kind, parent/child,
# blocking (including cross-project), a closed Item, and comments.
ITEMS = [
    dict(id="OTM-1", kind="prd", title="Vault-backed issue tracker", status="open", labels=["needs-triage"],
         body="## Problem\n\nAgents and humans need one tracker that lives in the Obsidian vault.\n\n## Solution\n\nA CLI, otman, that treats the vault as an issue tracker.\n"),
    dict(id="OTM-2", kind="spec", title="Item file format", status="open", parent="OTM-1", labels=["ready-for-human"],
         body="## Frontmatter\n\nFlat properties, quoted wikilinks for relations.\n"),
    dict(id="OTM-3", kind="issue", title="Parse vault config", status="closed", parent="OTM-1", labels=["ready-for-agent"], assignee="phillip",
         body="Read `.otman/config` and resolve the Project from the cwd.\n",
         comments=[("2026-10-02 14:10", "phillip", "Picked this up."), ("2026-10-03 09:30", "claude", "Done; config resolves from the repo pointer.")]),
    dict(id="OTM-4", kind="issue", title="Allocate item numbers", status="open", parent="OTM-1", blocked_by=["OTM-3"], labels=["ready-for-agent"],
         body="Take the next number from the Project's sequence.\n"),
    dict(id="OTM-5", kind="issue", title="Frontier command", status="open", parent="OTM-1", blocked_by=["OTM-4", "OTM-2"], labels=["needs-info"],
         body="List open, unblocked, unassigned children of a map.\n"),
    dict(id="WEB-1", kind="prd", title="Project website", status="open", labels=[],
         body="## Problem\n\notman needs a landing page.\n"),
    dict(id="WEB-2", kind="issue", title="Docs page for numbering", status="open", parent="WEB-1", blocked_by=["OTM-4"], labels=["ready-for-human"],
         body="Explains how Item numbers are allocated. Blocked across Projects on OTM-4.\n"),
]
PROJECTS = {"OTM": "otman", "WEB": "otman website"}

def fm(item, extra=None, link=lambda i: i):
    lines = ["---", f"id: {item['id']}", f"title: {item['title']}", f"aliases:\n  - \"{item['title']}\"",
             f"kind: {item['kind']}", f"status: {item['status']}"]
    if extra: lines += extra
    if item.get("parent"): lines.append(f"parent: \"[[{link(item['parent'])}]]\"")
    if item.get("blocked_by"):
        lines.append("blocked_by:"); lines += [f"  - \"[[{link(b)}]]\"" for b in item["blocked_by"]]
    lines.append("labels:" + ("" if item["labels"] else " []")); lines += [f"  - {l}" for l in item["labels"]]
    if item.get("assignee"): lines.append(f"assignee: {item['assignee']}")
    lines += ["created: 2026-10-01", "updated: 2026-10-03", "---", ""]
    return "\n".join(lines)

def body(item):
    out = f"# {item['title']}\n\n{item['body']}"
    if item.get("comments"):
        out += "\n## Comments\n"
        for ts, who, text in item["comments"]:
            out += f"\n### {ts} · {who}\n\n{text}\n"
    return out

def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "w").write(text)

def bases(root, project_folder_expr, key, status_open='status == "open"'):
    # Kanban view needs Obsidian 1.14 (early access); table grouped by status works on 1.13.
    write(os.path.join(root, f"Boards/{key} Board.base"), textwrap.dedent(f"""\
        filters:
          and:
            - {project_folder_expr}
            - file.ext == "md"
        views:
          - type: table
            name: By status
            groupBy:
              property: status
              direction: ASC
            order: [file.name, title, kind, labels, parent, blocked_by]
          - type: table
            name: Open
            filters:
              and:
                - '{status_open}'
            order: [file.name, title, kind, labels, assignee]
          - type: kanban
            name: Board (needs 1.14)
            groupBy:
              property: status
              direction: ASC
            order: [title, kind]
        """))
    # Sidebar base: relations of the active note (uses `this`).
    write(os.path.join(root, "Boards/Relations.base"), textwrap.dedent("""\
        views:
          - type: table
            name: Children
            filters:
              and:
                - 'parent == this'
            order: [file.name, title, status]
          - type: table
            name: Blocks
            filters:
              and:
                - 'blocked_by.contains(this)'
            order: [file.name, title, status]
        """))

def config(root, dirname):
    write(os.path.join(root, dirname, "config.toml"),
          "# PROTOTYPE. Project registry would be canonical in otman.db (ADR 0001); shown here for readability.\n"
          + "".join(f'[projects.{k}]\nname = "{v}"\n' for k, v in PROJECTS.items()))
    write(os.path.join(root, dirname, "otman.db.PLACEHOLDER"), "SQLite db would live here (see ADR 0001 / number-safety ticket).\n")

def readme(root, text):
    write(os.path.join(root, "README (prototype).md"), text)

def variant_a(root):
    """Folder per Project, ID-only filenames, hidden .otman/."""
    for it in ITEMS:
        key = it["id"].split("-")[0]
        write(os.path.join(root, "Projects", key, it["id"] + ".md"), fm(it) + body(it))
    for key in PROJECTS: bases(root, f'file.inFolder("Projects/{key}")', key)
    config(root, ".otman")
    readme(root, "# Variant A: folder per Project, ID filenames\n\n- `Projects/<KEY>/<KEY>-n.md`\n- Title in `title` + `aliases` (search/link autocomplete by title)\n- Config in hidden `.otman/` (note: Obsidian Sync skips dot-folders, and Obsidian hides them in the file explorer)\n")

def variant_b(root):
    """Folder per Project with Kind subfolders, ID-only filenames, visible _otman/."""
    sub = {"issue": "Issues", "prd": "PRDs", "spec": "Specs"}
    for it in ITEMS:
        key = it["id"].split("-")[0]
        write(os.path.join(root, "Projects", key, sub[it["kind"]], it["id"] + ".md"), fm(it) + body(it))
    for key in PROJECTS: bases(root, f'file.inFolder("Projects/{key}")', key)
    config(root, "_otman")
    readme(root, "# Variant B: folder per Project, subfolder per Kind\n\n- `Projects/<KEY>/{Issues,PRDs,Specs}/<KEY>-n.md`\n- Kind is visible in the file tree (and duplicated in `kind:`)\n- Config in a visible `_otman/` folder, so it is shown in Obsidian and synced by Obsidian Sync\n")

def variant_c(root):
    """Flat Items/ folder, project in frontmatter, ID + title filenames."""
    name = {it["id"]: f"{it['id']} {it['title']}" for it in ITEMS}
    for it in ITEMS:
        key = it["id"].split("-")[0]
        write(os.path.join(root, "Items", name[it["id"]] + ".md"),
              fm(it, extra=[f"project: {key}"], link=lambda i: name[i]) + body(it))
    for key in PROJECTS: bases(root, f'project == "{key}"', key)
    config(root, ".otman")
    readme(root, "# Variant C: flat Items/, title in filename\n\n- `Items/<KEY>-n <Title>.md`, Project set by the `project:` property\n- Readable file explorer, but every title edit is a rename (otman must rewrite links itself; renames on disk don't update links)\n- Links look like `[[OTM-4 Allocate item numbers]]`\n")

for v, fn in [("variant-a", variant_a), ("variant-b", variant_b), ("variant-c", variant_c)]:
    root = os.path.join(HERE, v)
    shutil.rmtree(root, ignore_errors=True)
    fn(root)
    print("wrote", root)
