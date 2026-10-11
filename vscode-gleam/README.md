# Gleam for VS Code

Run goals through the [Gleam](../README.md) agent engine without leaving the editor.

The extension starts `gleam serve` as a child process and speaks **JSON-RPC 2.0 over
newline-delimited JSON on stdio** — the same channel any editor can use (see
[`editor-plugin/gleam.plugin.json`](../editor-plugin/gleam.plugin.json) for the method
inventory). Nothing else is required: no daemon, no port, no cloud.

## Install for development

```bash
# from this directory
code --extensions-dir .tmp-extensions .
# or symlink it into your extensions folder if you prefer that workflow
ln -s "$(pwd)" ~/.vscode/extensions/gleam
```

`Gleam: Start engine` then launches the executable named by `gleam.command`
(`gleam` by default) with the arguments in `gleam.args`.

## What it gives you

| Command | What it does |
| --- | --- |
| `Gleam: Submit goal…` | Prompts for a goal; an active editor selection is attached as a `kind: "file"` reference |
| `Gleam: Explain selection` | Sends the selected code as the task and opens with a review prompt |
| `Gleam: Cancel task…` | Cancels one of the running tasks |
| `Gleam: Show task result…` | Dumps `goal/status` into a JSON document |
| `Gleam: Open web UI` | Opens the desktop web UI in your browser |
| `Gleam: Show output channel` | Raw engine stream, including every tool call |

The sidebar view lists recent tasks with their status, and the status bar shows
whether the engine is up.

## Safety

The engine never takes a high-risk step on its own. When one comes up it sends the
host a **`goal/ask_approval` request**, which this extension turns into a modal
dialog showing the risk level and the plan. Dismissing the dialog counts as a
rejection, so agents fail closed rather than proceeding unattended.

## Layout

| File | Responsibility |
| --- | --- |
| `src/rpc.js` | JSON-RPC framing, correlation and timeouts. No VS Code API — unit tested with `node --test test/rpc.test.js` |
| `src/extension.js` | Process lifecycle, UI surfaces, approval dialog |

Keep that split: everything worth testing should live in `src/rpc.js`.

## Settings

| Key | Default | Meaning |
| --- | --- | --- |
| `gleam.command` | `gleam` | Executable to launch |
| `gleam.args` | `["serve"]` | Arguments for the engine |
| `gleam.workingDirectory` | workspace root | Engine working directory |
| `gleam.requestTimeoutMs` | `30000` | Per-request timeout |
| `gleam.defaultMode` | `auto` | Execution mode for new goals |
| `gleam.uiUrl` | `http://127.0.0.1:8787` | Web UI address |
