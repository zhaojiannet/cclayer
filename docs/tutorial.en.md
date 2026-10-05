# cclayer tutorial

[中文](tutorial.zh.md) · [日本語](tutorial.ja.md)

This tutorial explains from scratch what cclayer is, how to install it and how to use it. If the README left questions open, read this.

## 1. What problem it solves

Claude Code keeps its configuration under `~/.claude/`: the global rules `CLAUDE.md`, `rules/`, `skills/`, output styles, hook scripts, `settings.json`, plugins, MCP servers. Once you have these the way you want them, you want them the same on every machine.

At the same time, the projects on your disk may come from more than one team. Each team has its own git commit identity, its own rules and hooks. These should take effect only in that team's projects, never mix into other projects, and never appear on another team's machines.

cclayer splits the configuration into two kinds of "layers":

| Layer | What goes in | How many | Can it be public |
|---|---|---|---|
| Base layer `base` | What is the same on every machine: `CLAUDE.md`, `rules/`, `skills/`, output styles, hook scripts, the shared keys of `settings.json`, plugin and marketplace lists, MCP definitions | 1 | Yes. It contains no identity |
| Overlay | What belongs to one team: git identity, hooks, the remote URL patterns of its repositories, the `.claude/settings.local.json` keys and the `CLAUDE.local.md` block written into its projects | 1 per team | Private |

Each machine gets the base layer plus the overlays it is allowed to use. One command applies them to the machine, one command writes local changes back.

Three typical setups:

- **Just you, several machines**: one base layer is enough; overlays are optional.
- **Several teams, your own machines**: the base layer plus one overlay per team, applied automatically by project remote URL.
- **A machine issued by a team**: the base layer plus that one team's overlay; the other teams' layers are not installed.

## 2. A few terms

- **Layer**: a directory with a `layer.toml` at its root. It can be a git repository (cclayer clones it to the machine) or just a directory on this machine, for example a directory inside a cloud drive's sync folder.
- **Device manifest**: `~/.config/cclayer/device.toml`. It records which layers this machine has enabled, where each layer lives on this machine and where to look for projects. It stays on this machine and never enters any repository.
- **Clone directory**: the local copy of a git layer, by default `~/.local/share/cclayer/<layer>`; the Local clones row of setup, or `clone_dir` in the device manifest, picks another parent folder. A directory layer has no copy; it is the directory itself.
- **Project roots**: cclayer looks for git repositories under these directories and checks which overlay their remote URLs belong to.
- **Match**: when an overlay's `[[match]] remote = "github.com/acme-inc/*"` fits a repository's remote URL, that repository belongs to that overlay. A repository belongs to at most one overlay.
- **Inject**: apply writes the contents of the overlay's `project/` into the matched project's `.claude/settings.local.json` and `CLAUDE.local.md`. These two files are registered in the global git exclude list and are not committed into the project.
- **State directory**: `~/.local/state/cclayer/`. It records the hashes of the files the last apply wrote, which project belongs to which overlay, the plugin commands already run, and backups of overwritten files.

What cclayer does not touch: Claude Code logins, session records, prompt history, `~/.claude.json`, `projects/`, and the `env`, `permissions.allow`, `permissions.defaultMode` and `permissions.ask` keys of `settings.json`. These always stay on the machine.

## 3. Install

On macOS with Homebrew:

```sh
brew install --cask zhaojiannet/tap/cclayer
```

Upgrade to a new version later with `brew upgrade --cask cclayer`.

