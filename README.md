# Thymer Inbox

A plugin and CLI for getting data from the outside world into [Thymer](https://thymer.com). Turn Thymer into an event sink for your digital life.

## Currently Implemented

- **Markdown paste** - Pipe any markdown from the terminal into your Thymer Journal
- **GitHub sync** - Automatically sync issues and PRs from your repos into Thymer
- **Readwise sync** - Sync your highlights from Readwise Reader into Thymer
- **tm CLI** - Command-line interface to push content to Thymer

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│  OUTSIDE WORLD                                                  │
│  • tm CLI (echo "note" | tm)                                    │
│  • GitHub API (issues, PRs)                                     │
│  • Readwise Reader API (highlights)                             │
└──────────────────────────┬──────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  tm serve (local Go server)                                     │
│  • Queues incoming content                                      │
│  • Polls GitHub every minute, Readwise every hour               │
│  • Serves SSE stream to browser                                 │
└──────────────────────────┬──────────────────────────────────────┘
                           │ SSE (browser connects out, server pushes in)
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  Thymer Plugin (App Plugin)                                     │
│  • Receives content via SSE                                     │
│  • Parses markdown into Thymer's block format                   │
│  • Routes to collections based on frontmatter                   │
│  • Adds timestamped entries to Journal                          │
└─────────────────────────────────────────────────────────────────┘
```

The browser can't be a server, but it can hold a connection open. The plugin opens an SSE connection to `tm serve` and waits. Content gets pushed through the open pipe.

## Quick Start

### 1. Build the CLI

```bash
cd cmd/tm && go build -o ../../tm . && cd ../..
```

### 2. Configure

Create `~/.config/tm/config`:

```
url=http://localhost:19501
token=local-dev-token

# Optional: GitHub sync
github_token=ghp_xxxxxxxxxxxx
github_repos=owner/repo1,owner/repo2

# Optional: Readwise sync
readwise_token=xxxxxxxxxxxx
```

### 3. Install the Plugins

There are **two plugins** to install:

#### App Plugin (required)

Handles SSE connection, markdown parsing, and content routing.

1. Open Thymer → Command Palette (Cmd/Ctrl+P) → "Plugins"
2. Create a new **App Plugin**
3. Paste `plugin/plugin.js` into the Code tab
4. Paste `plugin/plugin.json` into the Configuration tab
5. Save and enable

Status bar shows `🪄 ●` (green = connected, red = disconnected).

#### Collection Plugins (optional, for sync features)

Define custom fields for synced content.

**GitHub Collection:**
1. In Thymer, create a new **Collection Plugin**
2. Paste `plugin/github-collection.json` into the Configuration tab
3. Save - this creates a "GitHub" collection with issue/PR fields

**Readwise Collection:**
1. Create another **Collection Plugin**
2. Paste `plugin/readwise-collection.json` into the Configuration tab
3. Save - this creates a "Readwise" collection with highlight fields

### 4. Start the Server

```bash
./tm serve
```

Output:
```
🪄 Thymer queue server on http://localhost:19501
   Token: local-dev-token
📡 GitHub sync enabled for: owner/repo1, owner/repo2
```

### 5. Test It

```bash
echo "Hello from the terminal!" | tm
```

You should see a new line in today's Journal: `15:21 Hello from the terminal!`

## CLI Usage

```
tm - Thymer queue CLI

Usage:
  cat file.md | tm                    Push markdown to Thymer
  echo 'note' | tm                    Push text to Thymer
  tm lifelog Had coffee with Alex     Push lifelog entry
  tm --collection 'Tasks' < todo.md   Push to specific collection
  tm serve                            Run local queue server
  tm resync [repo|readwise]           Clear sync cache and resync
  tm readwise-sync                    Trigger Readwise sync now

Options:
  --collection, -c    Target collection name
  --title, -t         Record title
  --action, -a        Action type (append|lifelog|create)
  --help, -h          Show help
```

## Smart Content Routing

The plugin automatically routes content based on its structure:

| Content Type | Behavior |
|-------------|----------|
| **One-liner** | Appends to Journal with timestamp: `15:21 Quick thought` |
| **2-5 lines** | First line becomes timestamped parent, rest are children |
| **Markdown doc** (starts with `# `) | Creates note in Inbox, adds reference to Journal |
| **Lifelog** (`tm lifelog ...`) | Adds bold timestamped entry: `**15:21** Had coffee` |
| **Frontmatter** | Routes to specified collection, matches properties |

## Markdown Support

- Headings (H1-H6, proper sizing when Thymer API available)
- Bold, italic, inline code
- Bullet and numbered lists
- Task lists (`- [ ]` and `- [x]`)
- Blockquotes
- Fenced code blocks (syntax highlighting when Thymer API available)

## GitHub Sync

Automatically sync GitHub issues and PRs to Thymer.

### Setup

1. Create a [GitHub Personal Access Token](https://github.com/settings/tokens) with `repo` scope
2. Add to your config:
   ```
   github_token=ghp_xxxxxxxxxxxx
   github_repos=owner/repo1,owner/repo2
   ```
3. Install the Collection Plugin (`plugin/github-collection.json`)
4. Start `tm serve`

### How It Works

- Polls GitHub every 1 minute for changes
- Uses `external_id` for deduplication (e.g., `github_owner_repo_123`)
- Computes dynamic verbs from state changes:
  - New issue → `opened`
  - Issue closed → `closed`
  - PR merged → `merged`
  - Other changes → `updated`
- Adds timestamped entries to Journal: `15:21 opened [[Issue Title]]`
- Stores sync state in `~/.config/tm/github.db` (bbolt)

### Resync

To force a full resync (e.g., after deleting issues):

```bash
tm resync              # Clear all GitHub cache
tm resync owner/repo   # Clear specific repo
# Then restart tm serve
```

## Readwise Sync

Automatically sync your Readwise Reader highlights to Thymer.

### Setup

1. Get your [Readwise Access Token](https://readwise.io/access_token)
2. Add to your config:
   ```
   readwise_token=xxxxxxxxxxxx
   ```
3. Install the Collection Plugin (`plugin/readwise-collection.json`)
4. Start `tm serve`

### How It Works

- Polls Readwise every 1 hour (strict API rate limits)
- Only syncs documents that have highlights (not all saved items)
- Each document becomes a record with:
  - LLM-generated summary (when available)
  - All highlights as blockquotes
  - User notes preserved
- First sync adds journal entry: `15:21 highlighted [[Article Title]]`
- Subsequent updates are silent (no journal spam)
- Stores sync state in `~/.config/tm/readwise.db` (bbolt)

### Manual Sync

```bash
tm readwise-sync       # Trigger sync now (via running server)
tm resync readwise     # Clear cache and resync from scratch
```

## Universal Frontmatter Interface

Any content with YAML frontmatter is automatically routed:

```yaml
---
collection: Reading
external_id: readwise_abc123
verb: highlighted
title: Article Title
author: John Doe
---

Article content here...
```

Key fields:
- `collection` (required): Target collection name
- `external_id`: For deduplication across syncs
- `verb`: Action for journal entry (added, updated, opened, closed, etc.)
- Other keys: Matched against collection properties

This makes it easy to integrate any source—the plugin doesn't care where content comes from.

## Running as a Service

For always-on availability:

```bash
# Install systemd user service
task service:install

# Start/stop
task service:start
task service:stop

# View logs
task service:logs
```

## Claude Code Integration

A skill for [Claude Code](https://claude.com/claude-code) that lets you capture notes using natural language.

### Install

```bash
task skill:install
```

### Usage

Just tell Claude what to capture:

- "note this conversation"
- "log that I finished the code review"
- "capture this as a meeting summary"
- "inbox this discussion"

Claude automatically picks the right `tm` pattern based on content.

### Uninstall

```bash
task skill:uninstall
```

## Project Structure

```
thymer-inbox/
├── cmd/tm/
│   ├── main.go           # CLI + local server
│   ├── github.go         # GitHub sync logic
│   └── readwise.go       # Readwise sync logic
├── plugin/
│   ├── plugin.js         # App Plugin (SSE, markdown, routing)
│   ├── plugin.json       # App Plugin config
│   ├── github-collection.json    # Collection Plugin (GitHub)
│   └── readwise-collection.json  # Collection Plugin (Readwise)
├── skill/
│   └── SKILL.md          # Claude Code skill for natural language capture
├── worker/               # Cloudflare Worker (needs update)
└── CLAUDE.md             # Instructions for Claude Code
```

## Requirements

- **Thymer**: Account with Journal collection (for daily entries)
- **Go 1.21+**: To build the CLI
- **Browser**: Chrome recommended (handles localhost CORS)

## Roadmap

See [GitHub Issues](https://github.com/riclib/thymer-inbox/issues) for planned features:
- MCP server for Claude Code integration
- Cloudflare Worker update

## License

MIT
