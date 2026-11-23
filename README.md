# repo-template

Internal template repository seeding new repos with baseline config:

- `.github/dependabot.yml` — weekly GitHub Actions updates with `ci(deps)` commit prefix
- `.editorconfig` — universal whitespace defaults
- `.gitignore` — OS/editor noise (add language-specific entries per repo)
- `LICENSE` — MIT (change per repo if needed)

Used when new repos opt in via `from_template = true`. Replace this README
in a new repo's first commit: it is copied verbatim, and repos seeded from
this template are often public.
