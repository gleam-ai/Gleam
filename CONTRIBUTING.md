# Contributing to Gleam

Thanks for helping. Gleam is a local-first desktop AI agent: a Go backend (single
binary, zero third-party Go dependencies) plus a desktop shell, with a web UI
compiled into the backend. Conversations, tasks, memory and audit logs stay on
the user's machine; only the model endpoint you configure is contacted.

Org-wide expectations — branch names, Conventional Commits, review etiquette —
live in the [gleam-ai contributing guide](https://github.com/gleam-ai/.github/blob/main/CONTRIBUTING.md).
This file is the Gleam-specific part: how the repository is laid out, how to
build it, and what has to pass before a change can land.

## Ground rules

- **Code of Conduct**: [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md) applies to every
  issue, pull request, review and discussion.
- **Security**: never open a public issue for a vulnerability — follow
  [SECURITY.md](./SECURITY.md).
- **Language**: commit messages, pull requests, release notes and these community
  documents are written in **English**. Code and code comments are **Chinese**,
  matching the existing codebase — keep a file's existing language rather than
  translating it.
- **No CLA.** Contributions are accepted under the repository's license
  ([MIT](./LICENSE)); signing off each commit is enough (see below).

## Repository layout

| Path | What lives there |
| --- | --- |
| `cmd/gleam/` | CLI entry point: `app`, `serve`, `webui`, `desktop-sidecar`, `goal`, `tools`, `rules`, `skills`, `schedule`, `memory`, `pending`, `doctor`, `replay`, `eval`, `mcp-fake-server`, `version` |
| `internal/agent/` | Plan–execute–reflect loop, permission gate, audit log |
| `internal/harness/` | Conversations, memory, skills, tools registry, feedback |
| `internal/tools/` | Built-in tools (file, shell, HTTP, git, MCP, …) |
| `internal/webui/` | Loopback HTTP API, SSE stream, and `static/` — the web UI, embedded with `go:embed` |
| `internal/desktop/`, `internal/systray/` | Go lightweight desktop shell (browser in `--app` mode plus a tray) |
| `internal/worktree/`, `internal/market/`, `internal/eval/`, `internal/llm/` | Worktrees, market sources, evaluation harness, model transport |
| `pkg/types/` | Shared types |
| `desktop/` | Electron shell (TypeScript, `src/main`, `src/preload`) and its build scripts |
| `website/` | The public download page |
| `docs/` | `known-limits.md` (the deliberate-limits contract) and engineering notes |
| `scripts/` | Verification gates, smoke tests, packaging |

Two desktop paths exist and are easy to mix up: the **Electron shell** in
`desktop/` (the Windows installer) and the **Go lightweight desktop** built from
`internal/desktop` + `internal/systray`. The web UI in `internal/webui/static`
is shared: it runs unchanged in a browser, in the Electron shell, and in the Go
desktop build.

## Prerequisites

- **Go 1.22+** (see `go.mod`)
- **Node.js** — only for the Electron shell in `desktop/`
- **Python 3** — for `scripts/check-*.py` and the mutation batches
- **Git Bash** (or any POSIX shell) on Windows, for `scripts/*.sh`
- **SSH commit signing** if you push to protected branches (see below)

## Build and test

```bash
# Format, vet, build, test — the four layers you will run constantly
gofmt -l ./internal ./pkg ./cmd        # must print nothing
go vet ./...
go build ./...
go test ./... -count=1 -timeout 300s

# The whole gate: six layers, cheapest first, non-zero exit on failure
bash scripts/verify.sh            # full
bash scripts/verify.sh --quick    # skips the slow end-to-end layers
```

`scripts/verify.sh` is the authoritative gate. Its layers, in order: format and
`§`-references → `go vet` → build → tests → end-to-end smoke on the real binary →
readiness (`doctor` + eval baseline). A pre-step checks the harness itself, because
a broken gate is worse than no gate.

Run the CLI from source:

```bash
go run ./cmd/gleam doctor      # is this machine ready?
go run ./cmd/gleam webui       # loopback API + web UI; prints the token file path
go run ./cmd/gleam version     # the version owner, see below
```

### Gates that guard the documentation

Several checks read the repository's own prose, so documentation is not optional
plumbing:

- `check-api-docs.py` — every API route must appear in the `README.md` endpoint
  table, and the table must not list routes that do not exist (bidirectional);
- `check-dom-anchors.py` — every id referenced from `internal/webui/static/*.js`
  must exist in `index.html`;
- `check-version-owner.py` — the build version equals the single version literal on
  the website, and the published date matches the newest `dist/SHA256SUMS-<date>.txt`;
- `check-doc-budget.py`, `check-doc-refs.py`, `check-cue-owner.py`,
  `check-egress-owner.py`, `check-atomic-write.py`, `check-app-startup.py`,
  `check-py-utf8.py`, `check-mutation-anchors.py` — the rest of the set.

Two rules follow from this and are easy to violate:

1. **The version has exactly one owner** — `internal/buildinfo/buildinfo.go`.
   Everything else (the app, the website badge, release tags) reads from it or is
   checked against it. Do not hardcode a version anywhere else.
2. **`docs/known-limits.md` is the single source of truth for deliberate
   non-goals.** If you implement something the document says is out of scope, or
   change a decision it records, update the document and the matching gate in the
   same change — and say so in the pull request.

### Mutation batches

`scripts/mutation/batch-*.py` deliberately break the code and check that a test
or gate catches it; a surviving mutant means the guard does not work. They
rewrite source files temporarily and restore them, so **do not run other gates
concurrently** with a batch, and confirm a clean tree afterwards:

```bash
git diff | sha256sum     # before and after a batch run
```

## Working on the desktop shell

```bash
cd desktop
npm install
npm run build          # sidecar (Go, includes the web UI) + TypeScript
npm run typecheck      # fast feedback on the shell alone
npm run dev            # build and launch the shell
npm run pack:win       # Windows NSIS installer (writes outside the workspace, see below)
```

Things worth knowing before you touch the shell:

- **`internal/webui/static` is embedded into the Go binary** (`go:embed`). Editing
  the web UI changes nothing until the sidecar is rebuilt: run `npm run build`
  (or `npm run build:sidecar`) and restart.
- **Packaging writes to the system temp directory**, not into the repository. A
  file indexer watching the workspace can hold a freshly written artifact and make
  electron-builder fail with `EBUSY`; keeping the output outside the workspace
  removes the whole class of failure.
- **Closing the window does not quit.** The shell keeps a tray icon; ✕ hides the
  window and the app stays resident, and quitting goes through the tray menu.
- **Verification recipe.** To exercise the real shell without touching your own
  data, start a bypass instance with an isolated profile and data directory:

  ```bash
  cd desktop
  GLEAM_DESKTOP_USER_DATA=<tmp>/profile \
  GLEAM_NO_SPLASH=1 \
  GLEAM_SIDECAR_ARGS="--data-dir <tmp>/data --mock-llm" \
  npx electron . --remote-debugging-port=9222
  ```

  The API token is at `<tmp>/data/webui.token`. Drive the rendered window over
  CDP (`http://127.0.0.1:9222/json`) rather than taking screen captures, and use
  trusted input events (`Input.dispatchMouseEvent`) for interactions that rely on
  pointer capture. Other dev knobs: `GLEAM_TRAY_ACTION` replays tray menu actions
  so that path can be exercised without clicking a native menu, and
  `GLEAM_SIDECAR_BIN` points at another sidecar binary.

## Tests

- Behaviour changes need tests, and tests run with `-count=1` (no cached results).
- Prefer tests that exercise the real path — the repository's own gates run the
  built binary against real files rather than mocking.
- **Some Windows tests are known to flake** (temp-file rename races in
  `internal/webui`: `TestGuard_*`, `TestGoalDeleteRemovesArchive`). They pass on a
  re-run and fail on a clean worktree at an older commit too. Re-run before
  assuming your change caused it, and say which tests flaked in the pull request.
- CI puts `TEMP` inside the checkout, so any fixture that needs to live *outside*
  the repository must say so explicitly (`os.UserCacheDir()`).

## Commits, branches and pull requests

- **Conventional Commits**, English, with a scope when it helps:
  `feat(desktop): add a tray icon with a right-click menu`.
- **Sign every commit.** Protected branches require verified signatures:

  ```bash
  git -c gpg.format=ssh -c user.signingkey=~/.ssh/<your-signing-key>.pub \
      -c commit.gpgsign=true commit -s -m "feat(scope): description"
  ```

  The `-s` flag adds a `Signed-off-by` line, certifying you have the right to
  submit the work under the project license (Developer Certificate of Origin).
  Configure it per command rather than globally if you sign for several projects.
- **Branch**: cut from `master`, name it after the change
  (`feat/…`, `fix/…`, `docs/…`, `chore/…`), keep one concern per pull request,
  and delete the branch once it is merged.
- **`master` is protected**: no merge commits — merges are squash (or rebase) only,
  the pull request title must satisfy the Conventional-Commits check, and required
  checks must be green. Keep the branch linear.
- **Small changes (a single fix) may go straight to `master`** if you are a
  maintainer, signed as above; anything larger goes through a pull request.
- In the pull request: link the issue (`Closes #N`), describe what changed and
  why, list how you verified it, and call out anything you did not verify or
  deliberately left out.

## Release flow

1. Change the version in its single owner (`internal/buildinfo/buildinfo.go`) plus
   `desktop/package.json` and the website badge, then update `CHANGELOG.md`.
2. `bash scripts/package.sh` — Go binaries for every platform, dated copies,
   release archive, `SHA256SUMS-<date>.txt`.
3. `cd desktop && npm run pack:win` — the Windows installer, plus `latest.yml` and
   the blockmap that the in-app updater reads.
4. Publish the release: tag `v0.0.N`, title `Gleam 0.0.N`, English notes, and the
   assets attached — including `latest.yml` and the blockmap, without which
   auto-update silently does nothing.

Asset names carry no version number (`Gleam-Setup.exe`), so the website can link
`releases/latest/download/<name>` and stay correct across versions.

## Getting help

- Bugs and feature requests: use the issue templates.
- Questions and ideas: [Discussions](https://github.com/gleam-ai/Gleam/discussions).
- Support expectations: [SUPPORT.md](https://github.com/gleam-ai/.github/blob/main/SUPPORT.md).
- Security: [SECURITY.md](./SECURITY.md) — privately, never in an issue.
