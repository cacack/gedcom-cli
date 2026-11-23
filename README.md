# gedcom-cli

`gedcom` is a command-line tool for inspecting, validating and correcting
GEDCOM genealogy files. It is a thin wrapper over
[gedcom-go](https://github.com/cacack/gedcom-go) and is designed to be driven
by AI coding agents as well as by hand.

**Status: early.** Only `info` exists so far. Flags and JSON output may change
until v1.0.

## Install

```sh
go install github.com/cacack/gedcom-cli/cmd/gedcom@latest
```

Requires Go 1.25+.

## Usage

```sh
gedcom info family.ged
gedcom info --format json family.ged
```

```text
File:          family.ged
Version:       5.5.1
Encoding:      UTF-8
Source system: Ancestry.com Family Trees
Records:       1272
  FAM   184
  INDI  629
  ...
```

Run `gedcom --help` or `gedcom <command> --help` for every command and flag.

## Conventions

These hold for every command, so scripts and agents can rely on them:

- **Results go to stdout, diagnostics to stderr.** Stderr is empty on success.
- **`--format json`** gives machine-readable output; the default is text.
- **Exit codes:** `0` success, `1` the command failed (unreadable file, decode
  error), `2` the command line was wrong.
- **Input files are never modified.**

## Development

```sh
make check   # vet, test, gofmt: what CI runs
make build   # ./bin/gedcom
```

## License

[MIT](LICENSE)
