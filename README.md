# Thymer Inbox

A plugin and CLI for getting data from the outside world into [Thymer](https://thymer.com). Turn Thymer into an event sink for your digital life.

## Currently Implemented

- **Markdown paste** - Pipe any markdown from the terminal into your Thymer Journal
- **GitHub sync** - Automatically sync issues and PRs from your repos into Thymer
- **Google Calendar sync** - Sync your calendar events with time ranges into Thymer
- **Readwise sync** - Sync your highlights from Readwise Reader into Thymer
- **tm CLI** - Command-line interface to push content to Thymer

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│  OUTSIDE WORLD                                                  │
│  • tm CLI (echo "note" | tm)                                    │
│  • GitHub API (issues, PRs)                                     │
│  • Google Calendar API (events)                                 │
│  • Readwise Reader API (highlights)                             │
└──────────────────────────┬──────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  tm serve (local Go server)                                     │
│  • Queues incoming content                                      │
│  • Polls GitHub/Calendar every minute, Readwise hourly          │
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

### 1. Install Task (build tool)

This project uses [Task](https://taskfile.dev) as a build tool. Install it first:

```bash
# macOS
brew install go-task

# Arch Linux
sudo pacman -S go-task

# Ubuntu/Debian (snap)
sudo snap install task --classic

# Or with Go
go install github.com/go-task/task/v3/cmd/task@latest
```

### 2. Build the CLI

```bash
task build           # Build ./tm binary
task install         # Build and install to ~/.local/bin/tm
```

Or manually without Task:

```bash
cd cmd/tm && go build -o ../../tm . && cd ../..
```

### 3. Configure

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

### 4. Install the Plugins

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

Define custom fields for synced content. For each collection you want:

1. Click the **+** button next to "Collections" in the sidebar
2. In the dialog, click **"Edit as code"**
3. Paste the respective JSON file and save

**Files to paste:**
- `plugin/github-collection.json` → Creates "GitHub" collection for issues/PRs
- `plugin/calendar-collection.json` → Creates "Calendar" collection for events
- `plugin/readwise-collection.json` → Creates "Readwise" collection for highlights

**Tip:** Use `task plugin:copy-github` or `task plugin:copy-calendar` to copy the JSON to your clipboard.

### 5. Start the Server

```bash
./tm serve
```

Output:
```
🪄 Thymer queue server on http://localhost:19501
   Token: local-dev-token
📡 GitHub sync enabled for: owner/repo1, owner/repo2
```

### 6. Test It

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
  tm resync [repo|readwise|calendar]  Clear sync cache and resync
  tm readwise-sync                    Trigger Readwise sync now

  # Google Calendar
  tm auth google                      Authenticate with Google
  tm calendars                        List available calendars
  tm calendars enable <id>            Enable calendar for sync
  tm calendars disable <id>           Disable calendar

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

### Custom Workflow Fields

You can add your own fields to the GitHub collection for project tracking - **user-set values are preserved** when sync updates issues.

**Example workflow fields to add:**

| Field | Type | Purpose |
|-------|------|---------|
| Status | Choice | Local workflow: Backlog, This Week, Doing, Done |
| Due Date | Date | Manual planning (GitHub lacks native due dates) |
| Area | Choice | Group by project area across repos |
| Priority | Choice | P0, P1, P2, P3 |

**To add fields:**

1. Open your GitHub collection in Thymer
2. Click the collection name → Edit Collection
3. Add new properties (fields) as needed
4. Create views grouped/sorted by your custom fields

**How it works:**

- Sync only updates fields it knows about (repo, number, state, type, url)
- Your custom fields remain untouched across syncs
- Build kanban boards, time-based views, or area groupings on top of GitHub data

## Google Calendar Sync

Automatically sync your Google Calendar events to Thymer with proper time ranges.

### Setup

Google Calendar requires OAuth authentication. You'll need to set up your own Google Cloud credentials:

#### 1. Create Google Cloud Credentials

1. Go to [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Create a new project (or select existing)
3. Enable the **Google Calendar API**:
   - Go to "APIs & Services" → "Library"
   - Search for "Google Calendar API" and enable it
4. Create OAuth credentials:
   - Go to "APIs & Services" → "Credentials"
   - Click "Create Credentials" → "OAuth client ID"
   - Choose "Desktop app" as the application type
   - Note down the Client ID and Client Secret

#### 2. Configure tm

Add your credentials to `~/.config/tm/config`:

```
google_client_id=YOUR_CLIENT_ID.apps.googleusercontent.com
google_client_secret=YOUR_CLIENT_SECRET
```

#### 3. Authenticate

```bash
tm auth google
```

This opens your browser to authorize calendar access. Tokens are stored locally in `~/.config/tm/google_tokens.json`.

#### 4. Enable Calendars

```bash
# List available calendars
tm calendars

# Enable calendars to sync
tm calendars enable primary
tm calendars enable work@company.com
```

#### 5. Install Collection Plugin

1. Create a **Collection Plugin** in Thymer
2. Paste `plugin/calendar-collection.json` into the Configuration tab
3. Save

#### 6. Start Server

```bash
tm serve
```

### How It Works

- Polls Google Calendar every 1 minute
- Syncs events from 1 week ago to 12 weeks ahead
- Uses Thymer's `DateTime` with range support:
  - Timed events: `Sun Dec 21 11:00 — Sun Dec 21 12:15`
  - All-day events: `Dec 27` (single day) or `Dec 27 — Dec 29` (multi-day)
- Uses `external_id` for deduplication (e.g., `gcal_abc123`)
- Adds timestamped entries to Journal: `15:21 created [[Meeting Title]]`
- Stores sync state in `~/.config/tm/calendar.db` (bbolt)

### Calendar Commands

```bash
tm auth google              # Authenticate with Google
tm auth google --force      # Re-authenticate
tm calendars                # List all calendars
tm calendars enable <id>    # Enable a calendar for sync
tm calendars disable <id>   # Disable a calendar
tm calendar-test            # Debug: show raw calendar data
tm resync calendar          # Clear cache and resync
```

### Custom Fields

Like GitHub sync, you can add custom fields to the Calendar collection:

| Field | Type | Purpose |
|-------|------|---------|
| Prep Done | Checkbox | Track meeting preparation |
| Energy | Choice | Rate energy: High, Medium, Low |
| Outcome | Choice | Rate: Productive, Neutral, Waste |
| Needs Follow-up | Checkbox | Flag items needing action |

The collection template includes these fields by default.

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

## Available Tasks

Run `task` or `task --list` to see all available tasks:

```bash
# Build & Install
task build              # Build ./tm binary
task install            # Install to ~/.local/bin

# Server
task serve              # Run server in foreground
task service:install    # Install systemd service
task service:start      # Start service
task service:stop       # Stop service
task service:logs       # Tail server logs

# Plugin Helpers (copies to clipboard)
task plugin:copy        # Copy plugin.js
task plugin:copy-json   # Copy plugin.json
task plugin:copy-github # Copy github-collection.json
task plugin:copy-calendar # Copy calendar-collection.json

# Claude Code Skill
task skill:install      # Install capture skill
task skill:uninstall    # Remove skill
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
│   ├── auth.go           # Google OAuth flow
│   ├── calendar.go       # Google Calendar sync
│   ├── github.go         # GitHub sync logic
│   └── readwise.go       # Readwise sync logic
├── plugin/
│   ├── plugin.js         # App Plugin (SSE, markdown, routing)
│   ├── plugin.json       # App Plugin config
│   ├── calendar-collection.json  # Collection Plugin (Calendar)
│   ├── github-collection.json    # Collection Plugin (GitHub)
│   └── readwise-collection.json  # Collection Plugin (Readwise)
├── skill/
│   └── SKILL.md          # Claude Code skill for natural language capture
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
