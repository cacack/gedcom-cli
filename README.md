# gedcom-cli

`gedcom` is a command-line tool for inspecting, validating and correcting
GEDCOM genealogy files. It is a thin wrapper over
[gedcom-go](https://github.com/cacack/gedcom-go), built equally for use at a
terminal and by AI coding agents. See [CONSTITUTION.md](CONSTITUTION.md) for
what it is and is not trying to be.

**Status: early.** Only `info` exists so far. Flags and JSON output may change
until v1.0.

## Install

**Prebuilt binary.** Download the archive for your platform (linux, darwin or
windows; amd64 or arm64) from the
[latest release](https://github.com/cacack/gedcom-cli/releases/latest), check
it against `checksums.txt`, and put `gedcom` on your `PATH`:

```sh
sha256sum --check --ignore-missing checksums.txt
tar -xzf gedcom_*_linux_amd64.tar.gz gedcom
sudo mv gedcom /usr/local/bin/
```

**With Go** (1.25+):

```sh
go install github.com/cacack/gedcom-cli/cmd/gedcom@latest
```

`gedcom --version` reports the release either way.

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

`get` prints single records by xref, in the order given, as the GEDCOM lines of
each record:

```sh
gedcom get family.ged @I1@
gedcom get family.ged I1 F1          # the @ delimiters are optional
gedcom get --format json family.ged @I1@
```

`get --format json` prints an array of records in gedcom-go's entity shape,
which may change with the library.

`list` prints one line per record of a type (`indi`, `fam`, `sour`, `repo` or
`obje`), tab-separated and in file order; `--limit N` stops after N records:

```sh
gedcom list family.ged indi
gedcom list --limit 20 family.ged fam
```

```text
@I1@	Ada /Example/	3 MAR 1901	14 NOV 1978
@I2@	Ben /Example/	21 JUL 1899
```

With `--format json` each record is a flat object, so `jq` does the searching.
To find people by surname:

```sh
gedcom list --format json family.ged indi | jq '.[] | select(.name | test("/Example/"))'
```

`gedcom list --help` names the fields of each type.

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

Commits follow [Conventional Commits](https://www.conventionalcommits.org/).
Releases are cut by release-please from the commits on `main`: `feat` and `fix`
(including Dependabot's `fix(deps)`) and `perf` open or update a release PR,
and merging it tags the release and publishes the binaries. Other types, such
as `docs`, `ci` and `chore`, do not release on their own. Until v1.0 a breaking
change bumps the minor version.

Link issues from the pull request description (`Closes #N`, or `Refs #N` for
partial work), not from commit messages. release-please writes any issue a
commit mentions into the release PR as "closes", so the release would close an
issue that was only referenced.

Write pull request titles as plain prose (`Add a validate command`), not as
conventional commits. A merge commit carries the PR title as its body, and
release-please would count a conventional title as a second copy of the
change. CI checks the title; Dependabot's titles are rewritten automatically.

## License

[MIT](LICENSE)
