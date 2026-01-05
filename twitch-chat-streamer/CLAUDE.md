# CLAUDE.md - Twitch Chat Streamer

## Project Overview

A Node.js application that streams Twitch.tv chat messages in realtime by connecting to Twitch's IRC servers via WebSocket.

## Build & Run Commands

```bash
# Install dependencies
npm install

# Run the application
npm start -- <channel>

# Run with hot reload (development)
npm run dev -- <channel>

# Examples
npm start -- xqc
npm start -- shroud --events --json
```

## Architecture

- **src/index.js** - CLI entry point with interactive commands
- **src/twitch-client.js** - WebSocket client for Twitch IRC connection
- **src/message-parser.js** - IRC message parsing utilities

## Key Technical Details

### Twitch IRC Protocol
- Connects to `wss://irc-ws.chat.twitch.tv:443`
- Uses anonymous authentication (`justinfan<random>`) for read-only access
- Requests IRCv3 capabilities for message tags and commands
- Handles PING/PONG to maintain connection

### Message Format
IRC messages follow the format: `[@tags] [:prefix] command [params] [:trailing]`
- Tags contain metadata (user info, badges, etc.)
- Prefix contains source (nick!user@host)
- PRIVMSG is the primary chat message command

## Extending the Project

### Adding Authenticated Access
To enable sending messages (not just reading), replace anonymous auth with:
```javascript
this._send(`PASS oauth:${token}`);
this._send(`NICK ${username}`);
```

### Adding More Event Handlers
The client emits events for: `message`, `chat`, `join`, `part`, `notice`, `usernotice`, `clearchat`, `clearmsg`, `roomstate`, `raw`
