# CLAUDE.md

`gedcom` CLI: a thin wrapper over the `github.com/cacack/gedcom-go/v3` library, for people and coding agents alike. Direction, principles and non-goals are in [CONSTITUTION.md](CONSTITUTION.md); read it before proposing a command.

The rules for adding or changing a command are in [CONTRIBUTING.md](CONTRIBUTING.md): layout, the contract → code → enforcing-test map, and developing against a local gedcom-go. Read it before touching `internal/cli/`; don't restate it here.

- **No GEDCOM logic here.** Commands wrap library calls. A missing capability or a library bug is an issue on `cacack/gedcom-go`, not a workaround in this repo.

**Releases:** release-please cuts them from conventional commits on `main`; only `feat`, `fix` and `perf` produce one (README "Development"). Never tag or edit `CHANGELOG.md` by hand. Put issue references (`Closes #N`, `Refs #N`) in the PR description, never in a commit message: release-please turns a commit's reference into "closes", and the release PR then closes the issue. PR titles are plain prose, never conventional commits (CI checks): the merge commit copies the title, and release-please would count it twice.

`make check` runs what CI runs (vet, race tests, gofmt).
