# Contributing to Thymer Inbox

Thanks for your interest in contributing!

## Getting Started

1. Fork the repo
2. Clone your fork locally
3. Follow the [README](README.md) to set up local development
4. Make your changes
5. Submit a PR

## Development Setup

```bash
# Build CLI
cd cmd/tm && go build -o ../../tm .

# Run local server
./tm serve

# Install plugin in Thymer (see README)
```

## What to Work On

Check the [open issues](https://github.com/riclib/thymer-inbox/issues) - we've tagged several as good starting points.

## Code Style

- **Go**: Standard `gofmt`
- **JavaScript**: Keep it simple, no build step needed for the plugin

## Pull Requests

- Keep PRs focused on a single change
- Update the README if you change user-facing behavior
- Test locally before submitting

## Questions?

Open an issue or start a discussion. This is a small project - no question is too simple.
