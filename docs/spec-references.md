# GEDCOM specification references

CONSTITUTION.md principle 7: where the GEDCOM specification prescribes, the
tool follows it; where it offers a choice, is lenient or is silent, the tool
states its own policy. This file quotes the passages a policy rests on, so
the reasoning can be checked without rereading the specifications. Quotes are
verbatim. gedcom-go carries the machine-readable grammar; the full texts are
at the links below.

| Version | Source |
| --- | --- |
| 5.5.1 | [GEDCOM 5.5.1](https://gedcom.io/specifications/ged551.pdf), © The Church of Jesus Christ of Latter-day Saints: "This document may be copied for purposes of review or programming of genealogical software, provided this notice is included. All other rights reserved." |
| 7.0 | [FamilySearch GEDCOM 7.0.18](https://gedcom.io/specifications/FamilySearchGEDCOMv7.html) (17 February 2026), Copyright 1984–2026 Intellectual Reserve, Inc., licensed under the Apache License, Version 2.0 |

## Header source: `HEAD.SOUR` and `HEAD.SOUR.DATA`

Applied by: the header stamp of `combine`, `extract` and `--stamp` (README
"Conventions").

### 5.5.1

`HEADER` grammar (p. 23), the `SOUR` lines:

```text
  n HEAD                                       {1:1}
     +1 SOUR <APPROVED_SYSTEM_ID>              {1:1}
        +2 VERS <VERSION_NUMBER>               {0:1}
        +2 NAME <NAME_OF_PRODUCT>              {0:1}
        +2 CORP <NAME_OF_BUSINESS>             {0:1}
           +3 <<ADDRESS_STRUCTURE>>            {0:1}
        +2 DATA <NAME_OF_SOURCE_DATA>          {0:1}
           +3 DATE <PUBLICATION_DATE>          {0:1)
           +3 COPR <COPYRIGHT_SOURCE_DATA>     {0:1)
              +4 [CONT|CONC]<COPYRIGHT_SOURCE_DATA> {0:M}
```

> The header structure provides information about the entire transmission.
> The SOURce system name identifies which system sent the data. (p. 24)

> `APPROVED_SYSTEM_ID`: A system identification name which was obtained
> through the GEDCOM registration process. This name must be unique from any
> other product. (p. 42)

> `NAME_OF_PRODUCT`: The name of the software product that produced this
> transmission. (p. 54)

> `NAME_OF_SOURCE_DATA`: The name of the electronic data source that was used
> to obtain the data in this transmission. For example, the data may have been
> obtained from a CD-ROM disc that was named "U.S. 1880 CENSUS CD-ROM vol. 13."
> (p. 54)

> `VERSION_NUMBER` {Size=1:15}: An identifier that represents the version
> level assigned to the associated product. (p. 64)

### 7.0

`HEADER` (§3.2.1), the `SOUR` lines:

```text
  +1 SOUR <Special>                        {0:1}  g7:HEAD-SOUR
     +2 VERS <Special>                     {0:1}  g7:VERS
     +2 NAME <Text>                        {0:1}  g7:NAME
     +2 CORP <Text>                        {0:1}  g7:CORP
        +3 <<ADDRESS_STRUCTURE>>           {0:1}
        +3 PHON <Special>                  {0:M}  g7:PHON
        +3 EMAIL <Special>                 {0:M}  g7:EMAIL
        +3 FAX <Special>                   {0:M}  g7:FAX
        +3 WWW <Special>                   {0:M}  g7:WWW
     +2 DATA <Text>                        {0:1}  g7:HEAD-SOUR-DATA
        +3 DATE <DateExact>                {0:1}  g7:DATE-exact
           +4 TIME <Time>                  {0:1}  g7:TIME
        +3 COPR <Text>                     {0:1}  g7:COPR
```

> The header pseudo-structure provides metadata about the entire dataset.
> … `SOUR` describes the originating software. … `HEAD.SOUR.DATA` describes
> a larger database, electronic data source, or digital repository this data
> is extracted from. … `HEAD.SOUR.DATA` is now deprecated and applications
> should use `HEAD.SOUR.NAME` instead. (§3.2.1)

> `SOUR` (Source) `g7:HEAD-SOUR`: An identifier for the product producing
> this dataset. A registration process for these identifiers existed for a
> time, but no longer does. If an existing identifier is known, it should be
> used. Otherwise, a URI owned by the product should be used instead. (§3.3.4)

> `DATA` (Data) `g7:HEAD-SOUR-DATA`: The database, electronic data source, or
> digital repository from which this dataset was exported. (§3.3.4)

### Merging files

Neither version says what a header should hold when files are combined. 5.5.1
does not mention merging. 7.0 mentions it only for records: "a pair of FAM
with same spouses might also be the result of merging multiple files" (§3.3.4,
`FAM`).

### What this settles, and the policy where it does not

- **Prescribed:** `HEAD.SOUR` names the product that wrote the file, so a file
  this tool creates (`combine`, `extract`) names `gedcom-cli`. `DATA` names
  the data source the transmission was taken from, at most once.
- **Prescribed (7.0):** `HEAD.SOUR.DATA` is deprecated, so a 7.0 stamp writes
  no `DATA`. The suggested replacement, `HEAD.SOUR.NAME`, already names the
  product, so the input's system is not written there either.
- **Policy (silent spec):** a 5.x stamp keeps one step of lineage in `DATA`:
  the input's own `SOUR.DATA` with its subtree when it has one, else the
  input's `SOUR` value. `combine` uses the first file's header; later files'
  `SOUR` structures are listed in the JSON report's `header_conflicts`. Every
  report gives the replaced `SOUR` as `stamped_from`.
- **Policy (silent spec):** `DEST`, `DATE` and `FILE` describe the input's
  export, so a new dataset drops them; an edit (`--stamp`) keeps them.
