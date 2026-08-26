# Security Policy

## Reporting a vulnerability

Please report security issues privately rather than opening a public issue.

Use GitHub's [private vulnerability reporting](https://github.com/0xquark/ctos/security/advisories/new),
or email the maintainer listed on the GitHub profile for [@0xquark](https://github.com/0xquark).

Include what you found, how to reproduce it, and what an attacker could do with it. You can
expect an acknowledgement within a week.

## Scope

ctOS reads YAML from your config directory, runs commands you configure, and — from v0.2 —
connects to hosts you name over SSH. Things we consider vulnerabilities:

- Config values escaping into a shell in a way that executes unintended commands.
- Secrets (tokens, key material) written to logs, error messages, or the terminal.
- Remote command output escaping its widget frame to control the terminal.
- Any privilege or credential use beyond what the config asks for.

Things we do not: ctOS running a command you explicitly configured it to run, or reading a file
you explicitly pointed it at.

## Handling secrets

Never put a literal token in a dashboard file. Use `${VAR}` expansion so the value comes from
your environment and the file stays safe to share:

```yaml
token: ${GRAFANA_TOKEN}
```
