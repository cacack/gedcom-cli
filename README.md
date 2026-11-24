# gedcom-cli

`gedcom` is a command-line tool for inspecting, validating and correcting
GEDCOM genealogy files. It is a thin wrapper over
[gedcom-go](https://github.com/cacack/gedcom-go), built equally for use at a
terminal and by AI coding agents. See [CONSTITUTION.md](CONSTITUTION.md) for
what it is and is not trying to be.

**Status: early.** The commands so far are `info`, `get`, `list`,
`ancestors`, `descendants`, `relatives`, `media`, `validate`, `duplicates`, `extract`, `put`, `delete`, `convert`, `merge-people`, `merge-families`, `diff` and `combine`. Flags and JSON output may change until v1.0.

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

`list <file> plac` lists places instead of records: each distinct `PLAC`
value exactly as written, with how many times it is used, most used first
(ties in byte order), so each variant spelling of a place is its own line:

```sh
gedcom list family.ged plac
```

```text
12	Springfield, Example County, Ohio
3	Springfield,  Example,   Ohio
1	Ohio SPRINGFIELD
```

In JSON each place is `{"count", "place"}`, with `latitude` and `longitude`
from the first use of the place that has a `MAP` giving both.

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

`diff` compares two versions of the same tree, such as an earlier export and a
re-download, record by record: records added, removed and changed, matched by
xref and type. Each changed record is followed by its changed lines in GEDCOM
form, grouped by the structure they belong to, with each line's number in its
own file (`-` the old file, `+` the new):

```sh
gedcom diff old-export.ged family.ged
gedcom diff --ignore-tag _USER --ignore-tag _HPID --ignore-tag _WPID old-export.ged family.ged
gedcom diff --format json old-export.ged family.ged | jq '.changed[].xref'
```

```text
added	@I4@	INDI
removed	@I3@	INDI
changed	@I1@	INDI
  in INDI > BIRT
  - line 13: 2 DATE 3 MAR 1901
  + line 13: 2 DATE 4 MAR 1901
```

Only changes in meaning are reported: substructures reordered among siblings
with other tags (`SEX` before `NAME`) and a value split differently across
`CONT`/`CONC` lines are not changes; reordering siblings with the same tag
(two `NAME`s, `CHIL` order) is. The header is left out unless
`--include-header` is given, since its `DATE` and `TIME` change on every
export. `--ignore-tag TAG` (repeatable) leaves a tag out wherever it appears,
for vendor tokens that change between exports of an unchanged tree. Finding
differences is not a failure: `diff` exits `0` either way, and prints
`no differences` when there are none. In JSON, `added`, `removed` and
`changed` are arrays and `header` is `null` unless compared and different. A
change's `path` is the tags below the record, without its type (`["BIRT"]` is
`in INDI > BIRT`; `[]` is the record's own lines). The header has no xref: its
text row is `changed`, an empty field, then `HEAD`, and its JSON `xref` is
`""`.

`media check` reports whether each `FILE` reference resolves to a file under a
media folder: `ok`, `missing`, `empty` (a `FILE` with no value: the media is
not a local file), `url`, `absolute` (a rooted path) or `outside` (a relative
path that climbs out of the folder); the last two are not resolved. It exits
`1` when any reference is missing. `--unreferenced` also lists the files in
the folder that nothing references. The media files are only looked at, never
changed:

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

`extract` writes a branch of the tree to its own file: the individuals given,
every family one of them is a member of, and the sources, notes, media,
repositories and submitters those records cite. Other individuals are not
pulled in. `-` reads xrefs from stdin, one word each, so feed it the `xref`
column of `ancestors`, `descendants` or `list` JSON through `jq` (their text
output carries names too):

```sh
gedcom extract tree.ged @I1@ @I2@ -o couple.ged
gedcom descendants --format json tree.ged I1 | jq -r '.[].xref' | gedcom extract tree.ged - -o branch.ged
gedcom extract --redact-living --strip-submitter tree.ged I1 -o share.ged
```

```text
gedcom: warning: dropped pointer @F3@ HUSB @I7@ (restorable)
gedcom: warning: dropped pointer @F3@ WIFE @I8@ (restorable)
wrote couple.ged: 6 records, 2 individuals, 1 family, 2 pointers dropped
```

A pointer to an individual or family left out (a family's `HUSB` for a
parent not extracted) is removed, so the output has no dangling pointers. Each
one is a warning on stderr and an entry in the JSON report's `dropped`, marked
restorable when combining the file with another cut from the same master that
holds the target is sure to put it back. Records keep their xrefs and every
kept line is written as it was read. The output carries the input's header and
submitter, contact details included, unless `--strip-submitter` replaces each
submitter record with one named `Submitter`; the header itself is copied as
read even then, so check its source and corporation lines before sharing.
`--redact-living` applies to the file written. `-o` may not name the input;
`-o -` writes the GEDCOM to stdout and the report to stderr.

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

The report starts with what the decoder could not read cleanly in the input
(`decode`, as `validate` reports it): a line it could not parse is copied to
the output as it was. Then comes the library's conversion report: what the
target version cannot hold (`data loss`, with each `dropped` value), what was approximated,
normalized or preserved, each kind of transformation with a count, and
validation issues in the result. A conversion that loses data still writes
the output and exits `0`; with `--fail-on-loss` it writes nothing and exits
`1`, with the report on stdout. An existing output file is replaced only once
the whole result is ready, so a failed conversion leaves it as it was. Lines the conversion leaves alone keep their
layout, so converting a UTF-8 file to the version it already has writes it
back byte-identical. 5.5 is not a target, since 5.5 does not allow UTF-8.
Text from an ANSEL file is written decomposed (a letter followed by a
combining accent), as gedcom-go decodes it.

`merge-people` folds one individual into another, once you have confirmed
they are the same person (say, a pair `duplicates` found); nothing is merged
on its own. The first xref is kept, the second is dropped, and the result is
written to a new file. gedcom-go moves the dropped person's names, events,
family links, citations and notes to the kept one, skipping any the kept one
already holds line for line, and repoints every pointer to the dropped one.
Records not involved are written exactly as they were read. `merge-families`
does the same for two families:

```sh
gedcom merge-people family.ged @I1@ @I7@ -o merged.ged
gedcom merge-families merged.ged @F1@ @F4@ -o merged2.ged
gedcom merge-people --format json family.ged I1 I7 -o merged.ged | jq '.moved[] | select(.conflict)'
```

```text
conflict	BIRT	1 BIRT | 2 DATE 1802 | 2 SOUR @S1@
moved	DEAT	1 DEAT | 2 DATE 14 NOV 1878
moved	FAMS	1 FAMS @F4@
deduplicated	NAME	1 NAME Ada /Example/
repointed	@F4@	FAM.WIFE
collapsed	@F3@	FAM.CHIL
candidate	@I3@	@F1@	@F4@
```

Each line says what happened to one substructure or pointer: `moved` to the
kept record; `conflict`, moved, but the kept record already held that tag
with other lines (for a family, a second `HUSB` or `WIFE`): one fact stated two
ways, such as two births, or two facts, such as two occupations, so check
which; `deduplicated`, already there; `repointed`, a pointer now naming the
kept record; `collapsed`, a pointer removed because it would repeat its
neighbor. `candidate` lists families the kept person is now a spouse in with
the same partner: candidates for `merge-families`, which `merge-people` does
not merge. Run `validate` on the result.

`combine` joins files into one, in the order given, and writes it to a new
file. No people are merged. `--strategy` says what to do with an xref two
files share:

- `zip`: the files were cut from one master by `extract`, so a shared xref is
  the same record. Its copies are joined, putting back the pointers to people
  and families each file dropped at its boundary. Copies that differ in
  anything else conflict: each conflicting xref is listed, the command exits
  1, and nothing is written. The output holds the master's records, though
  not always in its order: records follow the first file that holds them, and
  a family split between files gets its `HUSB`, `WIFE` and `CHIL` lines back
  in one file's order.
- `prefix`: unrelated trees. A later file's colliding xrefs get that file's
  `--prefix` (one per file after the first), so each record's origin stays in
  its xref.
- `renumber`: as `prefix`, with the next free number instead.

```sh
gedcom combine paternal.ged maternal.ged --strategy zip -o tree.ged
gedcom combine mine.ged cousin.ged --strategy prefix --prefix C_ -o both.ged
```

```text
wrote both.ged: 412 records, 260 individuals, 98 families
cousin.ged	@F1@	@C_F1@
cousin.ged	@I1@	@C_I1@
```

Each renamed xref is a line (file, old, new); `--format json` gives the same
as a `remapped` map per input. The files must share a GEDCOM version; they may
declare different encodings, and the output is UTF-8. The header is the first
file's; a later file's differing header values are dropped (a warning on
stderr, or `header_conflicts` in JSON).

Run `gedcom --help` or `gedcom <command> --help` for every command and flag.

## Conventions

These hold for every command, so scripts and agents can rely on them:

- **Results go to stdout, diagnostics to stderr.** Stderr is empty on
  success, except for a warning that a result is incomplete (`duplicates`,
  or `combine` dropping a later file's differing header values) or that a
  pointer was dropped (`extract`). A write command given `-o -` puts the file
  on stdout and its report on stderr; with `--format json` stderr then holds
  only that JSON report.
- **`--format json`** gives machine-readable output; the default is text.
- **Exit codes:** `0` success, `1` the command failed (unreadable file, decode
  error), `validate` found errors or `combine` found conflicts, `2` the command
  line was wrong.
- **Input files are never modified.** A command that writes a file takes it
  as `-o`, which may not name an input, and replaces an existing file only
  once the whole output is written, keeping that file's permissions.
- **`--redact-living`** replaces what the output says about a person presumed
  living with `Living`: name, dates, places, notes, addresses, and in `get`
  and the merge reports their raw lines (a file a command writes is not
  redacted). The xref, sex and family links stay, so the structure is
  still readable. A family with a living spouse keeps only its spouses and
  children, and a note record a hidden person or family points to reads
  `Living`. `validate` still checks the whole file; a finding about a hidden
  record reads `Living` instead of its message, and `diff` lists a changed
  hidden record with its lines replaced by `Living`. Source, media, repository and
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
