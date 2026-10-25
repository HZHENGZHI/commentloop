[![CI](https://github.com/HZHENGZHI/commentloop/actions/workflows/ci.yml/badge.svg)](https://github.com/HZHENGZHI/commentloop/actions/workflows/ci.yml)

# Commentloop

English | [简体中文](README.zh-CN.md)

Commentloop is a local-first review loop for Markdown documents and AI agents.

You read, comment, and review in the browser. Your agent edits through MCP or the CLI. Comments, before/after changes, consensus, and rollbacks stay attached to the same Markdown file.

```text
comment → agent edit → human review → reply / rollback → updated document
```

![Commentloop demo: comment, agent edit, review, resolve](docs/demo.gif)

## What you get

- **Inline review threads**: select text, leave a comment, and keep the thread anchored to the source.
- **Agent edits**: the agent reads the document, comments, and consensus; writes a focused change; then replies with before/after context.
- **Document consensus**: import sessions from Pi, Claude Code, Codex, or Cursor and distill them into structured consensus. Later edits can use it instead of re-litigating old decisions.
- **Code context**: link a document to local directories or GitHub repositories; agents can read code, Git state, and branches.
- **Local-first**: a single Go binary with an embedded React frontend; binds to `127.0.0.1` by default; comments and sessions live in SQLite.
- **Read-only browser**: existing documents cannot be edited in the browser. Agents write through MCP/CLI, with document and repository access enforced by the Go backend.

## Quick start

Requirements: Go 1.26+ and Node.js 22+ (Node is only needed to build the frontend).

You can also download a prebuilt macOS/Linux binary (amd64 or arm64) from [Releases](https://github.com/HZHENGZHI/commentloop/releases).

```bash
git clone https://github.com/HZHENGZHI/commentloop.git
cd commentloop
make build
```

Start with a notes directory:

```bash
./bin/commentloop ~/Documents/notes
```

The server starts at http://127.0.0.1:4571/ and opens the browser.

Multiple directories, or a single file:

```bash
./bin/commentloop ~/Documents/notes --add-dir ~/obsidian-vault --add-dir /Volumes/work/wiki
./bin/commentloop ~/Documents/notes/current-note.md
```

Manage the background process:

```bash
./bin/commentloop status
./bin/commentloop stop
```

Run in the foreground when you need logs or want launchd/systemd to manage it:

```bash
./bin/commentloop run --no-open ~/Documents/notes
```

### Your first review

1. Pick a document in the sidebar.
2. Select text and leave a comment.
3. Choose a task type; the agent edits the document and replies with before/after context.
4. Keep the discussion going, roll back a paragraph, or resolve the thread. Only the user can resolve a comment.

## Connect an agent

### MCP

Agents launch Commentloop over stdio with `commentloop mcp`:

```json
{
  "mcpServers": {
    "commentloop": {
      "command": "/Users/you/Desktop/coding/commentloop/bin/commentloop",
      "args": ["mcp"]
    }
  }
}
```

For Cursor, allow the MCP server in `~/.cursor/cli-config.json`:

```json
{
  "permissions": {
    "allow": ["Mcp(commentloop:*)"]
  }
}
```

Also consider denying Cursor's built-in `Shell(*)` and `Write(**)` so it cannot bypass Commentloop's path controls.

### Automatic runs

Open **Settings → Code Agent** and choose:

- **Embedded Pi**: Commentloop starts Pi directly.
- **External Agent Runner**: a runner on the host claims jobs and starts the local agent.

Start an external runner:

```bash
./bin/commentloop agent-runner --url http://127.0.0.1:4571 --agent-type pi
./bin/commentloop agent-runner --url http://127.0.0.1:4571 --agent-type claude-code
./bin/commentloop agent-runner --url http://127.0.0.1:4571 --agent-type codex
./bin/commentloop agent-runner --url http://127.0.0.1:4571 --agent-type cursor
```

Options: `--once` (handle one job and exit), `--command` (custom agent), and `--prompt-mode stdin`.

The runner injects a one-time `COMMENTLOOP_URL` / `COMMENTLOOP_AGENT_TOKEN` into the agent process. The agent must already be configured to use the Commentloop MCP server.

### CLI

```bash
./bin/commentloop agent info
./bin/commentloop agent list --refresh
./bin/commentloop agent read notes/todo.md
printf '# Hello\n' | ./bin/commentloop agent write notes/hello.md
./bin/commentloop agent search todo
./bin/commentloop agent changes --since 30m
```

## Configuration and data

Default state directory:

```text
macOS: ~/Library/Application Support/commentloop/
Linux: ~/.config/commentloop/
```

Files:

```text
commentloop.db    SQLite: roots, comments, sessions, consensus, jobs
commentloop.log   background log
server.json       URL / PID of the current instance
```

Markdown files stay ordinary Markdown. Comments and consensus are stored in SQLite and are not written into the document.

Common flags:

```bash
--addr 127.0.0.1:4571   # listen address
--state-dir DIR         # custom state directory
--no-open               # do not open a browser
--no-state              # do not read or write state
--add-dir DIR           # add another workspace directory
```

In the default review mode:

- Existing documents cannot be edited from the browser (`PUT /api/doc` returns 403).
- Creating, importing, and deleting documents still works.
- Agents write through MCP/CLI; the backend checks document paths, linked repositories, and task capabilities.

## Docker

```bash
make cross
docker build -t commentloop .

docker run -d --name commentloop \
  -p 127.0.0.1:4571:4571 \
  -v "$HOME/Documents/notes:/notes" \
  -v commentloop-data:/data \
  commentloop
```

The container runs `commentloop run --no-open --addr 0.0.0.0:4571 /notes` in the foreground. `/data` stores SQLite, logs, and discovery files.

## Development

```bash
make test          # Go tests + frontend build
make debug DIR=~/Documents/notes
make dev-backend   # terminal 1
make dev-web       # terminal 2
make cross         # macOS / Linux, amd64 + arm64
```

`make debug` watches the frontend and Go code: frontend changes refresh the page, backend changes restart the server and reconnect the page.

## Releases

Pushing a `v*` tag builds macOS/Linux binaries for amd64 and arm64, generates `.tar.gz` archives and `SHA256SUMS`, and creates a GitHub Release.

```bash
git tag v0.1.0
git push origin v0.1.0
```

## License

MIT
