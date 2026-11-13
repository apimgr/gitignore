# Security Policy

## Supported Versions

Only the latest release of `gitignore` is supported with security updates. Older releases should be upgraded to the latest version before reporting an issue.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Older releases | No |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

The preferred way to report a vulnerability is through [GitHub's private vulnerability reporting](https://github.com/apimgr/gitignore/security/advisories/new).

If you are unable to use GitHub's private reporting, email security@apimgr.us. Please include a description of the vulnerability and its potential impact, steps to reproduce or a proof of concept, and the affected version or commit.

## Response Process

- We will acknowledge receipt within 48 hours.
- We will provide an initial assessment as soon as possible. Critical vulnerabilities are targeted for a fix within 14 days.
- We will coordinate a disclosure timeline with you once a fix is available.
- Credit will be given in the release notes unless you request otherwise.

## Additional Information

This project publishes a machine-readable security contact at `/.well-known/security.txt` and `/security.txt`.

The API intentionally serves public `.gitignore` templates without authentication. Reports about this documented behavior are out of scope.
