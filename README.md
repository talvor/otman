# otman

otman (Obsidian Task Manager) runs one Obsidian Vault as an issue tracker for several Projects. Each Item is a plain Markdown note with YAML frontmatter. You can read and edit Items in Obsidian, and agents can drive them through the `otman` CLI without Obsidian running.

## Install

otman is a single static Go binary. Install it with Go:

```sh
go install github.com/talvor/otman/cmd/otman@latest
```

You can also build it from a clone with `make build`, which writes `dist/otman`, or with `make install`, which installs it into `GOBIN`.

## Setup

1. Point otman at your Vault and set your actor name. Comments, Claims and Authors carry the actor name, and `@me` resolves to it.

   ```sh
   otman config set vault ~/Obsidian/Vault
   otman config set actor alice
   ```

   These go in your user config: `$XDG_CONFIG_HOME/otman/config.toml`, or `~/.config/otman/config.toml` when `XDG_CONFIG_HOME` is unset. A relative Vault path is stored as an absolute path.

2. Create a Project. A key is an uppercase letter followed by up to 15 uppercase letters or digits.

   ```sh
   otman project create OTM --name "otman"
   ```

   This creates `Projects/OTM/` in the Vault, along with its Project note and its Bases views. The first command that touches the Vault also creates `.otman/`, which holds this device's database and lock, so no separate init step is needed. Existing `Projects/<KEY>/` folders are adopted as Projects automatically.

3. Link your code repo to the Project. Run this anywhere inside the repo:

   ```sh
   otman project link OTM
   ```

   This writes `.otman.toml` at the git root, or in the current directory outside git. The file holds only the Project key, so you can commit it and share it with other machines. To repoint a repo that is already linked to another Project, pass `--force`.

   To work outside any linked repo, set a default Project instead: `otman config set project OTM`.

4. Check what otman will use:

   ```sh
   otman config show
   ```

   `config show` prints the effective Vault, Project and actor, and where each value came from.

### Using otman with the Matt Pocock skills

`otman tracker-template` prints the Tracker template for the linked Project. The Tracker template tells the skills how to do every tracker operation through otman. Save it as the repo's issue tracker doc:

```sh
otman tracker-template > docs/agents/issue-tracker.md
```

The canonical template is [`docs/agents/issue-tracker-otman.md`](docs/agents/issue-tracker-otman.md), which is embedded in the binary. If no Project is selected, `tracker-template` fails with exit 2.

## Discovery and precedence

Each setting is taken from the first source that sets it:

| Setting | Precedence, highest first |
|---|---|
| Vault | `--vault` > `OTM_VAULT` > user config `vault` |
| Project | `--project` > `OTM_PROJECT` > nearest `.otman.toml` > user config `project` |
| Actor | `--actor` > `OTM_ACTOR` > user config `actor` |

otman looks for `.otman.toml` in the working directory and each parent, up to the git root. Outside git, it searches up to the filesystem root. Because of this, commands work from any subdirectory of a linked repo.

The actor is never derived from git or the OS. Without a configured actor, `@me` cannot resolve and Items are created without an Author.

otman never picks a Project for you. With no Project selected, commands that need one fail with exit 2 `no_project`. Use `list` and `frontier` with `--all-projects` to span every Project.

## Syncing the Vault

`.otman/` is per-device state: a SQLite database and a lock. Do not sync it.

- **Syncthing:** add `.otman` to the Vault folder's `.stignore`.
- **iCloud Drive:** iCloud has no ignore list. Keep the Vault outside iCloud Drive, or sync it with a tool that can exclude `.otman/`.
- **git:** `.otman/` contains its own `.gitignore` of `*`, so it never reaches git.

Each device rebuilds its database from the Vault when the database is missing.
