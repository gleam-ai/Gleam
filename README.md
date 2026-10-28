<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="website/favicon.svg">
  <img width="64" src="website/favicon.svg" alt="Gleam">
</picture>

# Gleam 微光

**Your local-first AI agent — plans, executes, and reflects.**

[![Release](https://img.shields.io/github/v/release/gleam-ai/Gleam?label=version&color=22c55e)](https://github.com/gleam-ai/Gleam/releases)
[![License](https://img.shields.io/github/license/gleam-ai/Gleam?color=22c55e)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev/)

[Download](https://github.com/gleam-ai/Gleam/releases/latest) · [Website](https://gleam.wangjn.top) · [Docs](docs/)

</div>

---

Gleam is not a chatbot. It's an agent that **plans tasks, executes them with tools, then reflects on the results** — all while keeping your data local.

Built from scratch in Go with **zero third-party dependencies**, the core is a single **~10 MB** binary. The desktop shell (Electron) wraps it into a double-click installer with bundled Node and uv runtimes.

<img src="artifacts/gleam-desktop-2026-10-23.png" alt="Gleam Desktop" width="100%">

## What you get

- **Plan → Execute → Reflect** — DAG-based task execution with 0–100 scoring and automatic replanning on low scores
- **Local-first** — conversations, memory, tasks, and audit logs stay on your machine by default
- **9 LLM providers** — GLM, DeepSeek, Kimi, Qwen, OpenAI, Anthropic, OpenRouter, and more, with multi-model tier routing
- **27 built-in tools** — file, shell, web, git, desktop integration, memory, scheduling — plus MCP for unlimited extensions
- **7 expert roles** — analyst, engineer, PM, researcher, ops, creator, or bring your own
- **Skill system** — successful executions crystallize into reusable, versioned skills with success-rate tracking
- **Token efficient** — compact schemas, exponential backoff, role prompt optimization: **55% less token usage**

## Quick start

**Windows (recommended):** Download `Gleam-Setup.exe` from [Releases](https://github.com/gleam-ai/Gleam/releases/latest) — one-click install, no setup needed.

**CLI (all platforms):**

```bash
# Download from releases, then:
./gleam app                                    # Desktop window
./gleam goal "organize my markdown files"      # Run a task
./gleam goal "summarize this repo" --mock-llm  # Offline demo, no API key
```

Connect a real model by setting your API key in the settings page or via environment variable:

```bash
export GLEAM_API_KEY=your-key
./gleam goal "list and summarize recent git changes"
```

## Architecture

```
cmd/gleam/          CLI entry points (app / serve / goal / webui / desktop-sidecar)
internal/agent/     Plan-Execute-Reflect engine
internal/tools/     File, shell, web, git, desktop, MCP client
internal/llm/       Multi-protocol client (OpenAI Chat / Responses / Anthropic)
internal/webui/     HTTP REST + SSE + embedded frontend (i18n)
internal/harness/   Memory, safety gate, scheduler, skills, worktrees
desktop/            Electron shell (tray, auto-update, bundled runtimes)
```

## Local HTTP API

The desktop window and the CLI drive the embedded web UI over a local HTTP API (bound to `127.0.0.1`, token-gated). This list is the **owner of user-visible endpoints**: `scripts/check-api-docs.py` fails the build when a route is registered in `internal/webui` but missing here — or listed here but not registered.

<details>
<summary>All 102 endpoints</summary>

- **Account** — `/api/account` · `/api/account/configure` · `/api/account/oauth` · `/api/account/signin` · `/api/account/signout` · `/api/account/signup`
- **Approvals** — `/api/approvals` · `/api/approvals/{id}`
- **Connections** — `/api/connections`
- **Context** — `/api/context` · `/api/context/clear` · `/api/context/compress`
- **Conversations** — `/api/conversation/reset` · `/api/conversations` · `/api/conversations/{id}` · `/api/conversations/{id}/activate`
- **Cues** — `/api/cues` · `/api/cues/{id}/adopt` · `/api/cues/{id}/dismiss` · `/api/cues/unsuppress`
- **Daemon** — `/api/daemon`
- **Events (SSE)** — `/api/events`
- **Feedback** — `/api/feedback` · `/api/feedback/{id}` · `/api/feedback/{id}/resend` · `/api/feedback/attachment` · `/api/feedback/context`
- **Files** — `/api/fs`
- **Geo** — `/api/geo` · `/api/geo/analyze` · `/api/geo/history`
- **Goals** — `/api/goals` · `/api/goals/{id}` · `/api/goals/{id}/cancel` · `/api/goals/{id}/diff` · `/api/goals/{id}/revert`
- **CLI status** — `/api/go-status` · `/api/go-status/install`
- **Growth** — `/api/growth` · `/api/growth/recent`
- **Hardware** — `/api/hardware/configure-wifi` · `/api/hardware/connect` · `/api/hardware/devices` · `/api/hardware/disconnect` · `/api/hardware/info/{id}` · `/api/hardware/instances` · `/api/hardware/receive` · `/api/hardware/scan-wifi` · `/api/hardware/send`
- **Hooks** — `/api/hooks/{name}`
- **Heartbeat** — `/api/heartbeat`
- **Import** — `/api/import/apply` · `/api/import/scan`
- **Info** — `/api/info`
- **LLM** — `/api/llm/models` · `/api/llm/test`
- **Local data** — `/api/local-data`
- **Market** — `/api/market/mcp` · `/api/market/mcp/install` · `/api/market/runtimes` · `/api/market/skills` · `/api/market/skills/install` · `/api/market/sources`
- **MCP** — `/api/mcp` · `/api/mcp/{name}` · `/api/mcp/{name}/enabled` · `/api/mcp/{name}/reconnect`
- **Memory** — `/api/memory` · `/api/memory/{id}`
- **Models** — `/api/models` · `/api/models/{id}` · `/api/models/{id}/activate` · `/api/models/{id}/default`
- **Network** — `/api/network`
- **Onboarding** — `/api/onboarding`
- **Providers** — `/api/providers`
- **Readiness** — `/api/readiness`
- **Roles** — `/api/roles`
- **Schedules** — `/api/schedules` · `/api/schedules/{name}` · `/api/schedules/{name}/enabled` · `/api/schedules/{name}/notify`
- **Security** — `/api/security/audit`
- **Settings** — `/api/settings`
- **Show window** — `/api/show-window`
- **Skills** — `/api/skills` · `/api/skills/{name}` · `/api/skills/{name}/enabled` · `/api/skills/{name}/run`
- **Spaces** — `/api/spaces` · `/api/spaces/{id}` · `/api/spaces/{id}/activate`
- **SSH** — `/api/ssh/hosts`
- **Tools** — `/api/tools` · `/api/tools/call` · `/api/tools/permission`
- **Update** — `/api/update/apply` · `/api/update/check` · `/api/update/desktop/{name}`
- **Workspace** — `/api/workspace` · `/api/workspace/clear`
- **Worktrees** — `/api/worktrees` · `/api/worktrees/{id}`

</details>

## Platform support

| Platform | Format | Notes |
|----------|--------|-------|
| **Windows** | NSIS installer (~127 MB) | Bundled Node + uv, tray icon, auto-update |
| **Windows / macOS / Linux** | Single binary (~10–16 MB) | CLI + embedded web UI |

Unsigned binaries — macOS Gatekeeper and Windows SmartScreen will prompt once on first run.

## Contributing

PRs welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for commit conventions and local dev setup.

## License

[MIT](LICENSE) · Built with Go, running locally.
