# Security Policy

## Supported Versions

Security fixes are released on the latest release only. Upgrade to the
[latest release](https://github.com/cacack/gedcom-cli/releases/latest) before
reporting.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for a security vulnerability.**

Report it privately through GitHub's private vulnerability reporting:
[open a private vulnerability report](https://github.com/cacack/gedcom-cli/security/advisories/new)
(the "Report a vulnerability" button on the repository's Security tab).

Please include:

1. What the vulnerability is and what it allows.
2. Steps to reproduce, ideally with a minimal GEDCOM file. Use synthetic data:
   do not attach a file that describes real people.
3. The affected version (`gedcom --version`) or commit.

## Scope

This repository is the `gedcom` command-line tool and its release binaries.
Reports are welcome for anything in it, in particular:

- `--redact-living` leaving data about a person presumed living in a command's
  output.
- A command writing to its input file, or outside the path it was given.
- The release and build pipeline in `.github/workflows/`.

GEDCOM parsing, encoding and validation live in the
[gedcom-go](https://github.com/cacack/gedcom-go) library; report a
vulnerability there under its
[security policy](https://github.com/cacack/gedcom-go/security/policy).
