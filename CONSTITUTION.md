# Constitution

> The mission, principles, and non-goals of gedcom-cli. When in conflict with this document, future decisions should align here or explicitly update it.

## Mission

`gedcom` lets a person or a coding agent inspect, audit and correct GEDCOM files from the command line, without loading the file into context or writing a program. It exists so that genealogy research on real exports can be done in small, reviewable steps, and so that [gedcom-go](https://github.com/cacack/gedcom-go) has a consumer exercising its read, validate and write paths on real files.

## Audience

**This is for:** researchers who keep GEDCOM files under version control and work on them at a terminal, with shell scripts, or through a coding agent.
**This is not for:** people who want a genealogy application: tree views, a database, media management, publishing. Use [my-family](https://github.com/cacack/my-family) or a desktop program.

## Principles

When in doubt, prefer:

1. **Both readers over either one** — every command serves a person at a terminal and a coding agent equally: readable text by default, stable JSON on request, the same facts in each. A feature only one of them can use is not finished.
2. **A minimal diff over a tidy file** — an edit changes the bytes it was asked to change and nothing else, so that version control shows the correction and only the correction. Tidying is never a side effect.
3. **Explicit edits and named transforms over inferred fixes** — the tool applies the change it is told to make, or a deterministic transform the user asked for by name (strip these tags, normalize line endings). It reports problems; deciding what is true is the researcher's job.
4. **The library's logic over our own** — commands wrap gedcom-go. A missing capability or a bug becomes an upstream issue, not a local workaround.
5. **The input is never touched** — writes go to a new file, and nothing leaves the machine: no network calls, no telemetry. The files describe living people.
6. **Few commands that compose over many that overlap** — `list --format json | jq` beats a search flag.
7. **The specification where it prescribes, a stated policy where it does not** — where the GEDCOM specification is prescriptive, the tool does what it says. Where it offers a choice, is lenient, or is silent, the tool picks one policy, states it in the help, and applies it the same way in every command.

## Non-Goals

This project is explicitly **not** trying to:

- be a genealogy application or hold state between runs: no database, no index, no config file
- judge genealogical truth: no automatic merging of people, no heuristic "fix everything"
- manage media files; it reports and edits the references inside the GEDCOM only
- offer a second interface, such as an MCP server; the CLI is the interface
- match other GEDCOM tools' query languages, HTML publishing or diff tooling feature for feature; a record-level diff of two versions of a file is in scope, parity with other diff tools is not

## Success Criteria

We'll know this is working if:

**C1 — The build is healthy.**
Check: `make check` exits 0 on `main`

**C2 — Edits are minimal: a file passed through any write command with no change requested comes back byte-identical.**
Check: `go test ./internal/cli -run TestRoundTripByteIdentical` exits 0

**C3 — Every command explains itself: its help carries a summary and a runnable example.**
Check: `go test ./internal/cli -run TestCommandsDocumented` exits 0

**C4 — The work can be done with the tool alone.**
Check: judgment call — review sessions in the repos that use `gedcom` for tasks where the person or agent fell back to grepping raw GEDCOM or writing a one-off program. Each is a missing command; no test can see work that happened outside the tool.

**C5 — Gaps flow upstream.**
Check: judgment call — reviewers confirm per pull request that `internal/cli` holds no GEDCOM parsing or rewriting logic of its own. What counts as "logic" versus "wrapping" needs a reader.

---

*Last refreshed: 2026-11-25*
