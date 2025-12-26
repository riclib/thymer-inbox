# Thymer Inbox

A CLI tool and plugin system for sending content to [Thymer](https://thymer.com) from the command line.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  CLI                                                    │
│  cat file.md | tm                                       │
│  tm lifelog "Had coffee"                                │
└──────────────────────────┬──────────────────────────────┘
                           │ POST /queue
                           ▼
┌─────────────────────────────────────────────────────────┐
│  Queue Server                                           │
│  • Cloudflare Worker (production)                       │
│  • tm serve (local development)                         │
└──────────────────────────┬──────────────────────────────┘
                           │ SSE /stream
                           ▼
┌─────────────────────────────────────────────────────────┐
│  Thymer Plugin (plugin/)                                │
│  • Status bar toggle                                    │
│  • Receives content via SSE                             │
│  • Parses markdown → Thymer native blocks               │
└─────────────────────────────────────────────────────────┘
```

## Repository Structure

```
thymer-inbox/
├── cmd/tm/           # CLI tool source (Go)
├── plugin/           # Thymer plugin (JavaScript)
├── worker/           # Cloudflare Worker queue server
├── sdk/              # Thymer Plugin SDK (cloned from upstream)
├── Taskfile.yml      # Task runner commands
└── tm                # Compiled binary
```

## Quick Start

### 1. Build and Install CLI

```bash
task build        # Build the tm binary
task install      # Install to ~/.local/bin
```

### 2. Configure

Set environment variables or create `~/.config/tm/config`:

```bash
# For production (Cloudflare Worker)
export THYMER_URL=https://your-worker.workers.dev
export THYMER_TOKEN=your-secret-token

# Or for local development
export THYMER_URL=http://localhost:19501
export THYMER_TOKEN=local-dev-token
```

### 3. Install Thymer Plugin

1. Open Thymer → Command Palette (Cmd+P) → Plugins
2. Create a new App Plugin
3. Paste contents of `plugin/plugin.js` into Code tab
4. Paste contents of `plugin/plugin.json` into Configuration tab
5. Configure `queueUrl` and `queueToken` in the JSON
6. Save and enable the plugin

### 4. Use It

```bash
# Push markdown to Thymer
cat README.md | tm

# Quick lifelog entry
tm lifelog Had coffee with Alex

# Push to specific collection
tm --collection "Tasks" < todo.md
```

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

Actions:
  append (default)  Append to daily page
  lifelog           Add timestamped lifelog entry
  create            Create new record in collection
```

## Tasks

Run `task` to see all available commands:

### CLI
- `task build` - Build the tm binary
- `task install` - Install to ~/.local/bin
- `task serve` - Run local server (foreground)

### Service (systemd)
- `task service:install` - Install as user service
- `task service:start` - Start the service
- `task service:stop` - Stop the service
- `task service:logs` - Tail service logs

### Plugin
- `task plugin:copy` - Copy plugin.js to clipboard
- `task plugin:copy-json` - Copy plugin.json to clipboard
- `task plugin:copy-all` - Copy both files sequentially
- `task plugin:test` - Copy test markdown to clipboard

### Cloudflare Worker
- `task worker:dev` - Run worker locally
- `task worker:deploy` - Deploy to Cloudflare
- `task worker:kv-create` - Create KV namespace
- `task worker:secret` - Set THYMER_TOKEN secret

### SDK
- `task sdk:update` - Pull latest SDK from upstream
- `task sdk:dev` - Run SDK dev server (hot reload)
- `task sdk:install` - Install SDK npm dependencies

## Local Development

### Option 1: Local Server

```bash
# Terminal 1: Run local server
task serve

# Terminal 2: Send content
echo "Hello Thymer" | tm
```

### Option 2: Cloudflare Worker (wrangler dev)

```bash
# Terminal 1: Run worker locally
task worker:dev

# Terminal 2: Send content (configure THYMER_URL to wrangler's URL)
echo "Hello Thymer" | tm
```

## Cloudflare Worker Setup

1. Create KV namespace:
   ```bash
   task worker:kv-create
   ```

2. Update `worker/wrangler.toml` with the KV namespace ID

3. Set the auth token:
   ```bash
   task worker:secret
   ```

4. Deploy:
   ```bash
   task worker:deploy
   ```

## Plugin Features

### Working
- **Headings** - H1-H6 with proper levels
- **Bold/Italic/Inline Code** - Full inline formatting
- **Bullet lists** - Unordered lists
- **Ordered lists** - Numbered lists
- **Blockquotes** - Quote blocks
- **Tasks** - Checkbox items
- **Code blocks** - Fenced code with language
- **Paragraphs** - Regular text

### Commands
- **Paste Markdown** - Paste from clipboard into current note
- **Dump Line Items** - Debug: log items to console

### Status Bar
Click the 🪄 icon to toggle queue polling on/off.

## License

MIT
