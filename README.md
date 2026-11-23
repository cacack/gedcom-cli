# gedcom-cli

`gedcom` is a command-line tool for inspecting, validating and correcting
GEDCOM genealogy files. It is a thin wrapper over
[gedcom-go](https://github.com/cacack/gedcom-go), built equally for use at a
terminal and by AI coding agents. See [CONSTITUTION.md](CONSTITUTION.md) for
what it is and is not trying to be.

**Status: early.** The commands so far are `info`, `get`, `list`,
`ancestors`, `descendants`, `relatives`, `media`, `validate`, `duplicates`, `extract`, `put`, `delete` and `convert`. Flags and JSON output may change until v1.0.

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

`relatives` prints a person and their immediate family, one per line: the
person (`self`), then parents, siblings (`half-sibling` for a child of a
parent's other family), spouses and children. Each line gives the
relationship, the family it comes through, the person in `list indi`'s fields,
and the child's pedigree (`PEDI`) in that family where the file gives one, so
birth and adoptive parents, or two marriages, show separately:

```sh
gedcom relatives family.ged I1
gedcom relatives --format json family.ged I1 | jq '.relatives[] | select(.relationship == "child")'
```

```text
self		@I1@	Ann /Example/	4 APR 1920	1 JAN 1990
parent	@F1@	@I2@	Bert /Example/	1890		birth
parent	@F2@	@I5@	Earl /Placeholder/			adopted
half-sibling	@F5@	@I8@	Hal /Example/	1930
spouse	@F3@	@I9@	Ivan /Sample/
child	@F3@	@I10@	Jill /Sample/	1945		birth
```

`pedigree` is always the child's end of the link: the person's own for a
parent, the relative's for a sibling or child.

`--format json` prints one object: `person`, with `list indi`'s keys, and
`relatives` (which does not repeat the person), each with those keys plus
`relationship`, `family` and `pedigree`.

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

`put` and `delete` correct a file. They never write to the input: `-o` names
the new file (required, and not an input), or `-o -` writes it to stdout.
Every line they were not asked to change is written exactly as it was read.
A refused write prints the report (with `--format json`, as JSON) and exits
`1`; a failure before there is a report (an unreadable file, an unknown xref)
prints only the error on stderr.

`put` takes a file of level-0 records, with no header, as `get` prints them
(`-` reads it from stdin). Each record replaces the one with the same xref, in
its place, or is appended when the xref is new. `get`, edit, `put` is the edit
loop:

```sh
gedcom get family.ged @I1@ > i1.ged
$EDITOR i1.ged
gedcom put family.ged i1.ged -o family-edited.ged
```

```text
wrote family-edited.ged: 1 replaced, 0 added, 0 unchanged
  replaced	@I1@
```

A record put back unedited is left as it was, so the output is byte-identical.
A replaced record is written as its new lines, without the old record's
indentation or the blank lines before it. What the decoder could not read
cleanly in the records file is reported as `validate` reports it; when any of
it is an error, nothing is written and the exit code is `1`. Pointers are not
checked: a put record may point at an xref the file lacks, or replace a record
with one of another type, so run `validate` on the output.

`delete` removes records by xref. While other records still point at them, it
writes nothing, lists each pointer (the record holding it, the tag path to it,
the record it points at; `-` is the header, `"from": ""` in JSON) and exits
`1`. `--force` deletes anyway and reports the pointers as dangling:

```sh
gedcom delete family.ged @I2@ -o family-edited.ged
```

```text
family-edited.ged not written: pointers left to @I2@
references (1)
  @F1@	FAM.HUSB	@I2@
```

An xref the file defines more than once is refused, since `delete` could not
tell those records apart.

`convert` rewrites a file as GEDCOM 5.5.1 or 7.0 with gedcom-go's converter,
to a new file named by `-o`, always in UTF-8. It brings a legacy 5.5 ANSEL
file up to the version and encoding of the rest of a working set:

```sh
gedcom convert legacy.ged --to 5.5.1 -o legacy-utf8.ged
gedcom convert --fail-on-loss legacy.ged --to 7.0 -o legacy-v7.ged
gedcom convert --format json legacy.ged --to 5.5.1 -o out.ged | jq .dataLoss
```

```text
legacy.ged: 7.0 -> 5.5.1, wrote out.ged
data loss (1)
  EXID tags	Tag not supported in GEDCOM 5.5.1	@I1@
dropped (1)
  Individual @I1@ > EXID	EXID		Tag not supported in GEDCOM 5.5.1
transformations (2)
  ...
```

The report is the library's conversion report: what the target version cannot
hold (`data loss`, with each `dropped` value), what was approximated,
normalized or preserved, each kind of transformation with a count, and
validation issues in the result. A conversion that loses data still writes
the output and exits `0`; with `--fail-on-loss` it writes nothing and exits
`1`, with the report on stdout. Lines the conversion leaves alone keep their
layout, so converting a UTF-8 file to the version it already has writes it
back byte-identical. 5.5 is not a target, since 5.5 does not allow UTF-8.
Text from an ANSEL file is written decomposed (a letter followed by a
combining accent), as gedcom-go decodes it.

Run `gedcom --help` or `gedcom <command> --help` for every command and flag.

## Conventions

These hold for every command, so scripts and agents can rely on them:

`ancestors`, `descendants`, `relatives`, `media`, `validate`, `duplicates`, `extract`, `put` and `delete`. Flags and JSON output may change until v1.0.

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
