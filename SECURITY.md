# Security Policy

Gleam is a **local-first** desktop AI agent. It runs entirely on your machine:
the Go backend serves a loopback API, the desktop shell renders it, and your
conversations, tasks, memory and audit logs stay in `~/.gleam` unless you point
them elsewhere. What leaves the machine is what you configure — requests to the
LLM endpoint whose API key you supplied, plus any MCP server or tool you enable.
That threat model is what the reports below are triaged against.

## Supported versions

| Version | Supported |
| --- | --- |
| The latest release (`releases/latest`) | Yes |
| Previous `0.0.x` releases | Best effort — critical fixes may be backported at maintainer discretion |
| Pre-releases and test tags (`*-test.N`) | No |
| Code built from a fork or a modified tree | No |

Fixes ship in the next release. Because the desktop shell updates itself in
place, the fastest route to a fix is installing the latest release.

## Reporting a vulnerability

**Do not open a public issue, discussion or pull request for a security
problem.** A public report is visible to everyone before there is a fix.

Preferred channels, in order:

1. **GitHub private security advisory** — on this repository:
   [Report a vulnerability](https://github.com/gleam-ai/Gleam/security/advisories/new).
   This keeps the whole conversation private, gives us a place to draft the fix
   and the advisory, and lets us credit you when it is published.
2. **Email** — `markgus0@proton.me`, subject `[SECURITY] <short title>`.
   Use this if you do not have a GitHub account or prefer mail.

Please include:

- **Affected version** — the version from the app (`Settings → About`) or a commit SHA;
- **Platform** — Windows / macOS / Linux, and whether you run the installer or a binary;
- **Impact** — what an attacker gains, and in what scenario;
- **Reproduction** — steps, or a proof of concept. Keep it minimal; do not run
  anything destructive against systems you do not own;
- **Exposure** — whether the issue is already public anywhere;
- **Disclosure preference and attribution** — whether you want to be credited.

If you are unsure whether something is a vulnerability, report it privately and
let us triage. A report we close as "not a vulnerability" costs you nothing.

## What to expect

| Step | Target |
| --- | --- |
| Initial acknowledgement | within **3 business days** |
| Triage: severity, affected versions, impact | within **7 business days** |
| Fix or mitigation plan shared | within **30 days** for High/Critical when feasible |
| Public disclosure | coordinated with you, after a fix or a mitigating release |

We will tell you if we cannot meet a target, and give a revised estimate rather
than going quiet. There is **no bug bounty** — this is an unsponsored project —
but we credit reporters (with consent) in the advisory and release notes.

## In scope

Reports are especially valuable when they involve:

- **Unexpected network egress** — anything leaving the machine that the user did
  not configure: the model transport, the egress ledger (`GET /api/connections`),
  MCP connectors, tool execution, update checks;
- **Privilege escalation** — escaping the permission gate, or a tool acting
  outside the access level the user granted it (the gate is the product promise:
  *what you approved is what runs*);
- **Sandbox and workspace escapes** — reading or writing outside the selected
  workspace root through a tool, the shell, a worktree, or a path in a config;
- **Credential handling** — API keys or the loopback API token written to disk,
  logs, crash output or a subprocess environment beyond documented behavior;
- **The local API surface** — the loopback server is reachable without the
  token, or a request can be made from a page other than the app's own origin;
- **Desktop shell** — preload/IPC allowlist escapes, arbitrary code execution
  from a rendered page, or a navigation/window-open guard bypass;
- **Supply chain** — anything in the release binaries, the bundled runtimes, or
  the GitHub Actions workflows.

## Out of scope

- **Unsigned binaries.** No code-signing certificate exists for this project, so
  Windows SmartScreen and macOS Gatekeeper warn on first run. That is documented
  in the README and in `docs/known-limits.md`, not a vulnerability.
- **Documented non-goals.** [`docs/known-limits.md`](docs/known-limits.md) is the
  single source of truth for deliberate limits (for example: no container or
  sandbox isolation around tools, lexical rather than semantic memory retrieval,
  `shell.exec` does not kill the process tree). A report that only restates a
  documented limit is not a vulnerability — but if you can show the limit
  producing a *worse* outcome than the document admits, that is in scope.
- **Third-party components you choose to run.** MCP servers, models, endpoints,
  scripts and packages you configure are outside our control. Report those to
  their maintainers, unless Gleam handles them in a way that creates the issue.
- **Anything requiring a compromised machine or account** — physical access,
  malware already running with your privileges, or a modified build.
- **Denial of service through intended resource use** — models cost money and
  long tasks take time. Volume-based abuse is not a vulnerability here.
- **Findings from an outdated release** that no longer reproduce on
  `releases/latest`. Please re-test before reporting.

## How Gleam handles your data

These are the properties we intend to keep, so a report that breaks one is a bug:

- **No telemetry, no analytics, no phone-home.** The only outbound calls are the
  ones your configuration implies.
- **The local API is token-guarded.** The token is written to
  `<data-dir>/webui.token`, sent by the shell on every proxied request, and never
  exposed to the rendered page.
- **The permission gate runs before execution**, and the audit log records what
  was approved and why (`/api/connections`, task archives).
- **Imports are read-only until you apply them** — the scanner that looks for
  existing memories, skills and MCP configs on your machine only reports.

## Safe harbor

We will not pursue legal action against good-faith research that:

- avoids privacy violations, destruction of data, and disruption of service;
- keeps the details private until a fix is released and disclosure is coordinated;
- does not exploit the issue beyond what is needed to demonstrate impact;
- does not access, modify or exfiltrate data belonging to anyone else.

If in doubt about whether your testing crosses a line, ask us first.

## Related

- [Code of Conduct](./CODE_OF_CONDUCT.md) — for conduct, not security.
- [Contributing](./CONTRIBUTING.md) — how to build and test Gleam locally.
- [`docs/known-limits.md`](docs/known-limits.md) — the deliberate limits of this release.
