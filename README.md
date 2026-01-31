# klaudiusz-interface

Go HTTP server wrapping Claude Code CLI for Home Assistant integration.

## Overview

Provides headless HTTP API for Claude Code to enable voice-based home automation through Home Assistant.

**Architecture:**
```
Voice → Whisper STT → HA Conversation → HTTP → Claude Code MCP Server (Mac) → Response → Piper TTS
```

## Features

- HTTP API on port 8742
- Health check endpoint
- Chi router for minimal overhead
- Session-based conversations (5min timeout)
- Permission system for dangerous actions

## Development

### Requirements

- [mise](https://mise.jdx.dev/) for tool management

### Setup

```bash
mise install
```

### Tasks

```bash
mise run build    # Build binary
mise run test     # Run tests
mise run lint     # Run linter
mise run fmt      # Format code
mise run check    # Run all checks
mise run run      # Run server locally
mise run clean    # Clean artifacts
```

### API Endpoints

- `POST /ask` - Query Claude with optional session
- `POST /cancel` - Cancel pending action
- `GET /health` - Health check

See [brain-plan.md](brain-plan.md) for full implementation details.

## Deployment

### Local (launchd)

See brain-plan.md Phase 2 for launchd service setup.

### Home Assistant Integration

See brain-plan.md Phase 3-4 for HA integration.

## License

MIT