On Linux, Windows and machines without Homebrew, download the archive for your platform from [GitHub Releases](https://github.com/zhaojiannet/cclayer/releases), extract it and put `cclayer` on your PATH.

git is also required. The plugin, MCP and `leave` purge steps need Claude Code 2.1.288 or later. Without `claude` on PATH, the plugin and MCP steps are skipped with a notice, and `leave` stops before purging anything.

Once installed:

```sh
cclayer version
cclayer help
```

## 4. The simplest setup: one person, several machines, directory sync

If you do not want to deal with git, a layer is just a directory. Put it in your cloud drive's sync folder and give every machine the same path.

### The first machine

```sh
cclayer setup
```

setup is a full-screen editor. The left column has two boxes: "Layers" lists the base layer and the overlays this device uses, and "This device" holds the settings that belong to the machine, such as project dirs and auto pull. The right column explains the item selected on the left, holds its fields and says what saving will do with it, for example where it clones a repository or writes a starter file. The bottom line says what Save still needs, next to three buttons: "Save and apply", "Save only" and "Quit". The cursor starts on the first field to fill in. Up and Down choose, Enter edits, Tab moves between the columns and the buttons, Esc goes back, and `?` lists every key. Nothing is written until you save. Messages follow the system language; the "Language" row switches between English, 简体中文 and 日本語.

This machine needs two rows:

1. **Base layer**: enter a directory, for example `~/Dropbox/cclayer/base`. The directory does not exist yet, so a starter `layer.toml` is written when you save.
2. **Project dirs**: the directories where you keep code, comma-separated, for example `~/Projects`.

Leave the other rows as they are. Auto pull is off by default; turned on, every Claude Code session starts with an automatic `apply` once the SessionStart hook from section 10 is in the base layer's `claude/settings.json` (a directory layer has no pull step; only apply runs). Profiles stay off for now (see section 9). Trust no layer. Then pick "Save and apply"; `doctor` runs after the apply.

The starter `layer.toml` looks like this:

```toml
[layer]
name = "base"
kind = "base"
# private = true   # only you use this layer and it lives somewhere private:
#                  # check then lets email addresses through

[claude]
paths = ["CLAUDE.md", "rules/", "skills/", "output-styles/", "agents/", "hooks/"]
settings_keys = ["attribution", "permissions.deny", "hooks", "statusLine",
  "enabledPlugins", "extraKnownMarketplaces", "outputStyle", "effortLevel", "theme"]
ignore = ["skills/.trash/"]
```

`paths` lists the files and directories under `~/.claude` the layer manages; `settings_keys` lists the keys in `settings.json` the layer manages. When only you use the layer and the directory sits only in your cloud drive, uncomment the `private = true` line and `check` stops refusing email addresses in your rules. The layer is still empty at this point, so apply writes nothing.

Next, collect the configuration already on this machine into the layer:

```sh
cclayer capture
```

It lists the files that exist only on this machine and not in the layer, and tells you to admit them with `--add`:

```sh
cclayer capture --add CLAUDE.md --add rules/ --add skills/
```

Paths are relative to `~/.claude`. Before writing, every file passes `check`: strings shaped like secrets, email addresses and absolute paths pointing into your home directory are refused, with the line reported; fix them and capture again. `settings.json` needs no `--add`; the keys listed in `settings_keys` are written back whenever they changed on this machine.

Then take a look:

```sh
cclayer status
ls ~/Dropbox/cclayer/base/claude
```

Wait for the cloud drive to finish syncing the directory.

### The second machine

Install cclayer, wait for the cloud drive to sync `~/Dropbox/cclayer/base` down, then:

```sh
cclayer setup
```

Enter the same path for the base layer. This time the directory already has a `layer.toml`, and setup uses it as is. "Save and apply" gives this machine's `~/.claude` the configuration from the first machine. A file that already exists on this machine with different content counts as a conflict: apply shows it and asks before overwriting, and backs the old version up under `~/.local/state/cclayer/backups/` first.

### From then on

- Whichever machine changed something under `~/.claude`: `cclayer capture`, then wait for sync.
- The other machine: `cclayer apply`. With auto-pull on and the hook from section 10 in place, this happens automatically when a Claude Code session starts.
- Both sides changed the same file: `apply` lists it as a conflict. When run interactively it asks whether to overwrite the local file with the layer's version; in automatic mode it leaves the file alone.

Note: a cloud drive syncs plain files and has no version history. When two machines change the same file at the same time, the cloud drive's own handling decides. For history and merging, use the git approach in section 5.

## 5. Syncing with a git repository

### Creating the base layer repository

The easiest way is to first create a directory layer on this machine as in section 4, collect the content, then turn that directory into a repository:

```sh
cd ~/cclayer-base            # the directory entered during setup
git init -b main
git add -A
git commit -m "base layer"
git remote add origin git@github.com:you/cclayer-base.git
git push -u origin main
```

The base layer contains no identity, so the repository can be public; private works too.

You can also create the repository on GitHub first, write `layer.toml` and the `claude/` directory by hand, push them, and then run `setup` on the machine with the repository URL. An empty repository does not work: `setup` needs to find a `layer.toml` in it.

### Other machines

```sh
cclayer setup
```

Enter the repository URL as the base layer location, `https://github.com/you/cclayer-base.git` or `git@github.com:you/cclayer-base.git`. For a public repository, choose "Nothing" as the way to reach it. cclayer clones it to `~/.local/share/cclayer/base`.

### Daily use

```sh
cclayer push                 # upload: capture, show what would be committed, commit and push
cclayer apply --pull         # download: fast-forward the clones, then apply to this machine
```

`push` lists each layer's changes and asks once before committing. When another machine pushed in the meantime it puts this commit on top of theirs; when their commits conflict with yours it stops and leaves the clone as it was. `-m` gives every layer the same message; to write a different one per layer, run `cclayer capture` and commit in each clone yourself.

`capture` deliberately does not commit or push, so you can review the diff first. `status` shows whether each clone has uncommitted changes and how many commits it is ahead of the remote.

## 6. Adding a team: overlays

One private repository (or directory) per team. Contents:

```
layer.toml
project/settings.local.json    keys written into the matched project's .claude/settings.local.json
project/CLAUDE.local.md        the managed block written into the matched project's CLAUDE.local.md
git/fragment.gitconfig         optional; this team's hooks and aliases, applied in matched repositories only
```

`layer.toml`:

```toml
[layer]
name = "acme"
kind = "overlay"

[identity]
name = "Full Name"
email = "me@acme.example"

[[match]]
remote = "github.com/acme-inc/*"

[inject]
settings_keys = ["permissions.deny", "enabledPlugins", "extraKnownMarketplaces"]
claude_local = "project/CLAUDE.local.md"

[git]
fragment = "git/fragment.gitconfig"
```

Section by section:

- `[identity]`: git commits with this name and email in matched repositories. cclayer uses `includeIf "hasconfig:remote.*.url:..."` so it takes effect only in those repositories and never touches others.
- `[[match]]`: remote URL patterns. The host and the owner must be written literally, without wildcards, and two overlays may not name the same host and owner. After them, `*` matches one segment, `**` matches several. You can write more than one. Remotes written as `https://`, `git@` and `ssh://` all match, in any case.
- `[inject] settings_keys`: which keys of `project/settings.local.json` are written into the project. `permissions.allow` is never touched; the allow entries the project itself approved are kept.
- `[inject] claude_local`: the contents of this file are written between `<!-- cclayer:begin -->` and `<!-- cclayer:end -->` in the project's `CLAUDE.local.md`; content outside the block is left alone.
- `[git] fragment`: a raw git config fragment. It must not contain `[user]` (identity goes through `[identity]`), remote URLs or `include`. Common settings listed key by key, such as `pull.rebase`, `push.default`, `merge.conflictstyle` and `diff.algorithm`, are allowed; for any other key, aliases included, and keys that make git run a program such as `core.hooksPath` or `credential.helper`, the layer must be listed in the device manifest's `trust_exec`, otherwise apply refuses.

An overlay must not have a `[claude]` section; it never writes under `~/.claude/`.

When you add an overlay in setup and enter a directory that does not exist, setup goes on to ask for the committer name, email and remote pattern, and writes the starter files above when you save, without the `[git]` part and the fragment file, which you add when the team needs them.

### Adding it to a machine

```sh
cclayer layer add acme git@github.com:you/cclayer-acme.git
```

The name is the team's short name: lowercase letters, digits, hyphens. For a repository URL it first asks whether to use a deploy key or a token (see section 7); `--method deploy-key` or `--method token` answers in advance, and `--method none` skips the question when git can already reach the repository (a public one, or credentials you manage). A directory must already hold a `layer.toml`; only setup writes a starter one. It sets up the credential, clones the layer, checks that the device can take it and fills the blocklist; if any step fails, the device manifest is left as it was. Run `cclayer apply` afterwards.

You can also run `cclayer setup` again and pick "+ Add an overlay" under "Layers".

Or edit `~/.config/cclayer/device.toml` directly:

```toml
layers = ["base", "acme"]
roots = ["~/Projects"]

[clone]
base = "~/.local/share/cclayer/base"
acme = "~/.local/share/cclayer/acme"

[repo]
base = "https://github.com/you/cclayer-base.git"
acme = "git@github.com:you/cclayer-acme.git"
```

Then `cclayer init` clones the missing layers and `cclayer apply` applies them.

### Verifying

```sh
cclayer status                   # lists the projects that belong to each overlay under it
cd ~/Projects/acme-api
git config --get user.email      # should be me@acme.example
cat .claude/settings.local.json
cat CLAUDE.local.md
cclayer doctor                   # checks that the identity is right, the include block is in place and the exclude list is complete
```

In a repository no overlay matches, git refuses to commit and asks you to set an identity while `default_identity` is empty. This is deliberate, to prevent committing with the wrong identity. To make such repositories default to one layer's identity, pick it in the "Default identity" row of setup, or write `default_identity = "personal"` in the manifest.

### Several teams

Add one more layer per team. The order of `layers` is the apply order and later layers win, but a project belongs to only one overlay; when two overlays' patterns both fit the same repository, apply lists it as a conflict and asks you to change the patterns.

## 7. Credentials for private layer repositories

On a machine issued by a team, you usually have only the git credentials that team gave you, which cannot reach your private layer repositories. `keys setup` offers two ways, chosen per layer:

### Deploy key (automatic)

```sh
cclayer keys setup acme --method deploy-key
```

cclayer generates an ed25519 key `~/.ssh/cclayer-acme` on this machine, adds a managed `Host cclayer-acme` stanza at the top of `~/.ssh/config`, and rewrites the layer URL to go through this alias. If `gh` is logged in and you are an administrator of the repository, it attaches the public key to the repository with write access through `gh api`; otherwise it prints the public key and the command to run, for you to run where you have access. Finally it verifies the connection with `git ls-remote`.

A deploy key opens one repository only and is not tied to any account; if the machine is lost, delete the key in the repository settings. If the network blocks port 22, add `--port-443` to go through `ssh.github.com:443`.

### HTTPS token (created by hand)

```sh
cclayer keys setup acme --method token
```

cclayer prints the steps to create the token: open GitHub's fine-grained token page, choose the account or organization that owns the repository as the resource owner, choose "Only select repositories" for repository access and tick the layer repository, and grant Contents read and write. Once created, paste it into cclayer. It verifies once with this token first (without going through any credential helper); if the repository clearly rejects it, it is not stored. If it passes, it is stored in git's credential helper for this repository path only, and never written to any file.

Use this when you are not an administrator of the repository or when SSH is blocked.

### Listing and removing

```sh
cclayer keys list
cclayer keys remove acme     # deletes the key and ssh config stanza, or tells you how to drop the token; prints what to revoke on GitHub
```

Choosing how to reach a repository in setup does exactly this. A directory layer needs no credentials, and setup does not ask.

## 8. Daily commands in detail

### apply

```sh
cclayer apply [--pull] [--mcp-force] [--replay-plugins]
cclayer apply --hook
```

It does these things in order:

1. Takes a lock so two applies cannot run at once.
2. With `--pull`, fast-forwards each git layer's clone to the remote first. A clone with uncommitted changes, one without an upstream branch, one that is not a fast-forward, or no network is skipped with an explanation; a directory layer reports "nothing to pull".
3. Runs `check` on every layer and refuses content that must not enter a layer.
4. Mirrors the files listed in `paths` under the base layer's `claude/` into `~/.claude`. It remembers what it wrote last time: a file changed on this machine while the layer also changed is a conflict; interactive mode asks whether to overwrite, `--hook` leaves it alone and names it. A file deleted on this machine counts as a local change: if the layer did not change it is not rewritten, if the layer also changed it is a conflict. Overwritten files are backed up first.
5. Merges `settings.json`: only the keys listed in `settings_keys` are touched, other keys are kept as they are. Keys known to be harmless, such as appearance, model, `attribution` and `permissions.deny`, are merged as they are. Keys that only restrict Claude Code are merged without a question while the layer keeps or adds to the limit, and confirmed when it drops or loosens one; a key the layer only keeps off (`false` where the device has it off or unset) is merged without a question, except switches where `false` loosens a limit; any other key, in particular one that makes Claude Code run programs (`hooks`, `statusLine`, helper commands such as `apiKeyHelper`, `extraKnownMarketplaces`, `enabledPlugins`), is shown and confirmed first; among mirrored files, a change to anything under `hooks/`, `skills/`, `commands/` or `agents/`, to any file that is not markdown, or to an executable or shebang file is listed with its content and confirmed too.
6. Generates `~/.gitconfig.cclayer` and one `~/.gitconfig.cclayer.d/<layer>.gitconfig` per overlay, and maintains a marked include block at the end of `~/.gitconfig`; the rest of the file is left alone. The block is found by the path it includes, so one whose marker comment was lost is still replaced, not added twice.
7. Plugins and marketplaces: lists the `claude plugin` commands to run according to the base layer's lists, and runs them after a single confirmation. They run once per machine; `--replay-plugins` runs them again. Marketplaces and user plugins the machine already has are skipped either way, since registering a marketplace again would drop settings such as `autoUpdate`. Overlay plugins are installed into matched projects with `--scope local`.
8. MCP servers: each file `mcp/<name>.json` in the base layer is one server; only servers `claude mcp get` does not know are added, `--mcp-force` removes and re-adds them. Secrets are not written in plain text but as environment variable references like `${VAR}`.
9. Writes the injected files into every matched project and registers them in the global git exclude list. Such a key an overlay writes into a project is shown and confirmed first as well. Projects that no longer match have the injected content removed.
10. With profiles on, copies the base layer into each profile directory.

`--hook` is the non-interactive mode for the SessionStart hook: it asks nothing, and anything that needs confirmation is skipped and listed.

### capture

```sh
cclayer capture [--add <path relative to ~/.claude>]... [--from <project directory>]
```

The reverse of apply. Among the paths the layer manages under this machine's `~/.claude`, files that differ from what the last apply wrote are written back to the owning layer. New files that exist only on this machine are listed and left alone; `--add` admits them, one file at a time or a whole directory (every new file below it). Keys of `settings.json` listed in `settings_keys` are written back when changed.

`.claude/settings.local.json` and `CLAUDE.local.md` edited inside matched projects are written back to the overlay too; when several projects were changed inconsistently it refuses, and `--from` names the project to take.

### push

```sh
cclayer push [--add <path>]... [-m <message>] [--yes]
```

Runs capture (with the same `--add`), then `check` over every layer, since a clone may hold edits made by hand. Lists the changes of every layer repository, asks once, commits with the message given or "cclayer: capture from <host>", pulls with rebase and pushes. Commits already made but not pushed go out too. A conflict with the remote aborts the rebase for that layer and names the clone to merge in; the other layers still go. Directory layers are skipped.

Every file passes `check` first; refused files are not written and the reason is reported. Writing only changes the files in the layer directory; for a git layer you commit and push yourself.

### check

```sh
cclayer check
```

Checks every layer directory. Content refused in all layers:

- Strings shaped like secrets (tokens of various kinds, `KEY=` and secret values in JSON)
- `[user]` and remote URLs in git fragments
- Claude Code runtime files, `projects/`

The base layer additionally refuses:

- Email addresses (except example domains like `example.com`). Not checked when the base declares `private = true` in its `layer.toml`
- Words in the device manifest's `blocklist`. `blocklist` is filled automatically from the overlays by `init`/`setup`: layer names, committer names, the local part and domain of email addresses, and organization names in remote patterns. This keeps team names out of the public base layer. Single characters are left out. When a word does belong in the base layer (say the GitHub user that owns your public plugin marketplace), list it in the device manifest's `blocklist_except`; `init` leaves it out of `blocklist` and `check` lets it through
- `[includeIf]` and absolute paths pointing into your home directory

apply and capture run it automatically.

### status

One line per layer: directory, whether it is a local directory, whether it has uncommitted changes, how many commits it is ahead of or behind the remote. Then the projects that belong to each overlay, followed by the unmatched and conflicting ones.

### doctor

Checks for known pitfalls and suggests fixes: whether `~/.claude` is a git repository, whether there are symbolic links, whether `CLAUDE_CONFIG_DIR` is set correctly, whether the include block of `~/.gitconfig` is at the end, whether the global exclude list is complete, whether the git identity in each matched project is right, whether `claude` is on PATH. With profiles on, it also checks each profile directory for symbolic links and for a `CLAUDE.md` (`~/.claude/CLAUDE.md` is loaded anyway; another copy in the profile would be loaded twice).

### leave

```sh
cclayer leave acme [--force]
```

Use it when you stop working for a team. It first lists the projects to clean up for confirmation, then: runs `claude purge` on each project to clear Claude Code's project state, deletes the profile directory, removes the injected files, deletes the layer's credentials, regenerates the git configuration without the layer, saves the manifest, and deletes the clone last. It refuses while the clone has uncommitted or unpushed changes; only `--force` deletes it. A layer that is a directory you pointed cclayer at is not deleted and stays in place. At the end it lists what remains to be done by hand, such as revoking credentials on GitHub.

### init

Reads the `device.toml` you wrote, clones the missing layers and fills `blocklist`. It asks only whether to turn on auto-pull. Without a `device.toml` it asks for the layer URLs, project directories and default identity in plain prompts and writes one. Run `apply` after it.

## 9. Profiles: a separate Claude Code directory per team

By default all projects share `~/.claude`, with one set of logins, session records and prompt history. To separate them per team, set `profiles = true` in the device manifest (setup has a row for it too). From then on, apply copies the base layer into `~/.claude-profiles/<layer>/`, one copy per overlay, leaving out `CLAUDE.md`, which Claude Code reads from `~/.claude` anyway.

Make Claude Code use the matching directory when it starts:

```sh
cd ~/Projects/acme-api
cclayer env                 # prints export CLAUDE_CONFIG_DIR=..., for direnv or your shell
eval "$(cclayer env)"
cclayer run -- claude       # or start directly with the variable set
```

`profile_dir` changes the default location. `leave` deletes the corresponding profile directory as well.

## 10. SessionStart hook: apply automatically at session start

Put this in the base layer's `claude/settings.json`; `hooks` must be in `settings_keys`:

```json
{
  "hooks": {
    "SessionStart": [{
      "matcher": "startup",
      "hooks": [{ "type": "command", "command": "command -v cclayer >/dev/null && cclayer apply --hook || true" }]
    }]
  }
}
```

With `auto_pull = true` in the device manifest, every Claude Code session starts with `apply --hook`; with `false` the hook does nothing. On a machine without cclayer the hook passes through.

## 11. Where the files are

| Path | What it is |
|---|---|
| `~/.config/cclayer/device.toml` | Device manifest, this machine only |
| `~/.local/share/cclayer/<layer>/` | Clone of a git layer |
| `~/.local/state/cclayer/` | Record of the last apply, lock |
| `~/.local/state/cclayer/backups/` | Backups of overwritten files, one directory per run; the newest five runs plus the very first are kept |
| `~/.gitconfig.cclayer` | The main git configuration generated by cclayer |
| `~/.gitconfig.cclayer.d/<layer>.gitconfig` | Each overlay's identity and fragment |
| The marked block at the end of `~/.gitconfig` | The only place cclayer changes |
| `~/.ssh/cclayer-<layer>`, the marked block at the top of `~/.ssh/config` | Key and ssh alias of the deploy key method |
| `~/.claude-profiles/<layer>/` | Config directories in profiles mode |

The environment variables `CCLAYER_DEVICE` and `CCLAYER_STATE` change the location of the manifest and the state directory. `CCLAYER_LANG` picks the message language (`en`, `zh`, `ja`); without it, `lang` in the device manifest decides, then the system language. Errors stay in English so they can be searched.

## 12. FAQ

**apply says a file is a conflict.** Both this machine and the layer changed it. An interactive run asks whether to overwrite the local file with the layer's version; to keep the local one answer no, then `capture` writes it back to the layer.

**apply refused a git fragment and mentions trust_exec.** The fragment sets a key like `core.hooksPath` that makes git run a program, or a key cclayer does not know. Confirm the layer is under your own control, then add the layer name to the device manifest's `trust_exec`.

**capture refused a file.** Look at the rule it reports. Replace secrets with `${VAR}` read from environment variables; put email addresses in the overlay's `[identity]`; team names do not belong in the base layer.

**git refuses to commit in a repository no overlay matches.** This is deliberate while `default_identity` is empty. Either add an overlay with a matching pattern for this repository, or set `default_identity`.

**status says `local directory`.** This layer is a directory layer and does not use git. For history and merging, push the directory to a repository as in section 5, then run `cclayer setup` and change the layer's location to the repository URL so cclayer clones it. cclayer never runs git inside a directory layer, even one with a `.git`: that `.git/config` was not written by cclayer, and settings in it can make git run programs.

**A cloud-synced directory changed on both sides at once.** The cloud drive's handling decides; cclayer does not merge. Use git if you need merging.

**Windows.** No Homebrew; download from Releases. On Windows `cclayer env` prints PowerShell syntax.

**A wrong answer in setup.** Pick that item again and fix it; nothing is written before you save. An overlay added by mistake in this run goes with "Remove this overlay" in the right column. To drop an overlay that was saved earlier, run `cclayer leave <layer>`.

**setup runs in a pipe or script.** Add `--accessible` for plain-text mode with one question per line; it switches automatically when standard input is not a terminal.
