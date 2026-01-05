# Twitch Chat Streamer

Stream Twitch.tv chat messages in realtime. This application connects to Twitch's IRC servers via WebSocket and provides a full firehose of chat messages for any public channel.

## Features

- **Realtime streaming** - Receive chat messages as they happen
- **No authentication required** - Uses anonymous connection for read-only access
- **Multiple channels** - Join and monitor multiple channels simultaneously
- **Rich message data** - Access badges, subscriber status, mod status, and more
- **Event notifications** - Optionally see subscriptions, raids, bans, and other events
- **JSON output** - Machine-readable output mode for piping to other tools
- **Interactive commands** - Join/leave channels on the fly
- **Auto-reconnect** - Automatically reconnects on disconnection

## Installation

```bash
cd twitch-chat-streamer
npm install
```

## Usage

### Basic Usage

Stream chat from a single channel:

```bash
npm start -- xqc
```

### Multiple Channels

```bash
npm start -- shroud pokimane summit1g
# or
npm start -- -c shroud -c pokimane -c summit1g
```

### Options

```
-c, --channel <name>  Add a channel to join (can be used multiple times)
-d, --debug           Enable debug output (shows raw IRC messages)
-j, --json            Output messages as JSON (useful for piping)
-e, --events          Show all events (joins, subs, raids, bans, etc.)
-h, --help            Show help message
```

### Examples

```bash
# Stream with all events visible
npm start -- xqc --events

# Output as JSON for processing
npm start -- shroud --json

# Debug mode to see raw IRC traffic
npm start -- pokimane --debug

# Pipe JSON to jq for filtering
npm start -- xqc --json | jq 'select(.username == "nightbot")'
```

### Interactive Commands

While running, you can use these commands:

- `/join <channel>` - Join a channel
- `/part <channel>` - Leave a channel
- `/channels` - List currently joined channels
- `/quit` - Exit the application

## Output Format

### Standard Output

```
[14:32:15] [MOD] Nightbot: !commands
[14:32:16] xQcOW: OMEGALUL
[14:32:17] [SUB] someuser123: Nice play!
```

### JSON Output (`--json`)

```json
{
  "channel": "xqc",
  "username": "xQcOW",
  "content": "OMEGALUL",
  "tags": {
    "display-name": "xQcOW",
    "subscriber": "1",
    "mod": "0",
    "badges": "subscriber/48"
  },
  "formatted": "[14:32:16] xQcOW: OMEGALUL"
}
```

## How It Works

1. Connects to Twitch IRC via WebSocket (`wss://irc-ws.chat.twitch.tv:443`)
2. Authenticates anonymously using a `justinfan` username (read-only access)
3. Requests IRC capabilities for message tags and commands
4. Joins specified channels and streams messages in realtime
5. Parses IRC messages into structured data with user info, badges, etc.

## Events

When using `--events`, you'll see notifications for:

- **Subscriptions** - New subs and resubs
- **Gift subs** - Gifted subscriptions
- **Raids** - Incoming raids with viewer count
- **Timeouts/Bans** - Moderation actions
- **Room state changes** - Slow mode, sub-only mode, etc.

## Requirements

- Node.js 18+ (uses ES modules)
- Internet connection to Twitch servers

## License

MIT
