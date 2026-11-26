# gedcom-cli

`gedcom` is a command-line tool for inspecting, validating and correcting
GEDCOM genealogy files. It is a thin wrapper over
[gedcom-go](https://github.com/cacack/gedcom-go), built equally for use at a
terminal and by AI coding agents. See [CONSTITUTION.md](CONSTITUTION.md) for
what it is and is not trying to be.

**Status: early.** The commands so far are `info`, `get`, `list`,
`ancestors`, `descendants`, `relatives`, `media`, `places`, `validate`, `duplicates`, `extract`, `put`, `put-header`, `delete`, `name`, `retag`, `convert`, `merge-people`, `merge-families`, `merge-sources`, `merge-repositories`, `repeated-events`, `repeated-names`, `diff` and `combine`. Flags and JSON output may change until v1.0.

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

`checksums.txt` comes from the same release, so it shows the download is intact,
not where it was built. Releases after v0.4.0 carry a build-provenance
attestation; verify an archive against it with the
[GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify gedcom_*_linux_amd64.tar.gz --repo cacack/gedcom-cli
```

**With Go** (1.25+):

```sh
go install github.com/cacack/gedcom-cli/cmd/gedcom@latest
```

`gedcom --version` reports the release either way.

## Usage

| Command | What it does |
| --- | --- |
| [`info`](#info) | Summarize a file: version, encoding, source system, record counts |
| [`get`](#get) | Print records by xref, or the header |
| [`refs`](#refs) | List the pointers to records by xref: who uses this source, media record or person |
| [`list`](#list) | List the records of one type, or the people's names, the places, tag paths or one tag's values, one per line |
| [`ancestors`](#ancestors-and-descendants) | List a person's ancestors by generation |
| [`descendants`](#ancestors-and-descendants) | List a person's descendants by generation |
| [`relatives`](#relatives) | Print people with their parents, siblings, spouses and children |
| [`validate`](#validate) | Report what the decoder could not read cleanly and what validation finds |
| [`duplicates`](#duplicates) | Report people in one file who may be the same as people in another |
| [`diff`](#diff) | Compare two versions of a file record by record |
| [`media check`](#media-check) | Report whether each `FILE` reference resolves under a media folder |
| [`media add`](#media-add) | Link a media file from a record, writing a new file |
| [`media set`](#media-set) | Set the `FILE` of an existing `OBJE` record, writing a new file |
| [`places apply`](#places-apply) | Rewrite `PLAC` values and set their coordinates from a place table, writing a new file |
| [`extract`](#extract) | Write a branch of the tree to its own file |
| [`put`](#put) | Replace records by xref, or add new ones, writing a new file |
| [`put-header`](#put-header) | Replace the header, writing a new file |
| [`delete`](#delete) | Remove records by xref, writing a new file |
| [`name`](#name) | Edit one of a person's names: reorder it, set its TYPE, value or NICK |
| [`retag`](#retag) | Give every line at one tag path a new tag, writing a new file |
| [`move-value`](#move-value) | Move event lines' values into their `PLAC`, `ADDR` or a `NOTE` from a table, writing a new file |
| [`convert`](#convert) | Rewrite a file as GEDCOM 5.5.1 or 7.0, in UTF-8 |
| [`merge-people`](#merge-people-and-merge-families) | Fold one individual into another, writing a new file |
| [`merge-families`](#merge-people-and-merge-families) | Fold one family into another, writing a new file |
| [`repeated-events`](#repeated-events) | Report events a record holds twice, and fold them into one, writing a new file |
| [`repeated-names`](#repeated-names) | Report names a person holds twice, and fold them into one, writing a new file |
| [`merge-sources`](#merge-sources-and-merge-repositories) | Fold one source into another, writing a new file |
| [`merge-repositories`](#merge-sources-and-merge-repositories) | Fold one repository into another, writing a new file |
| [`combine`](#combine) | Join files into one, writing a new file |

Each section below says what a command is for and shows a typical run.
`gedcom <command> --help` is the reference for its arguments, flags, output
fields, JSON keys and edge cases, with more examples; `gedcom --help` lists
the commands. What holds for every command is under
[Conventions](#conventions).

### info

Summarizes a file: a first look at what an export holds.

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

### get

Prints single records by xref, in the order given, as the GEDCOM lines of
each record. Its text output is what [`put`](#put) takes back.

```sh
gedcom get family.ged @I1@
gedcom get family.ged I1 F1          # the @ delimiters are optional
gedcom get family.ged HEAD           # the header
gedcom get --format json family.ged @I1@
gedcom list --format json family.ged fam | jq -r '.[].xref' | gedcom get family.ged -
```

`-` reads more xrefs from stdin, as for [`extract`](#extract), printed where
`-` stands among the arguments; the file is read once however many there are.
With `--format json` each record is gedcom-go's own shape for it, plus a
`Type` key holding its level-0 tag (`INDI`, `FAM`, `SUBM`, and `HEAD` for the
header). `put` takes records back; [`put-header`](#put-header) takes the
header back.

### refs

Lists the pointers to records by xref, one per line: the record holding it
(`-` for the header), the tag path to it and the record it points at. It is
the list [`delete`](#delete) refuses with, asked for without writing
anything: who uses this source, this media record, this repository, before a
merge or a cleanup.

```sh
gedcom refs family.ged @O1@
gedcom list --format json family.ged obje | jq -r '.[].xref' | gedcom refs family.ged -
```

```text
@I1@	INDI.OBJE	@O1@
@F1@	FAM.OBJE	@O1@
```

A record nothing points at lists nothing and exits `0`; an xref the file does
not define exits `1`. `-` reads xrefs from stdin as for `get`. With
`--format json` each pointer has the keys `delete` reports: `from`, `path`,
`to`. The output is xrefs and tag paths only, so `--redact-living` does not
change it.

### list

Prints one line per record of a type (`indi`, `fam`, `sour`, `repo`, `obje`,
`subm`, or the shared notes `note` in GEDCOM 5.x and `snote` in 7.0),
tab-separated and in file order. That is every record type but `SUBN` and
extension records; `--help` gives each type's fields.

```sh
gedcom list family.ged indi
gedcom list --limit 20 family.ged fam
```

```text
@I1@	Ada /Example/	3 MAR 1901	14 NOV 1978	false
@I2@	Ben /Example/	21 JUL 1999		true
```

The last `indi` field, `living`, is whether gedcom-go presumes the person
living (see [Conventions](#conventions)). With `--format json` each record is
a flat object, so `jq` does the searching: to leave the living out, or to find
people by surname:

```sh
gedcom list --format json family.ged indi | jq '.[] | select(.living | not)'
gedcom list --format json family.ged indi | jq '.[] | select(.name | test("/Example/"))'
```

`list <file> names` lists each `NAME` line of each individual, one per row in
file order, so a person's later names show too: xref, the name's position
among the person's names, the value, the `GIVN`, `SURN`, `NSFX`, `NICK` and
`TYPE` under it as written (empty when absent), the number of citations under
it, and `same_as`, the position of the person's first earlier name equal to
this one (0 if none). Two names are equal, by gedcom-go's rule, when their
values hold the same words in the same order, case ignored, with spaces,
punctuation and the surname slashes only separating words: `John /Smith/ Jr.`
equals `john  smith, jr`, but `O'Brien` does not equal `OBrien`.
`--repeated` keeps only the names with a `same_as`. To fold repeated names
see [`repeated-names`](#repeated-names); `validate --style NAME_PART_MISMATCH`
reports a `GIVN` or `SURN` that disagrees with its name's value:

```sh
gedcom list family.ged names
gedcom list --repeated family.ged names
```

```text
@I1@	1	John /Smith/ Jr.	John	Smith	Jr.			1	0
@I1@	2	john smith, jr						2	1
@I1@	3	Jack /Smith/				Jack	aka	0	0
```

`list <file> plac` lists places instead of records: each distinct `PLAC`
value exactly as written, with how many times it is used, most used first, so
each variant spelling of a place is its own line:

```sh
gedcom list family.ged plac
```

```text
12	Springfield, Example County, Ohio
3	Springfield,  Example,   Ohio
1	Ohio SPRINGFIELD
```

`list <file> tags` surveys what the file holds: each tag path (the record
type and the tags down to the line, such as `INDI.BIRT.DATE`) with its number
of lines, most used first. The counts cover every line but the header, the
trailer and the records' level-0 lines. `--vendor` keeps only the paths with
an extension tag (starting with `_`) in them. `list <file> values --tag
<path>` lists the distinct values at one path with their counts, as `plac`
does for places:

```sh
gedcom list --vendor family.ged tags
gedcom list --tag INDI.EVEN.TYPE family.ged values
```

```text
14	INDI._MILT
14	INDI._MILT.DATE
2	INDI._UID
```

To see such a value beside the record that holds it, `--with <path>` adds a
column per path to a record type's listing, after its own fields and in the
order given. A column holds every value at the path in file order, joined by
` | `, with `CONT` lines folded in (as spaces in text); it is empty when the
record has none. In JSON each row gets a `with` object from each path to an
array of its values:

```sh
gedcom list --with OBJE.FILE.FORM._SIZE --with OBJE._OID family.ged obje
```

```text
@O1@	Ada, 1920	ada.jpg	image/jpeg	20480	0001
```

### ancestors and descendants

Walk one person's line by generation (1 is parents or children, 2
grandparents or grandchildren), one row per relative in `list indi`'s fields
with the generation first. These and [`relatives`](#relatives) follow a
family link only when both sides record it: a `FAMS` or `FAMC` on the person,
and a `HUSB`, `WIFE` or `CHIL` naming them on the family.

```sh
gedcom ancestors family.ged @I1@
gedcom descendants --generations 2 family.ged I1
gedcom ancestors --format json family.ged @I1@ | jq '.[] | select(.generation == 3)'
```

```text
1	@I2@	Bert /Example/	1920	1990	false
1	@I3@	Cleo /Sample/			true
2	@I4@	Dan /Example/			true
```

### relatives

Prints a person and their immediate family, one per line: the person
(`self`), then parents, siblings, spouses and children. Each line gives the
relationship, the family it comes through, the person, and the child's
pedigree (`PEDI`) in that family where the file gives one, so birth and
adoptive parents, or two marriages, show separately:

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

Several people can be given in one call, which reads the file once, and `-`
reads more xrefs from stdin, as for [`extract`](#extract). Each person's lines
follow in turn, each starting with its `self` line; `--format json` prints an
array of the single-person objects. One xref given alone prints one object,
as above, so a script whose list may hold one xref should send it on stdin
to get an array every time:

```sh
gedcom relatives --format json family.ged I1 I2 I3
printf '%s\n' $XREFS | gedcom relatives --format json family.ged -
gedcom duplicates --format json old-export.ged family.ged | jq -r '.[].b.xref' | gedcom relatives --format json family.ged -
```

### validate

Reports what the decoder could not read cleanly (`decode`) and what
gedcom-go's validator finds in the records (`validate`), grouped by severity
with a count per group. Each line gives where the finding is, its code, its
message and its source:

```sh
gedcom validate family.ged
gedcom validate --min-severity error family.ged
gedcom validate --format json family.ged | jq .counts
```

```text
error (2)
  @I1@ line 13	INVALID_LEVEL	invalid level number (context: "this line is not GEDCOM")	decode
  @I1@ line 18	ORPHANED_FAMS	FAMS reference to non-existent family @F9@ (related: @F9@)	validate
warning (1)
  @I2@ line 19	MISSING_NAME	Individual record has no NAME	validate
```

A family link only one record claims, such as a `FAMS` to a family that does
not name the person as `HUSB` or `WIFE`, is an `UNRECIPROCATED_<tag>` warning,
where `<tag>` is the pointer that is there. It is reported on the individual,
with the family as related xref.

A finding's xref is the record it is about or, for a decode finding, the
record its line is in, ready for `get` and `put`. A decode finding in the
header or outside any record has none. In `--format json` it is the `xref`
key, absent when there is none.

A file with nothing to report prints `family.ged: no findings`. The exit code
is `1` whenever the file has an error-severity finding, with the report still
on stdout and a one-line summary on stderr.

`--style` adds gedcom-go's personal-name style checks: habits that are valid
GEDCOM but usually worth tidying in an imported tree, such as a surname in
capitals, text after the closing slash, a nickname in quotes or a vendor tag
under `NAME`. They are style, not conformance: off by default, reported as
warnings with a `NAME_*` code once per `NAME` line, and they never change the
exit code. Give codes or `all`, and leave some out with `--skip-style`;
`validate --help` lists the checks:

```sh
gedcom validate --style all family.ged
```

```text
warning (2)
  @I1@ line 11	NAME_ALL_CAPS	Name style: a given name or surname with a word of two or more letters all in capitals	validate
  @I2@ line 14	NAME_TEXT_AFTER_SURNAME	Name style: text after the closing slash of the surname	validate
```

To run every check but one:

```sh
gedcom validate --style all --skip-style NAME_TEXT_AFTER_SURNAME family.ged
```

### duplicates

Reports people in one file who may be the same as people in another, for
reconciling overlapping trees. Each line is a candidate pair: the score (0 to
1, from gedcom-go's duplicate detection), then the person from the first file
in `list indi`'s fields, then the one from the second. Pairs are candidates
for review, not conclusions; nothing is merged or written:

```sh
gedcom duplicates old-export.ged family.ged
gedcom duplicates --min-score 0.9 old-export.ged family.ged
gedcom duplicates --format json old-export.ged family.ged | jq '.[] | [.a.xref, .b.xref]'
```

```text
0.90	@I1@	Ada /Example/	3 MAR 1901	14 NOV 1978	false	@I7@	Ada /Example/	1901		false
0.80	@I4@	Ada /Example/	1901		false	@I7@	Ada /Example/	1901		false
0.70	@I2@	Ben /Example/	21 JUL 1899		false	@I1@	Ben /Example/			true
```

Xrefs are each file's own, so `@I1@` can be a different person on each side.

The score sums what the two records agree on: surname, given name, birth year
and sex make up 0.9, and a full birth date, death year and places the rest. A
value one record lacks costs nothing; a contradiction (two different known
sexes, exact birth or death years more than two apart) rules the pair out.
`duplicates --help` lists the weights. Only people with the same surname are
compared, unless `--relatives` is given: it adds spouses' names (0.2) and
shared children's names (0.1 each, up to two) to the score, and compares people
who share a spouse's name whatever their surnames, so a woman under her
married name in one file and her maiden name in the other is found:

```sh
gedcom duplicates --relatives old-export.ged family.ged
```

`--groups` reports groups instead of pairs: the connected components of the
pairs at the chosen `--min-score`, so a person held twice in each file is one
group rather than four pairs. Each group lists its members' xrefs in each
file and the pairs, with scores, that join them; in JSON a group is an object
with `a`, `b` and `pairs`:

```sh
gedcom duplicates --groups old-export.ged family.ged
gedcom duplicates --groups --format json old-export.ged family.ged | jq -r '.[0].b[]' | gedcom relatives family.ged -
```

```text
group 1: a @I1@ @I4@; b @I7@
  0.90	@I1@	Ada /Example/	3 MAR 1901	14 NOV 1978	false	@I7@	Ada /Example/	1901		false
  0.80	@I4@	Ada /Example/	1901		false	@I7@	Ada /Example/	1901		false
group 2: a @I2@; b @I1@
  0.70	@I2@	Ben /Example/	21 JUL 1899		false	@I1@	Ben /Example/			true
```

Once a pair is confirmed, [`merge-people`](#merge-people-and-merge-families)
folds one into the other.

### diff

Compares two versions of the same tree, such as an earlier export and a
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

Only changes in meaning are reported, and the header is left out by default;
the help says what counts. Finding differences is not a failure: `diff` exits
`0` either way.

### media check

Reports whether each `FILE` reference resolves to a file under a media
folder. It exits `1` when any reference is missing. The media files are only
looked at, never changed:

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

### media add

Links a file from an individual, family or source, reusing the `OBJE` record
with that path or creating one, and writes the result to a new file. Running
it again changes nothing. A tab-separated file of links is applied whole or
not at all:

```sh
gedcom media add family.ged --to @I1@ --path photos/ada.jpg --title "Ada, 1920" -o out.ged
gedcom media add family.ged --root media --batch links.tsv -o out.ged
```

```text
@I1@	@O2@	created	photos/ada.jpg
```

### media set

Sets the `FILE`, and with `--form` the `FORM`, of an `OBJE` record that
already exists, such as one an export left with an empty `FILE`, and writes
the result to a new file. The record keeps its xref, its other lines and every
link to it. A `FILE` that holds another path is replaced only with
`--replace`, and a record with more than one `FILE` is refused. `--root`
checks each path as `media add` does. A tab-separated file of xref, path and
an optional `FORM` is applied whole or not at all:

```sh
gedcom media set family.ged --root media --obje @O1@ --path photos/ada.jpg -o out.ged
gedcom media set family.ged --root media --batch files.tsv -o out.ged
```

```text
@O1@	set	photos/ada.jpg
@O2@	unchanged	photos/ben.jpg
```

### places apply

Applies a place authority list to a file once you have settled what each
place string in `list plac` should read. The table is tab-separated with a
header row: `place` (matched byte for byte), `standard`, and `latitude` and
`longitude` in GEDCOM form; other columns are ignored. Each `PLAC` whose
value is a `place` gets the `standard` name and, when the row has
coordinates, a `MAP` with `LATI` and `LONG`, or the table's coordinates in
the `MAP` it already has. A malformed row is reported by line and nothing is
written:

```sh
gedcom places apply family.ged places.tsv -o out.ged
```

```text
wrote out.ged: 5 PLAC rewritten, 3 MAP added, 1 MAP updated, 3 records changed
  no MAP permitted	line 48
  no MAP permitted	line 51
  unmatched	Nowhere, Example County
```

`MAP` is added only where the file's GEDCOM version permits one, as
gedcom-go's grammar check reads it: not under a place of publication
(`SOUR.PUBL.PLAC`), nor anywhere in a 5.5 file. Every other line is written as
read, so running it again with the same table changes nothing. A `PLAC`
continued on `CONC` or `CONT` lines is left as written.

### extract

Writes a branch of the tree to its own file: the individuals given, every
family one of them is a member of, and the sources, notes, media,
repositories and submitters those records cite. Other individuals are not
pulled in. `-` reads xrefs from stdin, so a branch can come from
`ancestors`, `descendants` or `list` JSON through `jq`:

```sh
gedcom extract tree.ged @I1@ @I2@ -o couple.ged
gedcom descendants --format json tree.ged I1 | jq -r '.[].xref' | gedcom extract tree.ged - -o branch.ged
gedcom extract --redact-living --strip-submitter tree.ged I1 -o share.ged
```

```text
gedcom: warning: dropped pointer @F3@ HUSB @I7@ (restorable)
gedcom: warning: dropped pointer @F3@ WIFE @I8@ (restorable)
wrote couple.ged: 6 records, 2 individuals, 1 family, 2 pointers dropped, header stamped (SOUR was ExampleSoft)
```

A pointer to an individual or family left out (a family's `HUSB` for a
parent not extracted) is removed, so the output has no dangling pointers;
[`combine`](#combine) with `--strategy zip` puts the restorable ones back.
The output carries the input's header, [stamped](#conventions), and its
submitter, contact details included: read the help on what
`--strip-submitter` does and does not remove before sharing a file.

To put the extracts of one master back together in the master's order, save
each one's JSON report and give `combine` the reports of the files after the
first as `--dropped`:

```sh
gedcom extract --format json tree.ged I1 I2 -o a.ged > a.json
gedcom extract --format json tree.ged I3 I4 -o b.ged > b.json
gedcom combine a.ged b.ged --strategy zip --dropped b.json -o tree2.ged
gedcom diff tree.ged tree2.ged
```

Give `--keep-header` to both commands to get the master's header back too
(`gedcom diff --include-header`). Without the reports, a family split between
files gets its `HUSB`, `WIFE` and `CHIL` lines back in one file's order. Either way the round trip
reproduces only the records reachable from the people extracted: a record
nothing cites (a `SUBM`, `REPO` or `SOUR` with no pointer to it) is in no
extract, and so not in the combined file.

### put

Corrects a file from a file of level-0 records, with no header, as `get`
prints them. Each record replaces the one with the same xref, in its place,
or is appended when the xref is new. `get`, edit, `put` is the edit loop:

```sh
gedcom get family.ged @I1@ > i1.ged
$EDITOR i1.ged
gedcom put family.ged i1.ged -o family-edited.ged
```

```text
wrote family-edited.ged: 1 replaced, 0 added, 0 unchanged
  replaced	@I1@
```

Every line `put` was not asked to change is written exactly as it was read,
so a record put back unedited leaves the output byte-identical. What the
decoder could not read cleanly in the records file is reported as `validate`
reports it, with its xref and its line number in that file; an error stops
the write. Pointers are not checked: run `validate` on the output. The header
is written as read; `--stamp` [stamps](#conventions) it.

### put-header

Replaces a file's header with the lines `get <file> HEAD` prints, edited: to
drop lines another program left in it (`DEST`, `FILE`, `COPR`, `NOTE`), or
to make its `SOUR`, `DATE` and `SUBM` describe the tree itself.

```sh
gedcom get family.ged HEAD > head.ged
$EDITOR head.ged
gedcom put-header family.ged head.ged -o family-edited.ged
gedcom diff --include-header family.ged family-edited.ged
```

```text
wrote family-edited.ged: header replaced
```

Every line outside the header is written exactly as it was read, and a header
put back unedited leaves the output byte-identical. A header whose `GEDC.VERS`
or `CHAR` differs from the file's, as `get` prints it, is refused: that is
`convert`'s job. What the version requires of a header (`SOUR`, `SUBM`) is
not checked: run `validate` on the output.

### delete

Removes records by xref. While other records still point at them, it writes
nothing, lists each pointer (the record holding it, the tag path to it, the
record it points at) and exits `1`:

```sh
gedcom delete family.ged @I2@ -o family-edited.ged
```

```text
family-edited.ged not written: pointers left to @I2@
references (1)
  @F1@	FAM.HUSB	@I2@
```

### name

Edits one of a person's names, addressed by its position among the person's
`NAME` lines, counted from 1 in file order. It can move the name, with every
line under it, to another position; set or clear its `TYPE` or `NICK`; and
set its value, rewriting the `GIVN`, `SURN` and `NSFX` lines under it to the
text before, between and after the slashes and removing a part left empty.
Citations and notes under the name stay as they are:

```sh
gedcom name family.ged @I1@ 2 --move-to 1 --type birth -o fixed.ged
```

```text
wrote fixed.ged: @I1@ name 2, now name 1
  before	1 NAME Ada /Example/ | 2 SOUR @S1@
  after	1 NAME Ada /Example/ | 2 TYPE birth | 2 SOUR @S1@
```

Which name is right is yours to say; the command infers nothing. The output
is validated before it is written: an edit that adds a finding the input did
not have, such as a `TYPE` GEDCOM 7.0 does not define, writes nothing and
exits `1`.

### retag

Gives every line at one tag path a new tag: the bulk edit for a structure
written with one tag that belongs under another, such as a vendor tag that
has a standard form. The path is one `list <file> tags` prints, a record type
and the tags down to the line, and only lines at exactly that path are
renamed. Each keeps its value and the lines under it; `--add` puts a new line
directly under each, and may be repeated:

```sh
gedcom list --vendor family.ged tags
gedcom retag family.ged INDI._MILT EVEN --add "TYPE Military Service" -o out.ged
```

```text
wrote out.ged: 34 lines INDI._MILT renamed EVEN, 34 lines added, 24 records changed
```

A structure that already has a line `--add` would add is reported with its
xref and line, and nothing is written. The output is validated before it is
written, as `name` does: an edit that adds a finding the input did not have,
such as an `EXID` under a citation, where GEDCOM 7.0 permits none, writes
nothing and exits `1`. A path no line is at writes the file byte-identical.

### move-value

Moves a value written on the wrong line of an event to the line it belongs
on, as a table says: an `ADDR` that is a building's name to the head of the
`PLAC`, a `PLAC` that is a street address to `ADDR`, or a `PLAC` that is
neither to a `NOTE` that keeps the words. The table is tab-separated with a
header row: `path` (a tag path as `list <file> tags` prints it, or `*.PLAC`
for a `PLAC` under any event), `value` (the whole value, matched byte for
byte), `action` (`prepend PLAC`, `to ADDR`, `to PLAC` or `to NOTE`) and, for
`to NOTE`, a `prefix`:

```sh
gedcom move-value family.ged moves.tsv -o out.ged
```

```text
wrote out.ged: 4 lines moved, 1 MAP removed, 3 records changed
  row 2	INDI.BIRT.ADDR	Example General Hospital	prepend PLAC	1 moved, 1 MAP removed, 1 record changed
  row 3	*.PLAC	at home	to NOTE	3 moved, 0 MAP removed, 3 records changed
  unmatched	row 4	*.ADDR	Nowhere
```

`prepend PLAC` removes the `MAP` under that `PLAC`, since its point was the
old place's. A line the action cannot be applied to is reported with its
record and line, and nothing is written: `to ADDR` or `to PLAC` on an event
that has one already, `prepend PLAC` on an event with no `PLAC` or to a
`PLAC` with a `FORM` or `TRAN` that describes the old value, or a line with
substructures no action accounts for. The command decides nothing:
which values are building names is the table's to say. The output is
validated before it is written, as `retag` does, and a table with no rows
writes the file byte-identical.

### convert

Rewrites a file as GEDCOM 5.5.1 or 7.0 with gedcom-go's converter, to a new
file, always in UTF-8. It brings a legacy 5.5 ANSEL file up to the version
and encoding of the rest of a working set:

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

A conversion that loses data still writes the output and exits `0` unless
asked to fail. 5.5 is not a target, since 5.5 does not allow UTF-8. Text from
an ANSEL file is written decomposed (a letter followed by a combining
accent), as gedcom-go decodes it.

### merge-people and merge-families

`merge-people` folds one individual into another, once you have confirmed
they are the same person (say, a pair [`duplicates`](#duplicates) found);
nothing is merged on its own. The first xref is kept, the second is dropped,
and the result is written to a new file. `merge-families` does the same for
two families:

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

Each line says what happened to one substructure or pointer. A `conflict`
was moved, but the kept record already held that tag with other lines: one
fact stated two ways, or two facts, so check which. A `candidate` is a pair
of families the kept person is now a spouse in with the same partner, which
`merge-people` leaves for `merge-families`. Run `validate` on the result.

Two files rarely write one event with the same lines, so a person merged from
several files can end up with one `BIRT` per file. `--fold-events` (on both
commands) folds an event of the dropped record into the kept record's event
of the same tag when their dates are the same date, however written
(`7 DEC 1977`, `07 Dec 1977`), and their places agree or only one has a
place; the kept event gains the other's citations, notes and place. Each fold
is reported as `folded`, with the dropped event's lines and the kept event's
once every fold into it is done. Events that differ in date stay separate and are reported as
conflicts. `--help` gives the full rule.

### repeated-events

Reports the events one individual or family already holds twice: what a
file is left with by merges made without `--fold-events`, or by an import.
Two events are the same by the rule `--fold-events` folds by, which
`gedcom repeated-events --help` and `gedcom merge-people --help` both give;
an event with no `DATE` (a residence with only an address) is never
reported. `--fold` folds each group into its first event, as `--fold-events`
does, and writes the result to a new file:

```sh
gedcom repeated-events family.ged
gedcom repeated-events family.ged @I1@ @F2@
gedcom repeated-events --fold family.ged -o folded.ged
```

```text
repeated	@I1@	BIRT	1 BIRT | 2 DATE 7 DEC 1977	1 BIRT | 2 DATE 07 Dec 1977 | 2 SOUR @S1@
repeated	@F1@	MARR	1 MARR | 2 DATE 12 JUN 2000 | 2 PLAC Exampletown	1 MARR | 2 DATE 12 JUN 2000
```

With `--fold`, the report lists each event removed as `folded`, with the
record, its lines and those of the event it was folded into, as
`merge-people` does. Records with nothing to fold are written as read, and
folding the output again folds nothing.

### repeated-names

Reports the names one individual holds twice, often with different citations
under each copy, and with `--fold` folds each into the first, writing a new
file. Two names are the same when their values are equal by the rule
`list names` gives `same_as` by, their `TYPE`s agree (or neither has one),
and each name piece (`GIVN`, `SURN`, `NSFX` and the rest) both have is
written identically; a piece only one has does not keep them apart.
`gedcom repeated-names --help` gives the full rule:

```sh
gedcom repeated-names family.ged
gedcom repeated-names --fold family.ged -o folded.ged
```

```text
repeated	@I1@	NAME	1 NAME John /Example/ | 2 SOUR @S1@ | 3 PAGE 1	1 NAME john example | 2 SOUR @S1@ | 3 PAGE 2
```

The first name keeps its lines and gains each line of the others it lacks,
so a citation both hold line for line is kept once. The report and the rest
of the file behave as for `repeated-events`.

### merge-sources and merge-repositories

`merge-sources` folds one source record into another: a file combined from
two exports of one site often holds the same source many times. Every
citation of the dropped source is repointed to the kept one, with its own
lines (`PAGE`, `DATA`, vendor tags) untouched, and the dropped source is
removed. `merge-repositories` does the same for two repositories, repointing
the sources' `REPO` lines:

```sh
gedcom merge-sources family.ged @S1@ @S7@ -o merged.ged
gedcom merge-repositories merged.ged @R1@ @R2@ -o merged2.ged
gedcom merge-sources --format json family.ged S1 S7 -o merged.ged | jq '.dropped'
```

```text
conflict	_APID	1 _APID 1,5678::0
dropped	TITL	1 TITL Example parish register, 1800-1850	1 TITL Example parish register
deduplicated	REPO	1 REPO @R1@
repointed	@F1@	FAM.SOUR
```

The report is the merge-people one, with one more kind of line. A line a
source or repository may hold only once (`TITL`, `AUTH`, `PUBL`, `NAME`,
`ADDR` and so on) is carried when the kept record has none; when the kept
record holds a different one, it stays, and the dropped record's is reported
as `dropped`, beside the kept one, and is not in the output. Other lines
(`NOTE`, `REFN`, vendor tags such as `_APID`) are carried, and reported as a
`conflict` when the kept record already held that tag with other lines.
`--help` lists what each command carries. Run `validate` on the result.

One run can merge many pairs, on all four merge commands. Name several drops
after the keep, or give `--batch` a tab-separated file of pairs, one per
line: keep, then drop. Blank lines and lines starting with `#` are skipped:

```sh
gedcom merge-sources family.ged @S1@ @S7@ @S9@ -o merged.ged
gedcom merge-sources family.ged --batch pairs.tsv -o merged.ged
```

```text
# pairs.tsv
@S1@	@S7@
@S2@	@S8@
```

The merges run in the order given, each on the result of the one before, so
the output is the file the same pairs give merged one per run, each run on the
last one's output. A record an earlier pair kept may be dropped by a later
one. Every pair is checked first: one that names a missing record or one of
another type, merges a record into itself, or names a record an earlier pair
dropped is reported by argument or by line, and nothing is written. The
report lists each pair's lines in turn, each starting with the pair's keep
and drop; `--format json` gives an array of reports, one per pair. A single
`<keep> <drop>` prints the report above, unlabeled. On every merge command
`--stamp` [stamps](#conventions) the header, which is otherwise written as
read.

### combine

Joins files into one, in the order given, and writes it to a new file. No
people are merged. The strategy says what to do with an xref two files share:
`zip` for files cut from one master by [`extract`](#extract), where a shared
xref is the same record; `prefix` or `renumber` for unrelated trees. `prefix`
renames every record of each later file, so a record's origin can be read
from its xref; `renumber` renames only a later file's colliding xrefs:

```sh
gedcom combine paternal.ged maternal.ged --strategy zip -o tree.ged
gedcom combine mine.ged cousin.ged --strategy prefix --prefix C_ -o both.ged
```

```text
wrote both.ged: 412 records, 260 individuals, 98 families, header stamped (SOUR was ExampleSoft)
cousin.ged	@F1@	@C_F1@
cousin.ged	@F2@	@C_F2@
cousin.ged	@I1@	@C_I1@
...
```

Each renamed xref is a line (file, old, new). The header is the first
file's, [stamped](#conventions) unless `--keep-header` is given.

## Conventions

These hold for every command, so scripts and agents can rely on them:

- **Results go to stdout, diagnostics to stderr.** Stderr is empty on
  success, except for a warning that a result is incomplete (`duplicates`,
  or `combine` dropping a later file's header values) or that a
  pointer was dropped (`extract`). A write command given `-o -` puts the file
  on stdout and its report on stderr; with `--format json` stderr then holds
  only that JSON report.
- **`--format json`** gives machine-readable output; the default is text.
- **The header names who wrote the file.** `extract` and `combine` make a new
  dataset, so they stamp its header: the input's `SOUR` structure is replaced
  by `1 SOUR gedcom-cli`, with `VERS` (what `gedcom --version` prints) and
  `NAME gedcom-cli`, and its `DEST`, `DATE` and `FILE` lines, which describe
  the input's export, are removed. Every other header line is kept.
  `--keep-header` writes the header as read, for a cut to be zipped back
  byte for byte. The other write commands edit a file in place of its author
  and write the header as read; `put` and the merge commands take `--stamp`
  to replace its `SOUR` structure the same way, leaving every other line,
  and `gedcom put --stamp file.ged /dev/null -o out.ged` does that to the
  output of any of them. In a 5.5 or 5.5.1 file the stamp keeps where the
  data came from as `2 DATA` under the new `SOUR`: the input's own `SOUR.DATA`
  with its lines, or else the input's `SOUR` value. GEDCOM 7.0 deprecates
  `HEAD.SOUR.DATA`, so a 7.0 header gets none. The report says when the
  header was stamped and what its `SOUR` was (`stamped` and `stamped_from` in
  JSON; `header stamped (SOUR was ...)` in a summary line, a `stamped HEAD`
  line in a merge report). [docs/spec-references.md](docs/spec-references.md)
  quotes the specification text this rule follows.
- **Exit codes:** `0` success, `1` the command failed (unreadable file, decode
  error), `validate` found errors or `combine` found conflicts, `2` the command
  line was wrong.
- **Input files are never modified.** A command that writes a file takes it
  as `-o`, which may not name an input, and replaces an existing file only
  once the whole output is written, keeping that file's permissions.
- **`--redact-living`** replaces what the output says about a person presumed
  living with `Living`: name, dates, places, notes, addresses, and in `get`
  and the merge reports their raw lines. The xref, sex and family links stay,
  so the structure is still readable. A family with a living spouse keeps
  only its spouses and children, and a note record a hidden person or family
  points to reads `Living`. `validate` and `convert` still check the whole
  file; a finding about a hidden record reads `Living` instead of its message,
  and `diff` lists a changed hidden record with its lines replaced by
  `Living`. A command that writes a file always redacts its report, but only
  `extract` and `combine`, which make a new file to share, redact the file
  too: `put`, `put-header`, `delete`, `name`, `retag`, `move-value`, `media add`, `media set`, `places apply`,
  the merge commands, `repeated-events`, `repeated-names` and `convert` edit or rewrite the whole tree and write
  it unredacted. Source, media, repository and submitter records are not
  redacted, even when a hidden record cites them, and neither is the header
  (`get HEAD`): check their titles, file names and addresses yourself. `refs` prints xrefs and
  tag paths only, and prints them all. Use it before pasting
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

[CONTRIBUTING.md](CONTRIBUTING.md) has the rules for adding a command, the
map from each contract to its code and test, and how to develop against a
local gedcom-go.

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
