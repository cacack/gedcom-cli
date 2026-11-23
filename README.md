# gedcom-cli

`gedcom` is a command-line tool for inspecting, validating and correcting
GEDCOM genealogy files. It is a thin wrapper over
[gedcom-go](https://github.com/cacack/gedcom-go), built equally for use at a
terminal and by AI coding agents. See [CONSTITUTION.md](CONSTITUTION.md) for
what it is and is not trying to be.

**Status: early.** The commands so far are `info`, `get`, `list`,
`ancestors`, `descendants`, `media`, `validate` and `duplicates`. Flags and JSON output may change until v1.0.

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
which may change with the library. Its keys are the library's field names
(`XRef`, `Names`), not the lowercase keys of `list` and `validate`.

`list` prints one line per record of a type (`indi`, `fam`, `sour`, `repo` or
`obje`), tab-separated and in file order; `--limit N` stops after N records:

```sh
gedcom list family.ged indi
gedcom list --limit 20 family.ged fam
```

```text
@I1@	Ada /Example/	3 MAR 1901	14 NOV 1978	false
@I2@	Ben /Example/	21 JUL 1999		true
```

The last `indi` field, `living`, is whether gedcom-go presumes the person
living (see [Conventions](#conventions)). To leave the living out:

```sh
gedcom list --format json family.ged indi | jq '.[] | select(.living | not)'
```

With `--format json` each record is a flat object, so `jq` does the searching.
To find people by surname:

```sh
gedcom list --format json family.ged indi | jq '.[] | select(.name | test("/Example/"))'
```

`gedcom list --help` names the fields of each type. One known gap: for `obje`,
a record that puts `TITL` and `FORM` beside `FILE` instead of under it (the
GEDCOM 5.5 layout) lists its title and format empty; `get` shows the whole
record.

`ancestors` and `descendants` walk one person's line by generation (1 is
parents or children, 2 grandparents or grandchildren), one row per relative in
`list indi`'s fields with the generation first. `--generations N` stops after
generation N; 0, the default, walks the whole line:

```sh
gedcom ancestors family.ged @I1@
gedcom descendants --generations 2 family.ged I1
gedcom ancestors --format json family.ged @I1@ | jq '.[] | select(.generation == 3)'
```

```text
1	@I2@	Bert /Example/	1920	1990
1	@I3@	Cleo /Sample/
2	@I4@	Dan /Example/
```

Rows are in generation order, then in gedcom-go's order. A person reached by
more than one path (pedigree collapse) is listed once, at the generation of the
shortest path.

`validate` reports what the decoder could not read cleanly (`decode`) and what
gedcom-go's validator finds in the records (`validate`), grouped by severity
with a count per group. Each line gives where the finding is, its code, its
message and its source, tab-separated:

```sh
gedcom validate family.ged
gedcom validate --min-severity error family.ged
gedcom validate --format json family.ged | jq .counts
```

```text
error (2)
  line 13	INVALID_LEVEL	invalid level number (context: "this line is not GEDCOM")	decode
  @I1@ line 18	ORPHANED_FAMS	FAMS reference to non-existent family @F9@ (related: @F9@)	validate
warning (1)
  @I2@ line 19	MISSING_NAME	Individual record has no NAME	validate
```

A file with nothing to report prints `family.ged: no findings`.
`--min-severity error|warning|info` (default `warning`) limits the findings
shown and counted. It does not change the exit code, which is `1` whenever the
file has an error-severity finding, with the report still on stdout and a
one-line summary on stderr. The JSON report names the filter in `minSeverity`.

Findings are gedcom-go's, as reported. The decoder and the validator both
report a duplicated xref, so `DUPLICATE_XREF` appears once per source.
Duplicate detection (`POTENTIAL_DUPLICATE`) is slow on large files and runs
only with `--min-severity info`.

`duplicates` reports people in one file who may be the same as people in
another, for reconciling overlapping trees. Each line is a candidate pair: the
score (0 to 1, from gedcom-go's duplicate detection), then xref, name, birth
and death from the first file, then the same from the second. Pairs are
candidates for review, not conclusions; nothing is merged or written:

```sh
gedcom duplicates old-export.ged family.ged
gedcom duplicates --min-score 0.9 old-export.ged family.ged
gedcom duplicates --format json old-export.ged family.ged | jq '.[] | [.a.xref, .b.xref]'
```

```text
0.90	@I1@	Ada /Example/	3 MAR 1901	14 NOV 1978	@I7@	Ada /Example/	1901
0.70	@I2@	Ben /Example/	21 JUL 1899		@I1@	Ben /Example/
```

Xrefs are each file's own, so `@I1@` can be a different person on each side.
In JSON each pair is `{"score", "a", "b"}`, with `a` and `b` in `list indi`'s
keys. `--min-score` (default `0.7`) drops pairs whose printed score is lower. When the library
skipped or shortened comparisons (very large surname groups, very long given
names) a warning on stderr says so, and some pairs may be missing.

`media check` reports whether each `FILE` reference resolves to a file under a
media folder: `ok`, `missing`, `url`, `absolute` (a rooted path) or `outside`
(a relative path that climbs out of the folder); the last two are not
resolved. It exits `1` when any reference is missing. `--unreferenced` also
lists the files in the folder that nothing references. The media files are
only looked at, never changed:

```sh
gedcom media check --root media family.ged
gedcom media check --root media --unreferenced --format json family.ged
```

```text
ok	@O1@	line 812	photos/ada.jpg
missing	@O2@	line 815	photos/ben.jpg
url	@I4@	line 97	https://example.com/census.jpg
unreferenced			photos/cara.jpg
```

`media add` links a file from an individual, family or source, reusing the
`OBJE` record with that path or creating one, and writes the result to a new
file. Running it again changes nothing. `--root` checks that the path names a
file in the media folder. `--batch` takes a tab-separated file of rows
(xref, path, optional title) and applies all of them or, if any row fails,
none:

```sh
gedcom media add family.ged --to @I1@ --path photos/ada.jpg --title "Ada, 1920" -o out.ged
gedcom media add family.ged --root media --batch links.tsv -o out.ged
```

Run `gedcom --help` or `gedcom <command> --help` for every command and flag.

## Conventions

These hold for every command, so scripts and agents can rely on them:

- **Results go to stdout, diagnostics to stderr.** Stderr is empty on
  success, except for a warning that a result is incomplete (`duplicates`).
- **`--format json`** gives machine-readable output; the default is text.
- **Exit codes:** `0` success, `1` the command failed (unreadable file, decode
  error) or `validate` found errors, `2` the command line was wrong.
- **Input files are never modified.**
- **`--redact-living`** replaces what the output says about a person presumed
  living with `Living`: name, dates, places, notes, addresses, and in `get`
  their raw lines. The xref, sex and family links stay, so the structure is
  still readable. A family with a living spouse keeps only its spouses and
  children, and a note record a hidden person or family points to reads
  `Living`. `validate` still checks the whole file; a finding about a hidden
  record reads `Living` instead of its message. Source, media, repository and
  submitter records are not redacted, even when a hidden record cites them:
  check their titles, file names and addresses yourself. Use it before pasting
  output into an issue, a forum post or a chat.
- **Who is presumed living** is gedcom-go's rule: no death, burial or
  cremation, and no date of the person's, of a family they are a spouse in or
  of their children, more than `--living-years` (default 110) before
  `--reference-year` (default `0`, the current year). A person with no dates
  at all is presumed living. Fix `--reference-year` for reproducible output.

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
