# Slack as an Interactive Notification & Approval Platform

Deep-dive research into using Slack for a permission approval workflow where a CLI tool (Claude Code hook script) sends interactive notifications to a user, who can approve, reject, or ask follow-up questions -- and get the response back to the CLI.

---

## Table of Contents

1. [Executive Summary: Can Slack Match the Telegram Experience?](#1-executive-summary)
2. [Slack App Setup](#2-slack-app-setup)
3. [Sending Messages & Rich Formatting](#3-sending-messages--rich-formatting)
4. [Interactive Buttons (Block Kit)](#4-interactive-buttons-block-kit)
5. [Socket Mode Deep Dive](#5-socket-mode-deep-dive)
6. [The "Type a Question" Flow](#6-the-type-a-question-flow)
7. [Updating Messages After Response](#7-updating-messages-after-response)
8. [Rate Limits](#8-rate-limits)
9. [Alternatives to Public Webhooks](#9-alternatives-to-public-webhooks)
10. [Architecture for the Permission Approval Workflow](#10-architecture-for-the-permission-approval-workflow)
11. [Slack vs Telegram vs ntfy Comparison](#11-slack-vs-telegram-vs-ntfy-comparison)
12. [Verdict & Recommendation](#12-verdict--recommendation)
13. [Sources](#13-sources)

---

## 1. Executive Summary

**Can Slack match the Telegram-like conversational approval experience?**

**Yes, but with meaningful trade-offs.** Slack can deliver the full workflow -- rich notification with context, approve/reject/ask buttons, freeform typed questions relayed back to the CLI, and message editing after response. However, the architecture is different from Telegram in a critical way:

| Capability | Telegram | Slack |
|---|---|---|
| Send rich notification with buttons | Simple HTTP POST | Simple HTTP POST (with Block Kit) |
| Receive button clicks | Poll `getUpdates` (stateless, short-lived) | **Requires Socket Mode daemon** (long-lived process) |
| Receive typed replies | Same polling mechanism | Same Socket Mode daemon (subscribe to `message.im` event) |
| Text input in same message as buttons | N/A (users just type in chat) | **Not possible** -- `plain_text_input` only works in modals, not messages |
| Edit message after response | `editMessageText` API call | `chat.update` API call |
| No public URL needed | Yes (polling mode) | Yes (Socket Mode) |
| Works from short-lived CLI script | Yes (poll, get answer, exit) | **No** -- Socket Mode needs a persistent process |

**The critical difference**: With Telegram, the hook script itself can poll `getUpdates` in a loop, get the callback, and exit. With Slack, you need a **separate long-running Socket Mode daemon** that maintains the WebSocket connection, receives interactive payloads and DM messages, and communicates with the hook script (e.g., via a local file, Unix socket, or SQLite). The hook script sends the notification, then polls the daemon's output for the response.

**Bottom line**: Slack adds architectural complexity (a daemon process) but delivers a polished, professional experience -- especially if the user already lives in Slack. If you are building this for personal use on a solo workspace, Telegram or ntfy.sh are simpler. If you are building this for a team that already uses Slack, Slack is the natural choice.

---

## 2. Slack App Setup

### 2.1 What You Need

To create a Slack app that can send DMs and handle interactive buttons:

1. **A Slack workspace** -- free tier works fine for personal/solo use
2. **A Slack app** -- created at [api.slack.com/apps](https://api.slack.com/apps)
3. **A bot user** -- part of the app (automatic with modern Slack apps)
4. **An app-level token** -- for Socket Mode (starts with `xapp-`)
5. **A bot token** -- for sending messages (starts with `xoxb-`)

### 2.2 Free vs Paid Workspace

**A free Slack workspace works for this use case.** Here is what matters:

| Feature | Free Plan | Paid Plan (Pro, $7.25/user/mo) |
|---|---|---|
| Custom app/bot | Yes | Yes |
| Bot DMs | Yes | Yes |
| Interactive buttons | Yes | Yes |
| Socket Mode | Yes | Yes |
| App integrations | **10 max** | Unlimited |
| Message history | **90 days** | Unlimited |
| Message deletion | **1 year** (auto-deleted) | Never |

**For a solo developer with a personal workspace**, the free plan is sufficient. Your custom approval bot counts as 1 of the 10 integration slots. Message history is limited to 90 days, but for ephemeral permission notifications that does not matter.

**Developer sandboxes** are also available through the Slack Developer Program. A sandbox is a free Enterprise-grade org environment for development. Sandboxes support up to 8 users, 20 integrations per workspace, and last 6 months (renewable). You may need to provide a payment method but will not be charged.

### 2.3 Required OAuth Scopes (Bot Token Scopes)

| Scope | Purpose |
|---|---|
| `chat:write` | Send messages (including DMs) as the bot |
| `im:write` | Open direct message conversations with users |
| `im:read` | View basic information about DMs the bot is in |
| `im:history` | Read messages in DM conversations (needed to receive typed replies) |
| `users:read` | Look up user information (to resolve user IDs) |

### 2.4 App-Level Token Scope

| Scope | Purpose |
|---|---|
| `connections:write` | Required for Socket Mode -- lets the app open WebSocket connections |

### 2.5 Event Subscriptions

Subscribe to these bot events:

| Event | Purpose |
|---|---|
| `message.im` | Receive messages sent to the bot in DMs (for typed replies/questions) |

### 2.6 Interactivity

When Socket Mode is enabled, interactivity is automatically enabled. No Request URL is needed. Button clicks arrive as `block_actions` payloads over the WebSocket.

### 2.7 Setup Steps (Condensed)

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App**
2. Choose **From scratch**, name it (e.g., "Claude Code Approvals"), select your workspace
3. Go to **Basic Information** > **App-Level Tokens** > **Generate Token and Scopes**
   - Name: "socket-mode"
   - Scope: `connections:write`
   - Save the `xapp-...` token
4. Go to **Socket Mode** in the sidebar > **Enable Socket Mode**
5. Go to **OAuth & Permissions** > **Scopes** > **Bot Token Scopes** and add:
   - `chat:write`, `im:write`, `im:read`, `im:history`, `users:read`
6. Go to **Event Subscriptions** > **Enable Events** > **Subscribe to bot events** and add:
   - `message.im`
7. Go to **Install App** > **Install to Workspace** > Authorize
8. Copy the **Bot User OAuth Token** (`xoxb-...`)
9. Find your Slack user ID (click your profile > three dots > "Copy member ID")

You now have two tokens:
- `SLACK_APP_TOKEN` = `xapp-...` (for Socket Mode)
- `SLACK_BOT_TOKEN` = `xoxb-...` (for sending messages)

---

## 3. Sending Messages & Rich Formatting

### 3.1 Sending a DM to a Specific User

A Slack bot can send a DM directly to a user without them being in any channel. There are two approaches:

**Approach A (simplest):** Pass the user's ID directly as the `channel` parameter in `chat.postMessage`. Slack automatically opens a DM conversation if one does not already exist.

```bash
curl -X POST https://slack.com/api/chat.postMessage \
  -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "U0123456789",
    "text": "Permission request from Claude Code",
    "blocks": [...]
  }'
```

**Approach B (explicit):** Call `conversations.open` first to get the DM channel ID (starts with `D`), then send to that channel ID.

```bash
# Step 1: Open DM
curl -X POST https://slack.com/api/conversations.open \
  -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"users": "U0123456789"}'
# Response includes: {"channel": {"id": "D0123456789"}}

# Step 2: Send message to the DM channel
curl -X POST https://slack.com/api/chat.postMessage \
  -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "D0123456789",
    "text": "Permission request",
    "blocks": [...]
  }'
```

Approach A is simpler and sufficient for this use case. The bot token with `chat:write` and `im:write` scopes is all that is needed.

### 3.2 Block Kit Message Formatting

Slack uses Block Kit for rich message layouts. Messages can contain multiple blocks stacked vertically. The key block types for a permission notification:

| Block Type | Use Case | Notes |
|---|---|---|
| `section` | Main text content, with optional accessory | Supports `mrkdwn` formatting. Max 3000 chars per section text. |
| `context` | Smaller contextual info (metadata, timestamps) | Supports `mrkdwn` and images. Max 10 elements. |
| `divider` | Visual separator | Just `{"type": "divider"}` |
| `actions` | Interactive buttons | Contains button elements |
| `rich_text` | Preformatted code, lists, quotes | For code blocks, use `rich_text_preformatted` sub-element |
| `header` | Large bold text at the top | Plain text only, max 150 chars |

### 3.3 mrkdwn Formatting (Slack's Markdown)

Slack's markdown variant supports:
- `*bold*` for bold
- `_italic_` for italic
- `~strikethrough~` for strikethrough
- `` `inline code` `` for inline code
- ```` ```code block``` ```` for multi-line code blocks (use `\n` for newlines within)
- `>` for block quotes
- `<https://example.com|Link text>` for links
- `<@U0123456789>` for user mentions

### 3.4 Example: Rich Permission Notification

```json
{
  "channel": "U0123456789",
  "text": "Claude Code Permission Request: Bash",
  "blocks": [
    {
      "type": "header",
      "text": {
        "type": "plain_text",
        "text": "Permission Request"
      }
    },
    {
      "type": "section",
      "fields": [
        {
          "type": "mrkdwn",
          "text": "*Tool:*\n`Bash`"
        },
        {
          "type": "mrkdwn",
          "text": "*Project:*\nmy-app (`feature/auth`)"
        }
      ]
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "*Command:*\n```npm install lodash```"
      }
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "*Why:*\n> I need lodash for deep-clone functionality in the data transformation pipeline I'm building."
      }
    },
    {
      "type": "context",
      "elements": [
        {
          "type": "mrkdwn",
          "text": "Recent: `Read package.json` > `Edit src/transform.ts` > `Read src/utils.ts` > *this request*"
        }
      ]
    },
    {
      "type": "divider"
    },
    {
      "type": "actions",
      "block_id": "approval_actions",
      "elements": [
        {
          "type": "button",
          "text": {"type": "plain_text", "text": "Approve"},
          "style": "primary",
          "action_id": "approve_action",
          "value": "request_abc123"
        },
        {
          "type": "button",
          "text": {"type": "plain_text", "text": "Reject"},
          "style": "danger",
          "action_id": "reject_action",
          "value": "request_abc123"
        },
        {
          "type": "button",
          "text": {"type": "plain_text", "text": "Ask for Context"},
          "action_id": "ask_action",
          "value": "request_abc123"
        }
      ]
    }
  ]
}
```

This renders as a professionally formatted card with a header, two-column fields (tool and project), a code block for the command, a quoted explanation, contextual breadcrumbs, a divider, and three action buttons (green Approve, red Reject, neutral Ask).

### 3.5 Maximum Message Size

| Limit | Value |
|---|---|
| Blocks per message | **50** |
| Section block text | **3,000 characters** |
| Top-level `text` fallback | **40,000 characters** |
| mrkdwn field | **12,000 characters** |
| Total block payload (practical) | **~13,000 characters** (undocumented, empirically observed -- serialized JSON of all blocks combined hits `msg_blocks_too_long` around this threshold) |
| Header block text | **150 characters** |
| Context block elements | **10 elements** |

For a permission notification, you are unlikely to hit these limits. A typical notification with tool name, command, reasoning, and recent activity will be well under 3,000 characters total.

---

## 4. Interactive Buttons (Block Kit)

### 4.1 How Buttons Work

Buttons are placed inside an `actions` block. Each button has:
- `action_id` -- unique identifier used to route the callback
- `value` -- data payload sent with the callback (e.g., request ID)
- `style` -- `"primary"` (green) or `"danger"` (red), or omitted for neutral gray
- `text` -- button label (plain text only)

When a user clicks a button, Slack sends a `block_actions` payload to your app containing:
- The `action_id` and `value` of the clicked button
- The user who clicked
- The original message (including `ts` and `channel` for updating it later)
- A `response_url` for responding without using the Web API

### 4.2 Where Does the Callback Go?

This is the key architectural question.

**Without Socket Mode (traditional approach):** The callback goes to your app's **Request URL** -- a publicly reachable HTTPS endpoint that Slack POSTs to. This requires exposing a web server to the internet (directly, via ngrok, via a cloud function, etc.).

**With Socket Mode:** The callback arrives over the **WebSocket connection**. No public URL is needed. Your app receives the payload through the same WebSocket it used to connect.

### 4.3 The 3-Second Acknowledgment Rule

Your app **must acknowledge** (`ack()`) the button click within 3 seconds. If it does not, the user sees a timeout error in Slack. This means:
- You cannot do long processing before acknowledging
- Acknowledge immediately, then do your work asynchronously
- In Bolt, this is handled by calling `ack()` at the start of your action handler

### 4.4 Can You Combine Buttons AND Text Input in the Same Message?

**No.** This is a significant limitation compared to Telegram.

The `plain_text_input` element can only be used inside an `input` block, and `input` blocks are only supported in **Modals** and **Home tabs** -- not in messages. You cannot place a text input field in an `actions` block alongside buttons.

**Workarounds:**

1. **Button opens a modal:** The "Ask for Context" button triggers a modal with a text input field. The user types their question in the modal and submits. This is the "official" Slack pattern.

2. **User types in the DM thread:** Instead of inline text input, the user just types a reply in the DM conversation (or in a thread under the notification message). The bot receives this via the `message.im` event and routes it to the hook script.

3. **Deny-and-explain loop:** The "Ask" button returns a denial to Claude Code with a pre-written message asking it to explain and retry. No text input needed from the user.

Option 2 (typing in the DM) most closely matches the Telegram experience. The user sees the notification with buttons, and if they want to ask a question, they simply type their question in the conversation. The bot picks it up.

---

## 5. Socket Mode Deep Dive

### 5.1 How Socket Mode Works Technically

1. Your app has an **app-level token** (`xapp-...`) with the `connections:write` scope.
2. Your app calls `apps.connections.open` with this token.
3. Slack returns a **WebSocket URL** (dynamic, not static).
4. Your app opens a WebSocket connection to this URL.
5. All event payloads (button clicks, DM messages, etc.) arrive over this WebSocket.
6. Your app acknowledges each payload by sending back an `envelope_id`.
7. The WebSocket URL refreshes periodically -- the SDK handles reconnection automatically.

**Key properties:**
- Bidirectional, low-latency communication
- No public HTTPS endpoint needed
- No signing secret verification needed (the connection itself is pre-authenticated)
- Supports up to **10 simultaneous WebSocket connections** per app
- The connection refreshes regularly; expect and handle disconnects

### 5.2 Can a CLI/Hook Script Use Socket Mode?

**Not directly, and this is the fundamental challenge.**

Socket Mode requires a **long-running process** that maintains the WebSocket connection. A hook script that starts, does its work, and exits cannot practically maintain a WebSocket connection because:

1. **Startup cost:** Calling `apps.connections.open` and establishing the WebSocket takes time (hundreds of milliseconds to seconds).
2. **Rate limiting:** `apps.connections.open` has rate limits. Calling it on every permission request (connect, get response, disconnect) will eventually hit 429 errors.
3. **Design intent:** Socket Mode is designed for persistent services, not short-lived scripts. Rapid connect/disconnect cycles can cause connection loops and rate limiting.

### 5.3 The Daemon Pattern (Required for Slack)

The solution is a **lightweight daemon** that runs alongside Claude Code:

```
┌──────────────────────┐     ┌────────────────────┐     ┌───────────┐
│  Claude Code         │     │  Slack Daemon       │     │  Slack    │
│                      │     │  (long-running)     │     │           │
│  PermissionRequest   │     │                     │     │           │
│  hook fires          │     │  WebSocket ◄────────────► │  Socket   │
│       │              │     │  connection         │     │  Mode     │
│       ▼              │     │                     │     │           │
│  permission-gate.sh  │     │  Receives:          │     │           │
│       │              │     │  - button clicks    │     │           │
│       │ 1. POST msg  │     │  - DM messages      │     │           │
│       │ via Web API ─────► │                     │     │           │
│       │              │     │  Writes response    │     │           │
│       │ 2. Wait for  │     │  to local store     │     │           │
│       │ response     │     │  (file/socket/DB)   │     │           │
│       │ (poll local  │     │       │             │     │           │
│       │  store)      │     │       │             │     │           │
│       │ ◄────────────────── │       │             │     │           │
│       │              │     │                     │     │           │
│       ▼              │     │                     │     │           │
│  Return allow/deny   │     │                     │     │           │
│  to Claude Code      │     │                     │     │           │
└──────────────────────┘     └────────────────────┘     └───────────┘
```

**How it works:**

1. The **daemon** starts once (e.g., when you start your dev session) and maintains the Socket Mode WebSocket connection.
2. The **hook script** sends the notification via Slack's Web API (`chat.postMessage` -- this is a normal HTTP POST, no Socket Mode needed for sending).
3. When the user clicks a button or types a reply, the **daemon** receives the payload over the WebSocket.
4. The daemon writes the response to a local communication channel (file, Unix socket, SQLite database, Redis, etc.).
5. The **hook script** polls this local channel for the response.
6. Once it gets the response, the hook script returns `allow` or `deny` to Claude Code.

### 5.4 SDK Support for Socket Mode

| Language | SDK/Framework | Socket Mode Support | Notes |
|---|---|---|---|
| **Python** | `slack-bolt` (Bolt for Python) | Yes, since v1.2.0 | `SocketModeHandler` or `AsyncSocketModeHandler` |
| **JavaScript/Node.js** | `@slack/bolt` (Bolt for JS) | Yes | `socketMode: true` in app constructor |
| **Java** | `slack-bolt` (Bolt for Java) | Yes | `SocketModeApp` class |
| **Go** | `slack-go/slack` | Yes | `socketmode` package |
| **Python (low-level)** | `slack-sdk` | Yes | `SocketModeClient` |
| **Node.js (low-level)** | `@slack/socket-mode` | Yes | `SocketModeClient` |

### 5.5 Minimal Python Daemon Example

```python
#!/usr/bin/env python3
"""slack_approval_daemon.py - Lightweight Socket Mode listener for permission approvals."""

import os
import json
import sqlite3
from slack_bolt import App
from slack_bolt.adapter.socket_mode import SocketModeHandler

DB_PATH = os.path.expanduser("~/.claude/slack_approvals.db")

# Initialize DB
def init_db():
    conn = sqlite3.connect(DB_PATH)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS responses (
            request_id TEXT PRIMARY KEY,
            decision TEXT,
            message TEXT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    """)
    conn.commit()
    conn.close()

init_db()

app = App(token=os.environ["SLACK_BOT_TOKEN"])

@app.action("approve_action")
def handle_approve(ack, body, client):
    ack()
    request_id = body["actions"][0]["value"]
    # Update message to show "Approved"
    client.chat_update(
        channel=body["channel"]["id"],
        ts=body["message"]["ts"],
        text=f"Approved (request {request_id})",
        blocks=[{
            "type": "section",
            "text": {"type": "mrkdwn", "text": f"*Approved* by <@{body['user']['id']}>"}
        }]
    )
    # Write response for hook script to pick up
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        "INSERT OR REPLACE INTO responses (request_id, decision) VALUES (?, ?)",
        (request_id, "approved")
    )
    conn.commit()
    conn.close()

@app.action("reject_action")
def handle_reject(ack, body, client):
    ack()
    request_id = body["actions"][0]["value"]
    client.chat_update(
        channel=body["channel"]["id"],
        ts=body["message"]["ts"],
        text=f"Rejected (request {request_id})",
        blocks=[{
            "type": "section",
            "text": {"type": "mrkdwn", "text": f"*Rejected* by <@{body['user']['id']}>"}
        }]
    )
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        "INSERT OR REPLACE INTO responses (request_id, decision) VALUES (?, ?)",
        (request_id, "rejected")
    )
    conn.commit()
    conn.close()

@app.action("ask_action")
def handle_ask(ack, body, client):
    ack()
    request_id = body["actions"][0]["value"]
    # Post a reply in thread asking user to type their question
    client.chat_postMessage(
        channel=body["channel"]["id"],
        thread_ts=body["message"]["ts"],
        text="What would you like to know? Type your question here and I'll relay it to Claude."
    )

@app.event("message")
def handle_dm_message(event, client):
    """Handle typed replies in DM -- relay as 'ask' with the user's question."""
    # Only process threaded replies (thread_ts differs from ts)
    thread_ts = event.get("thread_ts")
    if not thread_ts:
        return  # Not a threaded reply, ignore

    # Look up the original message to find the request_id
    # (In production, you'd store a mapping of thread_ts -> request_id)
    user_question = event.get("text", "")

    # Write the question as a response with decision="ask"
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        "INSERT OR REPLACE INTO responses (request_id, decision, message) VALUES (?, ?, ?)",
        (thread_ts, "ask", user_question)  # Using thread_ts as request_id fallback
    )
    conn.commit()
    conn.close()

if __name__ == "__main__":
    handler = SocketModeHandler(app, os.environ["SLACK_APP_TOKEN"])
    print("Slack approval daemon started. Listening for events...")
    handler.start()  # Blocks forever
```

### 5.6 Minimal Node.js Daemon Example

```javascript
// slack_approval_daemon.js
const { App } = require("@slack/bolt");
const fs = require("fs");
const path = require("path");

const RESPONSE_DIR = path.join(
  process.env.HOME,
  ".claude",
  "slack_responses"
);
fs.mkdirSync(RESPONSE_DIR, { recursive: true });

const app = new App({
  token: process.env.SLACK_BOT_TOKEN,
  socketMode: true,
  appToken: process.env.SLACK_APP_TOKEN,
});

app.action("approve_action", async ({ ack, body, client }) => {
  await ack();
  const requestId = body.actions[0].value;
  await client.chat.update({
    channel: body.channel.id,
    ts: body.message.ts,
    text: `Approved (${requestId})`,
    blocks: [
      {
        type: "section",
        text: {
          type: "mrkdwn",
          text: `*Approved* by <@${body.user.id}>`,
        },
      },
    ],
  });
  // Write response file for hook script
  fs.writeFileSync(
    path.join(RESPONSE_DIR, `${requestId}.json`),
    JSON.stringify({ decision: "approved" })
  );
});

app.action("reject_action", async ({ ack, body, client }) => {
  await ack();
  const requestId = body.actions[0].value;
  await client.chat.update({
    channel: body.channel.id,
    ts: body.message.ts,
    text: `Rejected (${requestId})`,
    blocks: [
      {
        type: "section",
        text: {
          type: "mrkdwn",
          text: `*Rejected* by <@${body.user.id}>`,
        },
      },
    ],
  });
  fs.writeFileSync(
    path.join(RESPONSE_DIR, `${requestId}.json`),
    JSON.stringify({ decision: "rejected" })
  );
});

app.action("ask_action", async ({ ack, body, client }) => {
  await ack();
  await client.chat.postMessage({
    channel: body.channel.id,
    thread_ts: body.message.ts,
    text: "Type your question here and I'll relay it to Claude.",
  });
});

app.event("message", async ({ event, client }) => {
  if (!event.thread_ts) return;
  fs.writeFileSync(
    path.join(RESPONSE_DIR, `${event.thread_ts}.json`),
    JSON.stringify({ decision: "ask", message: event.text })
  );
});

(async () => {
  await app.start();
  console.log("Slack approval daemon running (Socket Mode)");
})();
```

### 5.7 Latency

Socket Mode uses WebSockets, which are inherently low-latency. In practice:

- Button click to your app receiving the payload: **sub-second** in normal conditions
- Some developers have reported Socket Mode being **slower than the legacy RTM API**, but for an approval workflow with human-speed interactions, this is irrelevant
- The bottleneck is the human looking at their phone and deciding, not the WebSocket transport
- Known reliability issue: after running for several days, Socket Mode connections can become stale and stop receiving events. The Bolt SDKs handle reconnection, but you should monitor the daemon and restart it if needed

---

## 6. The "Type a Question" Flow

This is the flow where the user sees the notification, decides they need more context, and types a freeform question like "why do you need this?" that gets relayed back to Claude.

### 6.1 How It Works in Slack

**Step 1:** The bot sends a DM with the permission notification (blocks + buttons).

**Step 2:** The user clicks the "Ask" button.

**Step 3:** The bot posts a reply in a **thread** under the original message, prompting the user to type their question. (Threading uses `thread_ts` set to the original message's `ts`.)

**Step 4:** The user types their question as a reply in the thread.

**Step 5:** The bot receives the message via the `message.im` event (delivered over Socket Mode). The event includes `thread_ts` (matching the original message) and `text` (the user's question).

**Step 6:** The daemon writes the question to the local store. The hook script picks it up.

**Step 7:** The hook script returns a denial to Claude Code with the user's question as the denial message: `{"behavior": "deny", "message": "User asks: why do you need this?"}`.

**Step 8:** Claude explains and retries. A new permission notification is sent with Claude's explanation included.

**Step 9:** The user reads Claude's explanation (now visible in the notification) and taps Approve or Reject.

### 6.2 Threading Mechanics

- To start a thread: post a message with `thread_ts` set to the parent message's `ts`
- To detect a threaded reply: incoming `message` events have `thread_ts` set to the parent's `ts`. If `thread_ts` differs from `ts`, it is a reply in a thread.
- The bot receives threaded replies in DMs via the `message.im` event subscription -- no additional scopes needed beyond `im:history`
- All of this works over Socket Mode with no public webhook

### 6.3 Can the App Listen for DM Messages Without a Public Webhook?

**Yes.** With Socket Mode enabled and the `message.im` event subscribed, all DM messages sent to the bot arrive over the WebSocket connection. No public URL is needed.

### 6.4 Comparison to Telegram

In Telegram, the flow is slightly simpler because there is no threading concept -- the user just types in the chat and the bot picks it up via `getUpdates`. In Slack, the threading adds structure (the question and answer are neatly grouped under the original notification), but it also adds a step (the user must click "Ask" first, which opens the thread, then type in the thread).

The Slack thread-based approach actually provides a **better organized conversation** than Telegram's flat chat, especially when multiple permission requests are pending simultaneously. Each request has its own thread.

---

## 7. Updating Messages After Response

### 7.1 The `chat.update` Method

After the user responds (approve/reject), the bot can update the original notification message to reflect the decision. This is done via `chat.update`:

```bash
curl -X POST https://slack.com/api/chat.update \
  -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "D0123456789",
    "ts": "1706456789.123456",
    "text": "Approved",
    "blocks": [
      {
        "type": "section",
        "text": {
          "type": "mrkdwn",
          "text": "*Approved* by <@U0123456789> at 3:47 PM\n\n*Tool:* `Bash`\n*Command:* `npm install lodash`"
        }
      }
    ]
  }'
```

Key facts:
- The bot can only update messages **it posted** (the `chat:write` scope is sufficient)
- The original message is updated in-place -- no new message appears
- If you provide new `blocks`, the old blocks are completely replaced
- If you provide `blocks`, the `text` field is used only as a notification fallback
- The "edited" flag is **not** shown when updating blocks (it is only shown when updating plain text)
- You can remove the buttons entirely (by not including them in the updated blocks) to prevent double-clicks

### 7.2 Using `response_url` (Alternative)

When a button is clicked, the `block_actions` payload includes a `response_url`. You can POST to this URL (up to 5 times within 30 minutes) to update or replace the original message without calling `chat.update`. This is simpler for one-shot responses.

---

## 8. Rate Limits

### 8.1 Relevant Rate Limits

| Method | Tier / Limit | Notes |
|---|---|---|
| `chat.postMessage` | **Special** -- 1 msg/sec per channel, several hundred/min workspace-wide | Generous burst allowed. For a permission workflow, you will never hit this. |
| `chat.update` | **Tier 3** -- ~50 requests/min | More than enough for approval workflows. |
| `conversations.open` | **Tier 3** -- ~50 requests/min | Only needed once to open the DM. |
| `apps.connections.open` | Rate-limited | Do not call repeatedly -- this is why Socket Mode needs a persistent process. |

### 8.2 Rate Limit Headers

When rate-limited, Slack returns HTTP 429 with a `Retry-After` header indicating seconds to wait. The Bolt SDKs handle this automatically.

### 8.3 Practical Impact

For a permission approval workflow, rate limits are a non-issue. You are sending at most a few messages per minute. The only risk is calling `apps.connections.open` too frequently (which is why the daemon pattern is important -- connect once, stay connected).

### 8.4 2025 Rate Limit Changes

In May 2025, Slack announced rate limit changes for `conversations.history` and `conversations.replies` for non-Marketplace apps. These changes do **not** affect `chat.postMessage`, `chat.update`, or `conversations.open`. Internal/customer-built apps maintain their existing rate limits.

---

## 9. Alternatives to Public Webhooks

The requirement is: receive interactive payloads (button clicks, DM messages) without exposing a public HTTPS endpoint.

### 9.1 Socket Mode (Primary Solution)

- WebSocket-based, no public URL
- Works behind firewalls, NATs, corporate networks
- Requires a long-running process
- Full support for all interactive features (buttons, modals, events)
- Supported by all official SDKs (Python, JavaScript, Java)

### 9.2 Slack CLI / Deno Functions (Next-Gen Platform)

Slack's next-gen platform uses Deno-based functions that run on Slack's managed infrastructure. You write TypeScript functions that Slack hosts and executes serverlessly.

- **No server needed** -- Slack runs your code
- Supports interactive messages, modals, workflows
- Uses webhook triggers for external invocations
- Limited to Deno/TypeScript
- Requires `slack deploy` to push code to Slack's infrastructure
- Not well-suited for a CLI tool that needs to send/receive data locally (the function runs on Slack's servers, not your machine)

**Verdict for this use case:** Not a good fit. The permission response needs to get back to the local hook script, not to a function running on Slack's cloud. Could theoretically work with a webhook trigger that the hook script polls, but this adds complexity without clear benefit over Socket Mode.

### 9.3 Summary: Socket Mode is the Answer

For a local CLI tool that needs to receive Slack interactive payloads without a public URL, **Socket Mode is the only practical option**. The trade-off is that it requires a persistent daemon process.

---

## 10. Architecture for the Permission Approval Workflow

### 10.1 Components

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                YOUR MACHINE                                     │
│                                                                                 │
│  ┌────────────────────┐    ┌──────────────────────┐    ┌──────────────────────┐ │
│  │   Claude Code      │    │  permission-gate.sh  │    │  slack-daemon        │ │
│  │                    │    │  (hook script)       │    │  (Python/Node.js)    │ │
│  │  PermissionRequest │    │                      │    │                      │ │
│  │  hook fires ──────────► │ 1. Extract context   │    │  Socket Mode         │ │
│  │                    │    │ 2. POST to Slack     │    │  WebSocket ◄─────────────┐
│  │                    │    │    (chat.postMessage) │    │                      │ │ │
│  │                    │    │ 3. Poll local DB     │    │  Receives:           │ │ │
│  │                    │    │    for response       │    │  - block_actions     │ │ │
│  │                    │    │         │             │    │  - message.im        │ │ │
│  │                    │    │         │             │    │                      │ │ │
│  │                    │    │         │  poll       │    │  Writes to:          │ │ │
│  │                    │    │         ▼             │    │  ~/.claude/responses/│ │ │
│  │                    │    │    Read response ◄────────── (local file/DB)     │ │ │
│  │                    │    │                      │    │                      │ │ │
│  │   ◄────────────────────── 4. Return JSON      │    │                      │ │ │
│  │   allow / deny     │    │    to Claude Code    │    │                      │ │ │
│  └────────────────────┘    └──────────────────────┘    └──────────────────────┘ │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                                                                      │
                                                              ┌───────▼───────┐
                                                              │    Slack      │
                                                              │    API        │
                                                              │               │
                                                              │  DM with      │
                                                              │  buttons      │
                                                              │       │       │
                                                              │       ▼       │
                                                              │  ┌─────────┐  │
                                                              │  │  Phone  │  │
                                                              │  │  Slack  │  │
                                                              │  │  App    │  │
                                                              │  └─────────┘  │
                                                              └───────────────┘
```

### 10.2 The Hook Script (Sending Side)

The hook script does NOT need Socket Mode. It uses the standard Slack Web API to send messages:

```bash
#!/usr/bin/env bash
set -euo pipefail

SLACK_BOT_TOKEN="${SLACK_BOT_TOKEN}"
SLACK_USER_ID="${SLACK_USER_ID}"  # Your Slack user ID
RESPONSE_DIR="${HOME}/.claude/slack_responses"

# Read hook payload from stdin
REQUEST=$(cat)
TOOL=$(echo "$REQUEST" | jq -r '.tool_name // "unknown"')
INPUT=$(echo "$REQUEST" | jq -r '.tool_input | tostring' | head -c 1000)
REQUEST_ID=$(head -c 8 /dev/urandom | xxd -p)

# Extract context from transcript (reuse context_extractor.py from EXPLORATION.md)
REASONING=$(echo "$REQUEST" | python3 ~/.claude/context_extractor.py 2>/dev/null || echo "")

# Send Slack message with blocks
RESPONSE=$(curl -s -X POST https://slack.com/api/chat.postMessage \
  -H "Authorization: Bearer ${SLACK_BOT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
    --arg channel "$SLACK_USER_ID" \
    --arg text "Permission: $TOOL" \
    --arg tool "$TOOL" \
    --arg input "$INPUT" \
    --arg reasoning "$REASONING" \
    --arg request_id "$REQUEST_ID" \
    '{
      channel: $channel,
      text: $text,
      blocks: [
        {type: "header", text: {type: "plain_text", text: "Permission Request"}},
        {type: "section", fields: [
          {type: "mrkdwn", text: ("*Tool:*\n`" + $tool + "`")},
          {type: "mrkdwn", text: "*Status:*\nPending"}
        ]},
        {type: "section", text: {type: "mrkdwn", text: ("*Input:*\n```" + $input + "```")}},
        {type: "section", text: {type: "mrkdwn", text: ("*Reasoning:*\n> " + $reasoning)}},
        {type: "divider"},
        {type: "actions", block_id: "approval", elements: [
          {type: "button", text: {type: "plain_text", text: "Approve"}, style: "primary", action_id: "approve_action", value: $request_id},
          {type: "button", text: {type: "plain_text", text: "Reject"}, style: "danger", action_id: "reject_action", value: $request_id},
          {type: "button", text: {type: "plain_text", text: "Ask"}, action_id: "ask_action", value: $request_id}
        ]}
      ]
    }'
  )")

# Wait for response from daemon (poll local file)
TIMEOUT=300
ELAPSED=0
RESPONSE_FILE="${RESPONSE_DIR}/${REQUEST_ID}.json"

while [ $ELAPSED -lt $TIMEOUT ]; do
  if [ -f "$RESPONSE_FILE" ]; then
    DECISION=$(jq -r '.decision' "$RESPONSE_FILE")
    MESSAGE=$(jq -r '.message // empty' "$RESPONSE_FILE")
    rm -f "$RESPONSE_FILE"

    case "$DECISION" in
      approved)
        echo '{"behavior": "allow"}'
        exit 0
        ;;
      rejected)
        echo "{\"behavior\": \"deny\", \"message\": \"Rejected remotely via Slack\"}"
        exit 0
        ;;
      ask)
        echo "{\"behavior\": \"deny\", \"message\": \"User asks: ${MESSAGE}. Please explain and try again.\"}"
        exit 0
        ;;
    esac
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

echo '{"behavior": "deny", "message": "Timed out waiting for Slack response"}'
```

### 10.3 The Full "Ask a Question" Flow (Conversational)

```
Bot (DM):  ┌─────────────────────────────────────┐
           │  Permission Request                  │
           │                                      │
           │  Tool: Bash                          │
           │  Status: Pending                     │
           │                                      │
           │  Input:                               │
           │  ┌─────────────────────────────────┐ │
           │  │ npm install lodash              │ │
           │  └─────────────────────────────────┘ │
           │                                      │
           │  Reasoning:                           │
           │  > I need lodash for deep-clone      │
           │  > functionality                     │
           │                                      │
           │  ─────────────────────────────────── │
           │  [Approve]  [Reject]  [Ask]          │
           └─────────────────────────────────────┘

User:      [clicks Ask]

Bot:       (in thread)
           "Type your question here and I'll
            relay it to Claude."

User:      (in thread)
           "Why not use structuredClone()?"

           ── daemon receives message.im event ──
           ── writes {decision: "ask", message: "..."} ──
           ── hook script picks up, denies with question ──
           ── Claude explains and retries ──

Bot (DM):  ┌─────────────────────────────────────┐
           │  Permission Request (retry)           │
           │                                      │
           │  Tool: Bash                          │
           │  Input: npm install lodash            │
           │                                      │
           │  Claude's explanation:                │
           │  > structuredClone isn't available    │
           │  > in Node 14 which is the project's │
           │  > target runtime. lodash cloneDeep   │
           │  > is the standard solution.          │
           │                                      │
           │  [Approve]  [Reject]  [Ask]          │
           └─────────────────────────────────────┘

User:      [clicks Approve]

Bot:       ┌─────────────────────────────────────┐
           │  Approved by @you at 3:47 PM         │
           │  Tool: Bash                          │
           │  Command: npm install lodash          │
           └─────────────────────────────────────┘
```

---

## 11. Slack vs Telegram vs ntfy Comparison

Comparing the three options specifically for the Claude Code permission approval workflow:

| Dimension | Slack | Telegram | ntfy.sh |
|---|---|---|---|
| **Setup complexity** | High (app creation, scopes, tokens, daemon) | Medium (BotFather, 2 tokens, polling) | Low (no account, curl) |
| **Infrastructure** | Daemon process required | Polling in hook script (or webhook) | No daemon, pure HTTP |
| **Public URL needed?** | No (Socket Mode) | No (polling) | No |
| **Rich formatting** | Excellent (Block Kit) | Good (Markdown + HTML) | Basic (title + body) |
| **Action buttons** | Native, styled (primary/danger) | Native (inline keyboard) | HTTP action buttons |
| **Text input in message** | Not possible (needs modal or thread) | User types in chat | Not possible |
| **Threaded conversation** | Excellent (native threads) | Flat chat (less organized) | No conversation |
| **Message editing** | `chat.update` (clean, removes buttons) | `editMessageText` (clean) | Not supported |
| **Freeform question flow** | Thread reply + `message.im` event | Reply in chat + `getUpdates` | Not supported natively |
| **Mobile push reliability** | High (Slack app) | High (Telegram app) | High (ntfy app) |
| **Works on free plan** | Yes (10 integration limit) | Yes (fully free) | Yes (rate-limited) |
| **Latency** | Sub-second (WebSocket) | 1-2 seconds (polling interval) | Sub-second (HTTP) |
| **Multi-request organization** | Each request in its own thread | All in one flat chat | Separate notifications |
| **Team use** | Excellent (existing workspaces) | Possible but unusual | Good (self-host) |
| **Existing user base** | Many devs already use Slack | Many devs have Telegram | Niche, developer-oriented |

### When to Use Each

**Use Slack when:**
- Your team already uses Slack for work communication
- You want the most polished, professional approval UX
- You value threaded conversations for organizing multiple requests
- You are comfortable running a lightweight daemon process
- You want team-wide visibility of permission decisions

**Use Telegram when:**
- You want the simplest possible interactive experience (no daemon needed)
- You are building this for personal use
- You want the "Ask" flow to be as natural as texting
- You do not want to run any infrastructure

**Use ntfy.sh when:**
- You want the absolute minimum setup (zero accounts, zero processes)
- Binary approve/reject is sufficient (no "ask a question" flow)
- You want self-hostable, open-source infrastructure
- You are building a proof of concept in an afternoon

---

## 12. Verdict & Recommendation

### Can Slack Match the Telegram Conversational Experience?

**Yes, with two caveats:**

1. **You need a daemon process.** Unlike Telegram where the hook script can poll for responses directly, Slack requires a persistent Socket Mode listener. This is a meaningful increase in operational complexity -- you need to start the daemon, keep it running, monitor it, and restart it if it dies.

2. **The "Ask" flow requires threading, not inline input.** In Telegram, the user types directly in the chat and the bot picks it up. In Slack, the user clicks "Ask", then types in a thread. The result is the same, but Slack adds one extra click. (Alternatively, the "Ask" button could open a modal with a text input field, which some users may prefer.)

### Slack-Specific Advantages Over Telegram

- **Threads** keep multiple concurrent permission requests neatly organized
- **Block Kit** produces more professional-looking notifications than Telegram's Markdown
- **Team integration** -- if your team already uses Slack, approval notifications are where people already are
- **`chat.update`** cleanly replaces the notification after approval, removing buttons to prevent double-clicks
- **Search** -- approved/rejected requests are searchable in Slack

### Recommendation for the Permission Notification System

For the Claude Code permission notification project:

1. **Start with ntfy.sh** for the proof of concept (as the existing EXPLORATION.md recommends)
2. **Add Telegram** as the first interactive upgrade (no daemon, natural "ask" flow)
3. **Add Slack** as a team/enterprise option, with the daemon pattern, for users whose workflow is already Slack-centric

The Slack integration is worth building, but it is not the best starting point due to the daemon requirement. It is best positioned as the "team edition" of the notification system.

---

## 13. Sources

### Slack Official Documentation
- [Socket Mode](https://api.slack.com/apis/socket-mode) -- WebSocket-based event delivery
- [Using Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/) -- Setup and usage guide
- [Comparing HTTP & Socket Mode](https://docs.slack.dev/apis/events-api/comparing-http-socket-mode/) -- Trade-offs between approaches
- [Creating Interactive Messages](https://docs.slack.dev/messaging/creating-interactive-messages/) -- Block Kit interactive messages
- [Interactive Components Reference](https://api.slack.com/reference/block-kit/interactive-components) -- Button, menu, input element specs
- [Block Kit](https://docs.slack.dev/block-kit/) -- Block Kit overview
- [Blocks Reference](https://docs.slack.dev/reference/block-kit/blocks/) -- All block types and limits
- [Plain-text Input Element](https://docs.slack.dev/reference/block-kit/block-elements/plain-text-input-element/) -- Where text inputs can and cannot be used
- [Modals](https://docs.slack.dev/surfaces/modals/) -- Modal surfaces (where text input is supported)
- [chat.postMessage](https://api.slack.com/methods/chat.postMessage) -- Send messages
- [chat.update](https://api.slack.com/methods/chat.update) -- Edit messages
- [Modifying Messages](https://api.slack.com/messaging/modifying) -- Update/delete messages
- [conversations.open](https://api.slack.com/methods/conversations.open) -- Open DM conversations
- [Permission Scopes](https://api.slack.com/scopes) -- OAuth scope reference
- [Rate Limits](https://docs.slack.dev/apis/web-api/rate-limits/) -- API rate limiting
- [Rate Limit Changes for Non-Marketplace Apps](https://docs.slack.dev/changelog/2025/05/29/rate-limit-changes-for-non-marketplace-apps/) -- 2025 changes
- [Formatting Message Text](https://docs.slack.dev/messaging/formatting-message-text/) -- mrkdwn syntax
- [Rich Text Block](https://docs.slack.dev/reference/block-kit/blocks/rich-text-block/) -- Rich text formatting
- [Feature Limitations on Free Version](https://slack.com/help/articles/27204752526611-Feature-limitations-on-the-free-version-of-Slack) -- Free plan details
- [Slack Developer Program](https://api.slack.com/developer-program) -- Developer sandboxes
- [Developer Sandboxes](https://docs.slack.dev/tools/developer-sandboxes/) -- Free Enterprise dev environments

### SDK & Framework Documentation
- [Bolt for Python -- Socket Mode](https://tools.slack.dev/bolt-python/concepts/socket-mode/) -- Python Socket Mode guide
- [Bolt for Python -- GitHub](https://github.com/slackapi/bolt-python) -- Source and examples
- [Bolt for Python -- Socket Mode Example](https://github.com/slackapi/bolt-python/blob/main/examples/socket_mode.py) -- Working example code
- [Bolt for JavaScript -- Building an App](https://docs.slack.dev/tools/bolt-js/building-an-app/) -- JS getting started
- [Bolt for JavaScript -- GitHub](https://github.com/slackapi/bolt-js) -- Source and examples
- [Node.js Socket Mode Package](https://www.npmjs.com/package/@slack/socket-mode) -- Low-level Node.js client
- [Java Bolt SDK -- Interactive Components](https://tools.slack.dev/java-slack-sdk/guides/interactive-components/) -- Java action handlers

### Next-Gen Platform
- [Deno Slack SDK](https://docs.slack.dev/tools/deno-slack-sdk/) -- Deno-based serverless functions
- [Creating Custom Functions](https://docs.slack.dev/tools/deno-slack-sdk/guides/creating-custom-functions/) -- Serverless function guide
- [Creating Webhook Triggers](https://docs.slack.dev/tools/deno-slack-sdk/guides/creating-webhook-triggers/) -- External invocation

### Community & Issue Reports
- [Socket Mode Slower Than RTM](https://github.com/slackapi/node-slack-sdk/issues/1159) -- Performance report
- [Socket Mode Unreliable](https://github.com/slackapi/bolt-js/issues/1151) -- Reliability issues
- [Socket Mode Stops Responding](https://github.com/slackapi/node-slack-sdk/issues/1652) -- Stale connections
- [Text Input in Message Block](https://github.com/slackapi/bolt-js/issues/818) -- Limitation confirmation
- [Blocks Too Long Error](https://github.com/slackapi/bolt-js/issues/2509) -- Undocumented size limits
- [Section Block 3000 Char Limit](https://github.com/slackapi/python-slack-sdk/issues/1336) -- Text length limits

### Tutorials & Guides
- [Knock: Creating Interactive Slack Apps with Bolt](https://knock.app/blog/creating-interactive-slack-apps-with-bolt-and-nodejs) -- End-to-end tutorial
- [Knock: Developer's Guide to Slack Markdown](https://knock.app/blog/the-guide-to-slack-markdown) -- Formatting reference
- [Sending DMs from Slack Bot](https://medium.com/@shiv0403gupta/sending-dms-from-slack-bot-72b3ffea93f5) -- DM implementation guide
