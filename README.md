# Thymer Inbox

A CLI tool and Thymer plugin for capturing content from the command line directly into [Thymer](https://thymer.com).

## What It Does

Pipe any content from your terminal and it appears in your Thymer Journal:

```bash
# Quick thought
echo "Remember to call mom" | tm

# Multi-line note with hierarchy
echo -e "Meeting notes\n- Action item 1\n- Action item 2" | tm

# Lifelog entry (timestamped)
tm lifelog Had coffee with Alex

# Full markdown document → creates linked note
echo -e "# Project Ideas\n\n## Backend\n- API redesign\n- Caching layer" | tm
```

## Smart Content Routing

The plugin automatically routes content based on its structure:

| Content Type | Behavior |
|-------------|----------|
| **One-liner** | Appends to Journal with timestamp: `15:21 Quick thought` |
| **2-5 lines** | First line becomes timestamped parent, rest are children with full markdown parsing |
| **Markdown doc** (starts with `# `) | Creates note in Inbox collection, adds clickable reference to Journal |
| **Lifelog** (`tm lifelog ...`) | Adds bold timestamped entry: `**15:21** Had coffee` |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  CLI (tm)                                               │
│  echo "note" | tm                                       │
│  tm lifelog "message"                                   │
└──────────────────────────┬──────────────────────────────┘
                           │ POST /queue (JSON + timestamp)
                           ▼
┌─────────────────────────────────────────────────────────┐
│  Queue Server                                           │
│  • tm serve (local) ← recommended for personal use      │
│  • Cloudflare Worker (untested)                         │
└──────────────────────────┬──────────────────────────────┘
                           │ SSE /stream
                           ▼
┌─────────────────────────────────────────────────────────┐
│  Thymer Plugin                                          │
│  • Auto-connects on load (green dot = connected)        │
│  • Routes content to Journal or Inbox                   │
│  • Full markdown parsing with hierarchy                 │
└─────────────────────────────────────────────────────────┘
```

## Quick Start (Local Development)

### 1. Build the CLI

```bash
# Using Go directly
cd cmd/tm && go build -o ../../tm . && cd ../..

# Or with Task runner
task build
```

### 2. Start the Local Server

```bash
./tm serve
```

Output:
```
🪄 Thymer queue server on http://localhost:19501
   Token: local-dev-token
```

### 3. Configure the CLI

Create `~/.config/tm/config`:

```
url=http://localhost:19501
token=local-dev-token

# Optional: GitHub sync
github_token=ghp_xxxxxxxxxxxx
github_repos=owner/repo1,owner/repo2
```

Or set environment variables:
```bash
export THYMER_URL=http://localhost:19501
export THYMER_TOKEN=local-dev-token
export GITHUB_TOKEN=ghp_xxxxxxxxxxxx
export GITHUB_REPOS=owner/repo1,owner/repo2
```

### 4. Install the Thymer Plugin

1. Open Thymer → Command Palette (Cmd/Ctrl+P) → "Plugins"
2. Create a new **App Plugin**
3. Paste `plugin/plugin.js` into the Code tab
4. Paste `plugin/plugin.json` into the Configuration tab
5. Save and enable

The plugin auto-connects. Look for `🪄 ●` in the status bar:
- Green dot = connected
- Red dot = disconnected (click to retry)

### 5. Test It

```bash
echo "Hello from the terminal!" | tm
```

You should see:
- Terminal: `✓ Queued 27 bytes (append)`
- Thymer: New line in today's Journal with timestamp

## CLI Usage

```
tm - Thymer queue CLI

Usage:
  cat file.md | tm                    Push markdown to Thymer
  echo 'note' | tm                    Push text to Thymer
  tm lifelog Had coffee with Alex     Push lifelog entry
  tm --collection 'Tasks' < todo.md   Push to specific collection
  tm create --title 'New Note'        Create new record
  tm serve                            Run local queue server

Options:
  --collection, -c    Target collection name
  --title, -t         Record title (for create action)
  --action, -a        Action type (append|lifelog|create)
  --help, -h          Show help

Actions:
  append (default)    Append to Journal
  lifelog             Add timestamped lifelog entry
  create              Create new record in collection
```

## Requirements

- **Thymer**: You need a Thymer account with:
  - A "Journal" collection (for daily entries)
  - An "Inbox" or "Notes" collection (for markdown documents)
- **Go 1.21+**: To build the CLI
- **Browser**: Chrome recommended (handles localhost CORS properly)

## Running as a Service (Optional)

For always-on availability, run the server as a systemd user service:

```bash
# Install service
task service:install

# Start/stop
task service:start
task service:stop

# View logs
task service:logs
```

## Universal Frontmatter Interface

Any content with YAML frontmatter is automatically routed to the specified collection:

```yaml
---
collection: Reading
title: Article Title
author: John Doe
url: https://example.com/article
tags: [productivity, tech]
---

Article content here...
```

This makes it easy to integrate with any source (Readwise, Pocket, custom scripts):
- `collection` (required): Target collection name
- `title`: Record title (or use data.title)
- Any other keys: Matched against collection properties

## GitHub Sync (Experimental)

Automatically sync GitHub issues and PRs to a "GitHub" collection in Thymer.

### Setup

1. Create a [GitHub Personal Access Token](https://github.com/settings/tokens) with `repo` scope
2. Add to your config:
   ```
   github_token=ghp_xxxxxxxxxxxx
   github_repos=owner/repo1,owner/repo2
   ```
3. Create a "GitHub" collection in Thymer (or it will use "Inbox")
4. Restart `tm serve`

### How it works

- Syncs every 1 minute
- Uses frontmatter for routing: `collection: GitHub`
- Creates a note for each issue/PR: `repo#123 Issue Title`
- Includes: state, author, labels, body, link
- Stores state in `~/.config/tm/github.db` (bbolt)
- If collection has matching properties, they're auto-populated

### Collection Plugin (Optional)

For custom properties, create a GitHub collection using `plugin/github-collection.json`:
- Fields: Repository, #, Type, State, Author, URL
- Enables filtering/sorting by issue state, type, etc.

## Cloudflare Worker (Untested)

> **Warning**: The Cloudflare Worker has not been tested with the current plugin. Use local server for now.

If you want to try it:

```bash
cd worker
npm install
npx wrangler kv:namespace create QUEUE
# Update wrangler.toml with the KV namespace ID
npx wrangler secret put THYMER_TOKEN
npx wrangler deploy
```

Then update your config:
```
url=https://your-worker.workers.dev
token=your-secret-token
```

## Project Structure

```
thymer-inbox/
├── cmd/tm/main.go    # CLI + local server (Go)
├── plugin/
│   ├── plugin.js     # Thymer plugin code
│   └── plugin.json   # Plugin configuration
├── worker/           # Cloudflare Worker (untested)
├── sdk/              # Thymer Plugin SDK reference
└── tm                # Compiled binary
```

## How It Works

1. **CLI captures content** with timestamp (RFC3339 with timezone)
2. **Server queues it** in memory (local) or KV store (Cloudflare)
3. **Plugin connects via SSE** and receives items as they arrive
4. **Smart routing** decides: inline append vs. create linked note
5. **Markdown parser** converts to Thymer's native block format with hierarchy

## Markdown Support

- Headings (H1-H6) with hierarchical nesting
- Bold, italic, inline code
- Bullet and numbered lists
- Task lists (`- [ ]` and `- [x]`)
- Blockquotes
- Fenced code blocks with language
- Blank lines before headings (for readability)

## License

MIT
