# dvo — Azure DevOps CLI

A fast, native CLI tool for everyday Azure DevOps workflows: pull requests, work items, branch management, and more — built with Go.

---

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Building from Source](#building-from-source)
- [Shell Completion](#shell-completion)
- [Configuration](#configuration)
- [Commands](#commands)
  - [branch](#branch)
  - [pr](#pr)
  - [workitem](#workitem)
  - [repo](#repo)
  - [git](#git)
  - [vs](#vs)
  - [copilot](#copilot)
  - [commit-diff](#commit-diff)
  - [cache](#cache)
  - [config](#config)
  - [init](#init)
  - [version](#version)
- [Cancelling a command](#cancelling-a-command)
- [Dependencies](#dependencies)

---

## Prerequisites

| Tool | Purpose |
|------|---------|
| [Go 1.24+](https://go.dev/dl/) | Build toolchain |
| [Azure CLI](https://aka.ms/installazurecliwindows) | Azure DevOps API calls (`az devops`) |
| `azure-devops` CLI extension | `az extension add --name azure-devops` |
| [Git](https://git-scm.com/) | Repository detection and operations |
| [Make](https://www.gnu.org/software/make/) | Build automation (optional, can build directly) |

All commands must be run from within a Git repository whose remote points to Azure DevOps.

---

## Installation

### Quick install (Windows / Git Bash)

```bash
git clone https://github.com/Jonas-Marty/ad-cli
cd ad-cli
bash install.sh
```

The installer will:
1. Check for Go, Make, and Azure CLI
2. Build the binary for your platform
3. Add `dvo` to your PATH
4. Install bash and PowerShell completion scripts
5. Run `dvo init` to verify prerequisites

### Manual install

```bash
git clone https://github.com/Jonas-Marty/ad-cli
cd ad-cli
make build          # Unix / Git Bash
# or
make build-local    # Windows (produces dvo.exe)
```

Copy `bin/dvo` (or `bin/dvo.exe`) to any directory on your PATH.

---

## Building from Source

```bash
# Clone the repository
git clone https://github.com/Jonas-Marty/ad-cli
cd ad-cli

# Download dependencies
go mod download

# Build (Unix)
make build

# Build (Windows)
make build-local

# Format source code
make fmt

# Clean build artifacts
make clean
```

The `build` targets automatically:
- Inject the version and commit hash via `-ldflags`
- Run `dvo completion regen`, writing `bin/dvo-completion.bash` and
  `bin/dvo-completion.ps1` stamped with `# dvo-version: <version>`, and leaving
  a file untouched when its contents already match

### Build targets

| Target | Description |
|--------|-------------|
| `make build` | Build for Unix, regenerate completion files |
| `make build-local` | Build `dvo.exe` for Windows, regenerate completion files |
| `make fmt` | Run `go fmt ./...` |
| `make clean` | Remove the `bin/` directory |

---

## Shell Completion

### Bash (Git Bash / WSL)

Add to your `~/.bashrc`:

```bash
source /path/to/ad-cli/bin/dvo-completion.bash
```

Or let the installer handle it automatically via `bash install.sh`.

### PowerShell

Add to your PowerShell profile (`$PROFILE`):

```powershell
. /path/to/ad-cli/bin/dvo-completion.ps1
```

Or let the installer handle it automatically.

### Keeping completions current

`dvo init` installs a check in both shell profiles that compares the
`# dvo-version:` stamp at the top of the completion file against
`dvo version --short`. They only regenerate when the two differ, so a shell
that is already current starts with no extra work. To force it by hand:

```bash
dvo completion regen           # writes next to the dvo binary
dvo completion regen --dir bin # or into a specific directory
```

---

## Configuration

`dvo` stores persistent settings in `~/.config/dvo/config.json`.

```bash
# Get a config value
dvo config get repo-root

# Set a config value
dvo config set repo-root /d/Git
```

| Key | Description |
|-----|-------------|
| `repo-root` | Default root directory for repository and solution searches |

---

## Commands

### branch

Manage local branches. Alias: `b`

```
dvo branch <subcommand>
dvo b <subcommand>
```

#### branch create

Create a branch from a work item. Alias: `c`

```bash
dvo branch create -w <work-item-id> [commitish]
dvo b c -w 12345
dvo b c -w 12345 main        # branch from 'main'
```

Fetches the work item title and type from Azure DevOps and creates a branch following the convention:

| Work Item Type | Branch Prefix |
|----------------|---------------|
| Bug | `fix/` |
| Feature | `feat/` |
| Task | `task/` |

Example result: `fix/login-page-crashes-on-submit-12345`

| Flag | Description |
|------|-------------|
| `-w, --work-item <id>` | Work item ID (required) |
| `[commitish]` | Branch, tag, or commit to create from (default: current HEAD) |

#### branch cleanup

Interactively delete stale local branches. Alias: `cu`

```bash
dvo branch cleanup
dvo branch cleanup --offline   # skip PR API check
```

Lists local branches not present on the remote, annotated with their merge status:
- **merged** — every commit is reachable from `HEAD`; deleted with `git branch -d`
- **PR merged** — squash-merged via a pull request, and the local tip still matches
  the commit that PR actually merged
- **PR merged — local commits on top!** — a PR merged this branch, but it has commits
  added since; deleting it would lose them
- **PR merged — could not verify tip** — a PR merged this branch, but the merged commit
  is no longer available locally, so the tip cannot be compared
- **unmerged** — no evidence of merging

Presents an interactive picker to select which branches to delete. Only **merged** and
**PR merged** are pre-checked; the rest must be selected deliberately and are then
force-deleted.

Press <kbd>p</kbd> to preview the highlighted branch. The overlay lists the commits
deleting it would discard — for a branch committed to after its PR completed, that is
exactly the commits added since the merge:

```
╭──────────────────────────────────────────────────────────╮
│ fix/fix-wrong-key-type-16324                             │
│ (PR merged — local commits on top!)                      │
│                                                          │
│ 1 commit(s) added after the PR merged bf3777b:           │
│                                                          │
│   65d6524  2026-09-17  Update readme for 1.0.1           │
╰──────────────────────────────────────────────────────────╯
```

Picker keys: <kbd>↑</kbd>/<kbd>↓</kbd> move, <kbd>space</kbd> toggle, <kbd>a</kbd>
all/none, <kbd>p</kbd> preview, <kbd>enter</kbd> confirm, <kbd>esc</kbd> cancel.

The PR check compares commits, not branch names — a branch keeps its name after its PR
completes, so work pushed afterwards is flagged rather than silently deleted.

| Flag | Description |
|------|-------------|
| `--offline` | Skip the Azure DevOps PR API check |

#### branch push

Push the current branch and set the upstream tracking reference. Alias: `p`

```bash
dvo branch push
dvo b p
```

Equivalent to `git push -u <remote> <current-branch>`.

---

### pr

Manage pull requests.

```
dvo pr <subcommand>
```

#### pr create

Create a pull request for the current branch. Alias: `c`

```bash
dvo pr create "My PR title"
dvo pr create "Fix login bug" -t develop -r john.doe@company.com -w 12345
```

| Flag | Description |
|------|-------------|
| `-t, --target <branch>` | Target branch (default: repository default branch) |
| `-r, --reviewer <email>` | Required reviewer — repeatable, supports tab-completion |
| `-o, --optional-reviewer <email>` | Optional reviewer — repeatable |
| `-w, --work-item <id>` | Link a work item — repeatable; auto-detected from branch name |
| `--no-browser` | Do not open the PR in the browser after creation |
| `-v, --verbose` | Print resolved context before executing |

Work items are automatically detected from trailing numeric segments in the branch name (e.g., `fix/my-bug-12345` → work item `12345`).

#### pr open

Open the active pull request for the current branch in your browser. Alias: `o`

```bash
dvo pr open
```

#### pr update-description

Update the PR description with the commit messages since the branch diverged. Alias: `u`

```bash
dvo pr update-description
dvo pr update-description --yes   # skip confirmation
```

Collects all commits since the branch diverged from the target branch, formats them as a bullet list, and updates the PR description. Displays a preview before applying unless `--yes` is passed.

| Flag | Description |
|------|-------------|
| `-y, --yes` | Skip confirmation prompt |

---

### workitem

Manage work items. Aliases: `wi`, `w`

```
dvo workitem <subcommand>
```

#### workitem show

Open a work item in the browser. Alias: `s`

```bash
dvo workitem show 12345
dvo wi s 12345
```

#### workitem link

Copy a formatted HTML link for a work item to the clipboard. Alias: `l`

```bash
dvo workitem link 12345
dvo wi l 12345
```

Generates an HTML anchor tag (`<a href="...">`) and copies both HTML and plain text representations to the clipboard. Useful for pasting into Outlook, Word, or Teams.

#### workitem activity

List the work items you potentially worked on during one day, for your hour report. Alias: `a`

```bash
dvo workitem activity                                  # today, project of the current repo
dvo w a yesterday                                      # short form: `w` = workitem, `a` = activity
dvo workitem activity 2026-10-20 -o hours.csv          # a past day, also written to CSV
dvo workitem activity -p Alpha -p Beta --repo /d/Git/alpha --repo /d/Git/beta
```

The result is a deliberate **superset**: better too many items than too few, so skip the irrelevant ones yourself. Every row shows why the item was included and the first and last time of activity (local time). The day runs from local midnight to the next local midnight, DST included.

In a terminal the result is an interactive list: one block per item with its headline, the time span, the parent and one line per reason. Nothing is selected at first.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `j`/`k`, `PgUp`/`PgDn`, `g`/`G` | Move |
| `space` | Select / deselect the item |
| `a` | Select all / none |
| `c` | Copy the selected IDs to the clipboard as `#123;#223;#323` |
| `enter` | Copy and quit |
| `q` / `esc` | Quit |

When output is piped, or with `--plain`, a plain table is printed instead and the CSV is the same either way. Columns: `Id`, `Type`, `Title`, `State`, `Project`, `Parent` (`id title`), `First`, `Last`, `Reasons`. Rows with timestamps come first, ordered by `First`, then the rest by `Id`.

Signals (each adds items together with a reason):

| Signal | Reason shown |
|--------|--------------|
| Revisions you made: field edits, comments, state changes, links, attachments | `edited 3x: created, state New→Active, comment, remaining work` (what each edit changed, by comparing it with the previous revision; bookkeeping fields are ignored) |
| "My activity" list (items you viewed or edited; the service keeps only the latest activity per item, so run it the same day) | `my activity: visited` |
| Pull requests you created, completed, abandoned, commented on or voted on: linked work items, IDs in the title, numbers in the source branch | `PR !123 commented/voted` |
| Pushes you made: numbers in branch names, work items of the pushed branch's PRs, IDs in commit messages | `pushed branch ...`, `commit message in push to ...` |
| Local repos (`--repo`): your commits (also unpushed ones) and branches you checked out (from the reflog) | `local commit message (...)`, `checked out ...` |
| Items assigned to you that are still open, only when nothing else found them | `assigned to you, open (no activity found)` |

IDs are read from `#123` / `AB#123` in texts and from 3 to 7 digit numbers in branch names. Numbers that are not work items are dropped, and their count is printed. A source that fails (a repo you cannot read, the PR threads, My activity) prints a warning and the run continues.

The command calls the Azure DevOps REST API directly. It signs in with an `az` token for the tenant the organization accepts: the tenant the organization announces, or otherwise the first tenant you are logged into that it accepts (the result is remembered in `~/.config/dvo/cache/<org>/tenant.json` and re-detected if the organization stops accepting it; pass `--tenant` to override). If no token is accepted it falls back to the PAT stored by `az devops login` (or `AZURE_DEVOPS_EXT_PAT`). A PAT needs the Work Items, Code and Identity read scopes.

| Flag | Description |
|------|-------------|
| `--org <name or url>` | Organization (default: from the current repo) |
| `--tenant <id>` | Entra tenant ID to sign in with (default: discovered, see above) |
| `-p, --project <name>` | Project to search, repeatable (default: from the current repo) |
| `[date]`, `-d, --date <date>` | `YYYY-MM-DD`, `today` or `yesterday`, as argument or flag (default: today) |
| `-r, --repo <path>` | Local git repository to scan, repeatable. Git Bash paths like `/d/Git` work |
| `--author <name or email>` | Git author for local repos (default: your Azure DevOps e-mail) |
| `--closed-states <list>` | States that do not count as open for the assigned signal (default: `New,Proposed,To Do,Closed,Done,Removed,Resolved`) |
| `-o, --output <file>` | Also write a CSV (UTF-8 with BOM, opens correctly in Excel) |
| `--plain` | Print a plain table instead of the interactive list |
| `--separator <char>` | CSV separator, `tab` for a tab (default: `;`) |

---

### repo

Repository-level utilities.

```
dvo repo <subcommand>
```

#### repo open

Open the current repository in Azure DevOps. Alias: `o`

```bash
dvo repo open
dvo repo o
```

Extracts the organisation, project, and repository from the Git `origin` remote and opens the corresponding Azure DevOps page in the browser.

---

### git

Git-related utilities.

```
dvo git <subcommand>
```

#### git auth

Show the current authentication protocol for the `origin` remote.

```bash
dvo git auth
```

Outputs whether the remote uses HTTPS or SSH.

#### git auth switch

Switch the `origin` remote between HTTPS and SSH.

```bash
dvo git auth switch            # interactive
dvo git auth switch --to-ssh
dvo git auth switch --to-https
```

| Flag | Description |
|------|-------------|
| `--to-ssh` | Switch to SSH (no-op if already SSH) |
| `--to-https` | Switch to HTTPS (no-op if already HTTPS) |

Converts between:
- HTTPS: `https://dev.azure.com/<org>/<project>/_git/<repo>`
- SSH: `git@ssh.dev.azure.com:v3/<org>/<project>/<repo>`

---

### vs

Open solutions or files in Visual Studio or VS Code.

```
dvo vs <subcommand>
```

#### vs open

Find and open `.sln` / `.slnx` solution files, or search for a file and open its repository. Alias: `o`

```bash
dvo vs open                          # find solutions in current directory
dvo vs open --path /d/Git            # search in a specific directory
dvo vs open --file MyController.cs   # find file, open its repository
dvo vs open --code                   # open in VS Code instead
dvo vs open --2022                   # force Visual Studio 2022 Professional
```

| Flag | Description |
|------|-------------|
| `--2022` | Use Visual Studio 2022 Professional |
| `-i, --insiders` | Use Visual Studio Insiders |
| `-c, --code` | Open in VS Code instead of Visual Studio |
| `-p, --path <dir>` | Directory to search for solutions |
| `-f, --file <name>` | File name to search for |
| `-v, --verbose` | Print the underlying search commands |

When multiple solutions are found, an interactive picker is displayed. `.slnx` files take precedence over their `.sln` counterparts.

---

### copilot

Launch GitHub Copilot CLI pre-loaded with Azure DevOps work item context.

```bash
dvo copilot                    # auto-detect work item from branch name
dvo copilot -w 12345           # override work item
dvo copilot --allow-all        # auto-approve all tool use
dvo copilot -m claude-sonnet-4.6
```

Fetches the work item's title, description, acceptance criteria, and repro steps from Azure DevOps, strips HTML formatting, and passes them as context to the Copilot CLI session.

| Flag | Description |
|------|-------------|
| `-w, --work-item <id>` | Work item ID (overrides auto-detection from branch name) |
| `-m, --model <name>` | Model to use (default: `claude-sonnet-4.6`) |
| `--allow-all` | Auto-approve all tool use prompts |
| `-p, --non-interactive` | Exit after completion |

#### copilot prompt

Print (and copy) the generated Copilot prompt without launching a session. Alias: `p`

```bash
dvo copilot prompt -w 12345
```

Outputs the prompt to stdout and copies it to the clipboard via `clip.exe`.

| Flag | Description |
|------|-------------|
| `-w, --work-item <id>` | Work item ID (required if not on a work-item branch) |

---

### commit-diff

Open the Azure DevOps diff viewer for two commits or branches.

```bash
dvo commit-diff <base>..<compare>
dvo commit-diff <base> <compare>
```

**Examples:**

```bash
dvo commit-diff main..develop
dvo commit-diff abc123 def456
dvo commit-diff HEAD~5 HEAD
```

Resolves both refs to full SHAs and opens the `branchCompare` page in Azure DevOps.

---

### cache

Manage local caches used for tab-completion.

```
dvo cache <subcommand>
```

#### cache refresh

Fetch and cache all Azure DevOps users. Alias: `r`

```bash
dvo cache refresh
```

Calls `az devops user list` (handling pagination) and saves results to `~/.config/dvo/cache/<org>/users.json`. This cache is used by `dvo pr create -r` for reviewer tab-completion.

---

### config

Manage persistent configuration values.

```bash
dvo config get <key>
dvo config set <key> <value>
```

**Examples:**

```bash
dvo config set repo-root /d/Git
dvo config get repo-root
```

---

### init

Set up shell integration and verify prerequisites.

```bash
dvo init
```

Safe to re-run — uses idempotency markers so no duplicate entries are added to shell profiles.

---

### version

Print the version and commit hash.

```bash
dvo version
# dvo 1.2.3 (abc1234)
```

---

## Cancelling a command

Press `Ctrl+C` at any point — while a spinner is running, or in a picker — to
abort. Cancelling is always safe:

- The command stops there. It never continues with whatever the interrupted
  step managed to collect, so no branch is created, no PR is opened or updated,
  and no branch is deleted on the strength of a half-finished lookup.
- The `az` or `git` process the spinner was waiting on is killed with it.
- `dvo` prints `Cancelled.` and exits with code `130`, the usual shell
  convention for "interrupted", so scripts can tell a cancellation from a
  failure (`1`).

---

## Dependencies

### Direct

| Package | Version | Purpose |
|---------|---------|---------|
| [spf13/cobra](https://github.com/spf13/cobra) | v1.10.2 | CLI framework |
| [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) | v1.3.10 | Terminal UI framework |
| [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) | v0.21.0 | TUI components (spinners, pickers) |
| [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) | v1.1.0 | Terminal styling and colours |

### External runtime dependencies

| Tool | Required by |
|------|-------------|
| `az` (Azure CLI) | All Azure DevOps API calls |
| `az devops` extension | All Azure DevOps API calls |
| `git` | Branch detection, commit operations |
| `gh` (GitHub CLI with Copilot) | `dvo copilot` |
| `clip.exe` | `dvo copilot prompt` (clipboard) |
