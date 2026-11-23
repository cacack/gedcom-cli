# CLAUDE.md

`gedcom` CLI: a thin wrapper over the `github.com/cacack/gedcom-go/v3` library, for people and coding agents alike. Direction, principles and non-goals are in [CONSTITUTION.md](CONSTITUTION.md); read it before proposing a command.

- **No GEDCOM logic here.** Commands wrap library calls. A missing capability or a library bug is an issue on `cacack/gedcom-go`, not a workaround in this repo.
- **The library is consumed as a released version** in `go.mod`. To develop against a local checkout, use a `go.work` (gitignored); never commit a `replace`.
- **Layout:** `cmd/gedcom/main.go` only calls `cli.Run`. Each command is one file in `internal/cli/`, registered in `newRootCmd`.
- **Output contract** (README "Conventions"): results on stdout, diagnostics on stderr, `--format json` on every command, exit codes `ExitOK`/`ExitError`/`ExitUsage`. Return `usageError` for invocation mistakes.
- **Every command needs** an `Example` in its help (agents learn the tool from `--help`) and tests through `Run` covering text, JSON and exit codes.
- **Never write to an input file.** Commands that change data write a new file.
- Test data must be synthetic: no real people.

**Releases:** release-please cuts them from conventional commits on `main`; only `feat`, `fix` and `perf` produce one (README "Development"). Never tag or edit `CHANGELOG.md` by hand.

`make check` runs what CI runs (vet, race tests, gofmt).
