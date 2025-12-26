# Thymer Inbox CLI

When the user says "lifelog [content]" or "inbox [content]", use the `tm` CLI to capture it to their Thymer inbox.

## Quick Capture

```bash
# One-liner
echo "Quick thought to capture" | tm

# With timestamp (adds HH:MM prefix for journal entries)
echo "Meeting with team about roadmap" | tm --timestamp
```

## Multi-line Notes

```bash
# Markdown document (creates a note in Inbox collection)
tm << 'EOF'
# Meeting Notes

Discussed the following:
- Feature prioritization
- Q1 roadmap
- Team assignments
EOF
```

## Targeting Collections

```bash
# Send to specific collection
echo "Buy groceries" | tm --collection Tasks
```

## How it Works

The `tm` CLI connects to a local server (`tm serve`) which pushes content to the Thymer browser plugin via SSE. The plugin then:
- Routes one-liners to today's Journal
- Creates markdown documents in Inbox
- Respects `--collection` flag for explicit routing

## Prerequisites

The user must have:
1. `tm serve` running (or as systemd service)
2. Thymer plugin installed and connected (green indicator)
