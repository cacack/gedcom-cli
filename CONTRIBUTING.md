# Contributing

`gedcom` is a thin wrapper over the
[gedcom-go](https://github.com/cacack/gedcom-go) library. Read
[CONSTITUTION.md](CONSTITUTION.md) before proposing a command: it sets the
direction, the principles and what the tool will not do. Building, commits,
pull requests and releases are covered in README
["Development"](README.md#development).

## Where the code lives

- `cmd/gedcom/main.go` only calls `cli.Run`.
- Commands live in `internal/cli/`, each registered in `newRootCmd`
  (`root.go`), or on its parent command for a subcommand. A command usually
  has a file of its own, named after it, with its tests beside it in
  `<name>_test.go`. Commands that share most of their code may share a file
  (`merge.go` holds `merge-people` and `merge-families`), and a command group
  may keep its subcommands in one file or split them into one file each.
- Code shared by commands: `root.go` (exit codes, `usageError`, `--format`),
  `living.go` (`--redact-living`) and `write.go` (reading a file to write it
  back, and `-o`).

## No GEDCOM logic here

A command wraps library calls. A missing capability or a library bug is an
issue on [cacack/gedcom-go](https://github.com/cacack/gedcom-go/issues), not a
workaround in this repository.

The library is consumed as a released version in `go.mod`. To develop against
a local checkout, create a `go.work` in the repository root; it is gitignored:

```sh
go work init . ../gedcom-go   # the path to your gedcom-go checkout
```

Never commit a `replace` directive. A change that needs unreleased library
code waits for the library release, then bumps the version in `go.mod`.

## Adding a command

Every command keeps the contracts in README
["Conventions"](README.md#conventions). The table shows where each is
implemented and which test holds it.

| Rule | Use | Held by |
| --- | --- | --- |
| The help carries a summary and a runnable `Example`; coding agents learn the tool from `--help`. | `Short` and `Example` on the `cobra.Command` | `TestCommandsDocumented` (`cli_test.go`), for every command and subcommand |
| Results go to stdout and diagnostics to stderr; `--format json` works; the exit code is `ExitOK`, `ExitError` or `ExitUsage`. | `outputFormat`, and `usageError` for a mistake in the invocation (`root.go`) | The command's own tests, run through `Run` with the `run` helper (`cli_test.go`): text, JSON and exit codes |
| `--redact-living` is honored. | Decode through `openDocument` (`living.go`). A command that must see the whole file (`validate`), or that writes one, redacts its results with `hiddenRecords` instead. Only `extract` and `combine` redact the file they write (README "Conventions"). | `TestRedactLiving` (`living_test.go`): add the command to its table; the test fails for a command with no row. A new write command also goes in `TestRedactLivingWholeFiles`, or redacts its file and says so in the README |
| An input file is never written to; a command that changes data writes a new file. | `addOutputFlag`, `outputPath` and `writeOutput` (`write.go`) | The command's own tests |
| A write command keeps the file's layout: with no change requested, its output is byte-identical (CONSTITUTION.md C2). | Decode with `decodeForWrite`, encode with `encodeDocument` (`write.go`) | `TestRoundTripByteIdentical` (`roundtrip_test.go`): add the command to `writeCommands`. A command that always changes the file goes in `changingWriteCommands` instead, naming its own test that only the lines it means to change differ. `TestWriteCommandsListed` fails for a command with an `--output` flag in neither. |
| Test data is synthetic: no real people. | Fixtures in `internal/cli/testdata/` | Review |

Document the command in README "Usage" as well.

Run `make check` before opening a pull request: it runs what CI runs.
