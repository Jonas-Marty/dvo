# AGENTS.md

Guide for coding agents working in this repository. For the user-facing
command reference, see [README.md](README.md).

## What this is

`dvo` is a Go CLI for everyday Azure DevOps work: pull requests, work items,
branches, and repo navigation. It shells out to `az` (Azure CLI with the
`azure-devops` extension) and `git`, parses their JSON or text output, and wraps
them in a Bubble Tea TUI (spinners, pickers, editors).

The one exception is `workitem activity`, which needs hundreds of small REST
calls per run. `devops.Client` (`internal/devops/client.go`) takes a single
token from `az account get-access-token` and calls the REST API over HTTP with
`net/http`. Use it only for bulk or concurrent calls; everything else keeps
shelling out to `az`.

- Module: `github.com/Jonas-Marty/ad-cli`, Go 1.24+
- Binary: `dvo` (`dvo.exe` on Windows)
- Primary platform: Windows, with Git Bash and PowerShell. Keep Unix working too.

## Build, test, verify

```bash
go build ./...          # quick compile check
go vet ./...
go test ./...           # cmd tests create real temp git repos and take about 25s
make fmt                # go fmt ./...
make build-local        # Windows: bin/dvo.exe and regenerated completion files
make build              # Unix:    bin/dvo and regenerated completion files
```

`bin/` is gitignored. The `make` targets inject `Version` and `Commit` into
`cmd` through `-ldflags`. A plain `go build` reports `dev`.

Before you finish a change, run `go build ./... && go vet ./... && go test ./...`.
Most commands need a real Azure DevOps remote and a logged-in `az`, so you
usually can't exercise them end to end here. Say so instead of claiming you
tested them.

## Layout

```
main.go                  os.Exit(cmd.Execute())
cmd/                     one cobra command per file (package cmd)
  root.go                rootCmd, version, Execute() and exit-code mapping
  <group>.go             parent command (pr.go, branch.go, workitem.go, ...)
  <group>_<sub>.go       subcommand (pr_create.go, branch_cleanup.go, ...)
  completion.go          custom completion command and `completion regen`
  init.go                `dvo init`: shell profile snippets, prerequisite checks
internal/devops/         Context{Org,Project,Repo} parsed from the origin remote; URL builders; az queries;
                         Client (direct REST with an az-issued token, retries, tests via httptest)
internal/workday/        `workitem activity`: signal sources -> Collector -> Report (table, CSV)
internal/git/            thin wrappers over git commands
internal/config/         ~/.config/dvo/config.json (Load/Save, KnownKeys, NormalizePath)
internal/cache/          ~/.config/dvo/cache/<org>/users.json (reviewer completion)
pkg/ui/                  styled printers, OpenBrowser, RunSpinner(Progress), RunStreaming, PickOne,
                         MultiSelect(WithPreview), text editor
```

## Conventions

### Adding a command

1. Create `cmd/<group>_<name>.go` with a `var xxxCmd = &cobra.Command{...}`
   that sets `Use`, `Aliases` (short one-letter or two-letter forms are the
   norm), `Short`, `Long`, `Example`, and `RunE`.
2. Register it in that file's `init()` with `parentCmd.AddCommand(xxxCmd)`.
   A new group gets its own `cmd/<group>.go` that adds itself to `rootCmd`.
3. Put the logic in a named `runXxx(cmd *cobra.Command, args []string) error`
   function. Return errors; don't print them and call `os.Exit`.
4. Add shell completion where it helps (`ValidArgsFunction`,
   `RegisterFlagCompletionFunc`). See `pr_create.go` and `config.go`.
5. Document the command in `README.md`: the table of contents, a section with
   examples, and a flag table. The README is the user documentation, so keep it
   in sync.

### Errors, cancellation, exit codes

This was reworked recently. Don't regress it.

- `Execute()` in `cmd/root.go` maps errors to exit codes: `nil` gives 0,
  `ui.ErrCancelled` or `context.Canceled` gives 130 with `Cancelled.`, and
  anything else gives 1 with `Error: ...`. Cobra's own error and usage
  printing is turned off.
- Thread `cmd.Context()` through every long-running step. Use
  `exec.CommandContext` so that Ctrl+C kills the `az` or `git` child.
- Wrap blocking `az` calls in `ui.RunSpinner(cmd.Context(), label, func(c context.Context) error {...})`
  and use `c` inside it. Use `ui.RunStreaming` for commands with progress output.
- If any helper returns an error, including `ui.ErrCancelled`, return it right
  away. Never continue with partial results after a cancellation. A test in
  `pkg/ui/ui_test.go` exists because an interrupted fetch once looked like a
  success.
- Don't install a SIGINT handler. `root.go` explains why.
- When `az` fails, include its stderr in the error. See `azJSON` in
  `cmd/pr_open.go` and `CompletedPRMergeTips` in `internal/devops`.

Some older helpers in `internal/git` still use `exec.Command` without a
context. Give a helper a `ctx` parameter when you touch it on a cancellable path.

### Output

Print through the styled printers in `pkg/ui` (`ui.Info`, `ui.Success`,
`ui.Warning`, `ui.Error`, ...) rather than calling `fmt.Println` directly.
Success lines start with `✓`.

### Azure DevOps context

Get org, project, and repo from `devops.FromCurrentRepo()`. It accepts both
`https://[user@]dev.azure.com/<org>/<project>/_git/<repo>` and
`git@ssh.dev.azure.com:v3/<org>/<project>/<repo>`, and percent-decodes the
segments. Build web URLs with the `Context` methods (`RepoURL`, `PRURL`,
`WorkItemURL`, `CommitDiffURL`) instead of formatting them by hand. Pass
`--org ctx.OrgURL() --project ctx.Project` to `az`.

### Windows quirks

- Users pass Git Bash paths such as `/d/Git`. Run them through
  `config.NormalizePath` before using them.
- Don't use temp files to pass text to external tools. Pass inline arguments
  (see the recent `update-description` change).
- `OpenBrowser` uses `cmd /c start` on Windows.

### Completion files

`dvo completion regen` writes `bin/dvo-completion.{bash,ps1}` with a
`# dvo-version:` stamp that the snippets from `dvo init` compare against
`dvo version --short`. Rewrite these files only when their contents change.
Keep `dvo version --short` stable because shells depend on it.

### Tests

- Use table-driven tests. `cmd/branch_cleanup_test.go` builds throwaway git
  repos in `t.TempDir()` with `runGit` and `commit` helpers. Reuse that pattern
  for git logic.
- Tests must not call `az` or the network. Split pure logic, such as parsing
  and classification, out of the `az` call and test that part.
- Test Bubble Tea models by sending messages to `Update` directly, as in
  `pkg/ui/ui_test.go`.

### Style

- Run `gofmt`. Wrap comments at about 80 columns.
- Comments explain *why*, especially for behavior that is safety-relevant,
  such as branch deletion and cancellation. Match the existing density.
- Use `cmd/branch_cleanup.go` as the model for destructive operations. Prefer
  safe defaults, pre-check only what is provably safe, and make the user
  confirm force-deletes deliberately.
- Commit messages are mostly Conventional Commits (`feat: ...`, `fix: ...`).
