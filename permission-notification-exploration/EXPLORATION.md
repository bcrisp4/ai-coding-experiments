# Permission Notification System for Claude Code

## The Problem

When running Claude Code autonomously (e.g. on a long task while you're away from your desk), you face a three-way tradeoff:

1. **Babysit it** — Sit and watch, approving each permission prompt manually. Defeats the purpose of autonomous operation.
2. **Pre-approve everything** — Add blanket permissions to your config. Works until Claude needs something you didn't anticipate, and weakens your security posture.
3. **Skip permissions entirely** — Use `--dangerously-skip-permissions`. Fast but risky.

What's missing is a **middle ground**: pre-approve the common stuff, then get notified on your phone when something unexpected comes up, review the context, and approve/reject remotely.

---

## Part 1: How to Hook Into Claude Code

Claude Code has a hook system that fires shell commands at specific lifecycle events. This is the primary integration point.

### Relevant Hook Events

| Hook Event | When It Fires | Why It Matters |
|---|---|---|
| `PreToolUse` | Before any tool executes, before the permission check | Can approve/deny before the permission dialog appears |
| `PermissionRequest` | When Claude Code shows a permission prompt to the user | Direct interception of the "waiting for permission" moment |
| `Notification` | When Claude Code sends a notification (including `permission_prompt` type) | Awareness hook — knows a permission prompt is showing |
| `PostToolUse` | After tool execution completes | Useful for audit logging |

### How Hooks Work

Hooks are configured in `~/.claude/settings.json` (user-level) or `.claude/settings.json` (project-level):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/your-script.sh"
          }
        ]
      }
    ]
  }
}
```

- **`matcher`** — Filters which tools trigger the hook. Empty string matches all tools. Can be a tool name like `"Bash"`, `"Edit"`, `"mcp__server__tool"`, etc.
- **`type`** — Always `"command"` (runs a shell command).
- **`command`** — The shell command to execute. Receives context via environment variables or stdin (JSON with tool name, input, etc.).

### What a Hook Can Do

A hook script can return JSON to control behavior:

```json
// Approve the action
{"behavior": "allow", "updatedInput": "...optional modified input..."}

// Deny the action
{"behavior": "deny", "message": "Reason for denial"}

// Ask the user (default if no response)
{"permissionDecision": "ask"}
```

Exit codes also matter:
- Exit `0` — Hook succeeded, use the JSON output
- Exit `2` — Block the action

### What the Hook Actually Receives

Every hook gets a JSON payload on stdin. The full schema:

```json
{
  "session_id": "eb5b0174-0555-4601-804e-672d68069c89",
  "transcript_path": "/home/user/.claude/projects/-home-user-myproject/eb5b0174.jsonl",
  "cwd": "/home/user/myproject",
  "permission_mode": "default",
  "hook_event_name": "PermissionRequest",
  "tool_name": "Bash",
  "tool_input": {
    "command": "npm install some-package",
    "description": "Install dependencies"
  },
  "tool_use_id": "toolu_01ABC123..."
}
```

| Field | What It Tells You |
|---|---|
| `tool_name` | The tool Claude wants to use (Bash, Edit, Write, mcp__server__tool, etc.) |
| `tool_input` | Exact parameters — the command, file path, content, etc. |
| `session_id` | Unique session identifier |
| `transcript_path` | **The critical field** — full path to the session's JSONL transcript file |
| `cwd` | Current working directory |
| `permission_mode` | Which permission mode is active (default, plan, acceptEdits, etc.) |

The `tool_input` schema varies by tool:
- **Bash**: `command`, `description`, `timeout`
- **Edit**: `file_path`, `old_string`, `new_string`
- **Write**: `file_path`, `content`
- **Read**: `file_path`, `offset`, `limit`
- **MCP tools**: Follows `mcp__<server>__<tool>` naming with tool-specific input

### The Session Transcript — Your Window Into Context

The `transcript_path` field is the key to solving the context problem. It points to a JSONL file that contains the **entire conversation history** for this session:

```
~/.claude/projects/<encoded-project-path>/<session-uuid>.jsonl
```

Each line is a JSON object:

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      {"type": "text", "text": "I'll fix the validation bug by editing the check function."},
      {"type": "tool_use", "id": "toolu_01ABC...", "name": "Edit", "input": {"file_path": "..."}}
    ]
  },
  "uuid": "a1234567-...",
  "timestamp": "2026-01-28T10:30:55.015Z",
  "gitBranch": "feature/validation-fix"
}
```

Entry types include `"user"` (what you asked), `"assistant"` (Claude's responses including tool calls), and `"summary"` (compacted context). By parsing this file, your hook script can extract:

- **Claude's stated reasoning** — The text block in the last assistant message, right before the tool_use block, typically explains what Claude intends to do and why
- **Your original prompt** — What you asked Claude to work on
- **Recent conversation history** — The last N back-and-forth exchanges
- **Prior tool uses and their results** — What Claude has already done in this session
- **Git branch and project context** — From entry metadata

**What's NOT available**: Claude's internal chain-of-thought / extended thinking. Only its visible text output. But in practice, Claude almost always explains its reasoning before calling a tool, so the last assistant text block is a good proxy.

### The Key Insight

The `PermissionRequest` hook fires **when Claude Code is already waiting for a yes/no from the user**. If your hook script can:

1. Parse the transcript for context about what Claude is doing and why
2. Send a rich notification to your phone
3. Wait for your response
4. Return `allow` or `deny`

...then Claude Code will act on your remote decision as if you'd typed it at the terminal.

---

## Part 2: Notification Delivery Options

See [notification-options/README.md](../permission-notification-research/README.md) for a detailed comparison of 8 notification services. The three realistic options for this use case are ntfy.sh, Slack, and Telegram. WhatsApp was investigated and ruled out.

### ntfy.sh — Best for simplicity and self-hosting

- Send: `curl -d "message" ntfy.sh/your-topic`
- Native approve/reject buttons via `http` actions
- Response comes back via a second topic
- Self-hostable, open source, free
- No account needed for prototyping
- **Limitation**: No conversational "ask a question" flow. Binary approve/reject only.

### Slack — Best for rich interaction (if you already use it)

Slack can be used at three different levels of complexity:

**Level 1: Send-only (no daemon, no buttons)**
- Hook script POSTs a rich Block Kit notification via `chat.postMessage` or an incoming webhook — just a `curl` call, same simplicity as ntfy
- You see the full context on your phone: tool, command, reasoning, recent activity, project/branch
- No interactive buttons — the notification is informational only
- Pair with ntfy for the actual approve/reject decision (see Hybrid approach below)
- **No daemon, no Socket Mode, no persistent process**

**Level 2: Send + incoming webhook (no daemon, no buttons)**
- Even simpler: create an incoming webhook URL in Slack, POST to it
- No bot token or OAuth scopes needed
- Same rich Block Kit formatting
- Still informational only — no interactive response path back

**Level 3: Full interactive (daemon required)**
- Block Kit with native approve (green) / reject (red) / ask (neutral) buttons
- **Threaded conversations** keep each permission request and its follow-up discussion neatly organized
- Can edit the message after response to show "Approved by @you at 3:47 PM" and remove buttons
- No public URL needed — uses Socket Mode (WebSocket)
- **Requires a persistent daemon process** to receive button clicks and DM messages
- See [SLACK-DEEP-DIVE.md](./SLACK-DEEP-DIVE.md) for full details

**The key distinction**: sending to Slack is trivial (one HTTP POST). Receiving *back from* Slack is what requires the daemon. This is why a hybrid approach is appealing.

### Hybrid: Slack for context + ntfy for decisions

The best-of-both-worlds approach, with no daemon needed:

1. Hook fires → parse transcript for context
2. Send a **rich Slack notification** via `chat.postMessage` (tool, command, reasoning, recent activity, project/branch) — this is your "situation report"
3. Simultaneously send an **ntfy notification** with approve/reject/more-info action buttons — this is your "decision interface"
4. Hook script subscribes to the ntfy response topic, blocks until you tap a button

You get Slack's polished formatting for understanding what's going on, and ntfy's zero-infrastructure buttons for acting on it. No daemon, no WebSocket, no persistent process. Just two `curl` calls and a blocking subscribe.

```
Hook fires
  → curl POST to Slack (rich context notification)
  → curl POST to ntfy (approve/reject buttons)
  → curl subscribe to ntfy response topic (blocks)
  → User reads Slack notification for context
  → User taps ntfy button to decide
  → Hook returns allow/deny
```

### Why Not the Others?

- **WhatsApp** — Investigated in depth and **ruled out**. See below.
- **Telegram** — Strong option if you use it. Inline keyboard buttons, message editing, conversational flow. No daemon needed (hook script can poll directly). We don't currently use it, so it would mean adding another app.
- **Pushover** — No native multi-action buttons. Would need an external web page.
- **Discord** — Requires a full bot application for interactive buttons. Overkill.
- **Email/SMS** — Too slow and no native interactivity.
- **Native app / PWA** — Massive overkill. Weeks of development for something ntfy does in 2 minutes.

### WhatsApp — Why It Doesn't Work

WhatsApp was investigated thoroughly as a notification channel since it's already installed and in regular use. The conclusion: **it is fundamentally designed for business-to-customer communication and is a poor fit for developer tooling**.

The problems:

1. **Business verification required** — The WhatsApp Business API requires a Meta Business Manager account with legal business documentation. Verification takes 1-14 business days. There's no "personal developer" tier.

2. **Template messages** — Every outbound notification (the core of this use case) must use a pre-approved message template. You submit the template to Meta, wait for review, and can only use the exact approved format. Changing the format means resubmitting.

3. **24-hour messaging window** — You can only send free-form messages within 24 hours of the user's last reply. Outside that window, you must use a (paid) template message. Since permission requests are unpredictable, you'd frequently be outside the window.

4. **Webhook server mandatory** — Unlike Telegram (polling) or Slack (Socket Mode), WhatsApp has **no polling or WebSocket option**. You must run a publicly reachable HTTPS server with a valid SSL certificate to receive button taps and replies.

5. **Per-message cost** — $0.01-0.05 per template message at US rates. Small but non-zero for something Slack does for free.

6. **Only 3 buttons** — Quick reply messages support a maximum of 3 buttons with 20-character labels. Barely sufficient.

7. **Unofficial alternatives are worse** — Libraries like whatsapp-web.js and Baileys that automate WhatsApp Web have **broken interactive button support** (WhatsApp actively patches against it), violate the Terms of Service, and carry real risk of permanent account bans. A malicious fork ("lotusbail") was discovered on npm in late 2025 that silently exfiltrated all WhatsApp authentication tokens and messages — the supply chain risk is real.

**Bottom line**: To send yourself a notification with 3 buttons, you'd need business verification (weeks), a webhook server, template pre-approval, and per-message fees. Slack does the same thing with a free workspace, Socket Mode (no public URL), styled buttons, and zero per-message cost. There's no scenario where WhatsApp is the right choice for this.

---

## Part 3: End-to-End Architecture

### Option A: ntfy.sh (Recommended Starting Point)

```
┌─────────────────────────┐
│    Claude Code          │
│                         │
│  Permission needed      │
│  for: Bash(rm -rf dist) │
│         │               │
│  PermissionRequest hook │
│  fires                  │
│         │               │
│         ▼               │
│  ┌──────────────────┐   │
│  │ permission-gate  │   │
│  │ (shell script)   │   │
│  │                  │   │
│  │ 1. POST to ntfy  │   │
│  │    /my-alerts    │──────────────► ntfy.sh
│  │                  │   │              │
│  │ 2. Subscribe to  │   │              │ Push notification
│  │    /my-responses │   │              │ with Approve/Reject
│  │    (blocking)    │   │              │ buttons
│  │         │        │   │              │
│  │         │        │   │              ▼
│  │         │        │   │         ┌──────────┐
│  │         │        │   │         │  Phone   │
│  │         │        │   │         │          │
│  │         │        │   │         │ [Approve]│
│  │         │        │   │         │ [Reject] │
│  │         │        │   │         │ [Info]   │
│  │         │        │   │         └────┬─────┘
│  │         │        │   │              │
│  │         │        │   │    User taps │"Approve"
│  │         │        │   │              │
│  │         │◄───────────────────────────┘
│  │  3. Receive      │   │   (HTTP action POSTs
│  │     "approved"   │   │    to /my-responses)
│  │                  │   │
│  │  4. Return JSON: │   │
│  │  {"behavior":    │   │
│  │   "allow"}       │   │
│  └──────────────────┘   │
│         │               │
│  Claude Code proceeds   │
│  with the command        │
└─────────────────────────┘
```

### The Hook Script (Conceptual)

```bash
#!/usr/bin/env bash
# permission-gate.sh
# Called by Claude Code's PermissionRequest hook

# Read the permission request context from stdin
REQUEST=$(cat)
TOOL_NAME=$(echo "$REQUEST" | jq -r '.tool_name // "unknown"')
TOOL_INPUT=$(echo "$REQUEST" | jq -r '.tool_input // "no input"')

# Generate a unique request ID
REQUEST_ID=$(uuidgen | head -c 8)
RESPONSE_TOPIC="claude-responses-${REQUEST_ID}"

# Send notification with approve/reject buttons
curl -s -H "Content-Type: application/json" -d "{
  \"topic\": \"claude-alerts-YOURSECRETTOKEN\",
  \"title\": \"Claude Code Permission Request\",
  \"message\": \"Tool: ${TOOL_NAME}\nInput: ${TOOL_INPUT}\",
  \"priority\": 4,
  \"actions\": [
    {
      \"action\": \"http\",
      \"label\": \"Approve\",
      \"url\": \"https://ntfy.sh/${RESPONSE_TOPIC}\",
      \"method\": \"POST\",
      \"body\": \"approved\"
    },
    {
      \"action\": \"http\",
      \"label\": \"Reject\",
      \"url\": \"https://ntfy.sh/${RESPONSE_TOPIC}\",
      \"method\": \"POST\",
      \"body\": \"rejected\"
    },
    {
      \"action\": \"http\",
      \"label\": \"More Info\",
      \"url\": \"https://ntfy.sh/${RESPONSE_TOPIC}\",
      \"method\": \"POST\",
      \"body\": \"moreinfo\"
    }
  ]
}" https://ntfy.sh > /dev/null

# Wait for response (blocks until a message arrives on the response topic)
# Timeout after 5 minutes
RESPONSE=$(curl -s --max-time 300 "https://ntfy.sh/${RESPONSE_TOPIC}/json?poll=1&since=now" | head -1)

# If no response within timeout, deny by default
if [ -z "$RESPONSE" ]; then
  echo '{"behavior": "deny", "message": "Permission request timed out (no remote response within 5 minutes)"}'
  exit 0
fi

DECISION=$(echo "$RESPONSE" | jq -r '.message // "denied"')

case "$DECISION" in
  approved)
    echo '{"behavior": "allow"}'
    ;;
  rejected)
    echo '{"behavior": "deny", "message": "Rejected remotely"}'
    ;;
  moreinfo)
    # Could send additional context, then wait again
    echo '{"behavior": "deny", "message": "More info requested — check your notifications"}'
    ;;
  *)
    echo '{"behavior": "deny", "message": "Unknown response: '"$DECISION"'"}'
    ;;
esac
```

### The Settings Configuration

```json
{
  "permissions": {
    "allow": [
      "Read",
      "Glob",
      "Grep",
      "Task",
      "WebSearch",
      "WebFetch",
      "Bash(npm test*)",
      "Bash(npm run*)",
      "Bash(git status*)",
      "Bash(git diff*)",
      "Bash(git log*)",
      "Bash(git add*)",
      "Bash(git commit*)",
      "Bash(ls *)"
    ],
    "deny": [
      "Bash(rm -rf /)*",
      "Bash(curl * | sh)*",
      "Bash(wget * | sh)*"
    ]
  },
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/permission-gate.sh"
          }
        ]
      }
    ]
  }
}
```

The flow would be:
1. Claude wants to run a tool
2. If it matches `allow`, it runs immediately — no hook, no notification
3. If it matches `deny`, it's blocked — no hook, no notification
4. If it's anything else, the permission dialog appears, triggering `PermissionRequest`
5. Your hook script fires, sends you a notification, waits for your response
6. You approve or reject from your phone
7. Claude Code continues

### Option B: Telegram Bot (More Polish)

Same architecture, but instead of ntfy topics, you use the Telegram Bot API:

```bash
#!/usr/bin/env bash
# permission-gate-telegram.sh

REQUEST=$(cat)
TOOL_NAME=$(echo "$REQUEST" | jq -r '.tool_name // "unknown"')
TOOL_INPUT=$(echo "$REQUEST" | jq -r '.tool_input // "no input"')
REQUEST_ID=$(uuidgen | head -c 8)

BOT_TOKEN="YOUR_BOT_TOKEN"
CHAT_ID="YOUR_CHAT_ID"

# Send message with inline keyboard
curl -s -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
  -H "Content-Type: application/json" \
  -d "{
    \"chat_id\": \"${CHAT_ID}\",
    \"text\": \"🔐 *Permission Request*\n\n*Tool:* \`${TOOL_NAME}\`\n*Input:* \`${TOOL_INPUT}\`\n\n_Waiting for your decision..._\",
    \"parse_mode\": \"Markdown\",
    \"reply_markup\": {
      \"inline_keyboard\": [[
        {\"text\": \"✅ Approve\", \"callback_data\": \"approve_${REQUEST_ID}\"},
        {\"text\": \"❌ Reject\", \"callback_data\": \"reject_${REQUEST_ID}\"},
        {\"text\": \"ℹ️ Info\", \"callback_data\": \"info_${REQUEST_ID}\"}
      ]]
    }
  }" > /dev/null

# Poll for callback response (simplified — production version needs a persistent bot process)
TIMEOUT=300
ELAPSED=0
DECISION=""
OFFSET=0

while [ $ELAPSED -lt $TIMEOUT ] && [ -z "$DECISION" ]; do
  UPDATES=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/getUpdates?offset=${OFFSET}&timeout=10")

  CALLBACK=$(echo "$UPDATES" | jq -r ".result[] | select(.callback_query.data | startswith(\"approve_${REQUEST_ID}\") or startswith(\"reject_${REQUEST_ID}\") or startswith(\"info_${REQUEST_ID}\")) | .callback_query.data" | head -1)

  if [ -n "$CALLBACK" ]; then
    DECISION=$(echo "$CALLBACK" | cut -d'_' -f1)
    # Acknowledge the callback
    CALLBACK_ID=$(echo "$UPDATES" | jq -r ".result[] | select(.callback_query.data == \"${CALLBACK}\") | .callback_query.id" | head -1)
    curl -s -X POST "https://api.telegram.org/bot${BOT_TOKEN}/answerCallbackQuery" \
      -d "callback_query_id=${CALLBACK_ID}" > /dev/null
  fi

  # Update offset
  NEW_OFFSET=$(echo "$UPDATES" | jq -r '.result[-1].update_id // empty')
  [ -n "$NEW_OFFSET" ] && OFFSET=$((NEW_OFFSET + 1))

  ELAPSED=$((ELAPSED + 10))
done

case "$DECISION" in
  approve) echo '{"behavior": "allow"}' ;;
  reject)  echo '{"behavior": "deny", "message": "Rejected remotely via Telegram"}' ;;
  info)    echo '{"behavior": "deny", "message": "More info requested — check Telegram"}' ;;
  *)       echo '{"behavior": "deny", "message": "Timed out waiting for remote response"}' ;;
esac
```

---

## Part 4: Rich Context — Making Notifications Useful

The raw hook payload gives you `tool_name: "Bash"` and `tool_input: {"command": "npm install some-package"}`. That's not enough to make an informed decision from your phone. You need to know *why* Claude wants to do this, *what it's been working on*, and *what happens if you say no*.

### What Context to Include

There are three tiers of information, matching how much screen space and attention they require:

#### Tier 1: The Notification Banner (what you see at a glance)

This is the push notification as it appears on your lock screen. ~100 characters max. Must answer: "what does Claude want to do?"

```
Title:  Claude Code — Bash
Body:   npm install lodash  ·  my-project (main)
```

Format: `Tool name` in the title, `command/action` + `project (branch)` in the body. For different tools:

| Tool | Banner Body Example |
|---|---|
| Bash | `rm -rf dist/ && npm run build  ·  my-app (feature/auth)` |
| Edit | `Edit src/auth/login.ts (lines 45-52)  ·  my-app (feature/auth)` |
| Write | `Create src/utils/helpers.ts (new file)  ·  my-app (feature/auth)` |
| MCP tool | `mcp: github/create_pull_request  ·  my-app (feature/auth)` |

#### Tier 2: The Expanded Notification (what you see when you pull down or tap)

This is the detail view. Room for ~500-1000 characters. Must answer: "why does Claude want to do this?"

```
Claude Code — Bash
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Command:  npm install lodash
Project:  my-project (main)
Session:  "Add utility functions for data processing"

Why: "I need to install lodash to use its deep-clone
functionality for the data transformation pipeline
I'm building."

Recent: Read src/transform.ts → Edit src/transform.ts
        → Read package.json → [this request]
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
       [Approve]  [Reject]  [More Info]
```

The "Why" field comes from parsing the transcript — specifically, the last assistant text block before the tool_use.

The "Recent" field shows the last 3-4 tool calls to give a sense of the current activity flow.

The "Session" description comes from the first user message (your original prompt).

#### Tier 3: Full Details (what you see when you tap "More Info")

This is a separate message or web page with the complete picture:

- Full command/input (not truncated)
- Claude's complete explanation (entire last assistant text)
- Last 10+ conversation exchanges
- The original task/prompt
- List of files modified so far in the session
- How long the session has been running
- Token usage / how deep into context

### How to Extract Each Piece

Here's a concrete reference for extracting each context element from the hook payload and transcript:

```python
#!/usr/bin/env python3
"""context_extractor.py — Extract rich context from a Claude Code hook payload."""
import json
import sys

# --- Read hook payload from stdin ---
payload = json.load(sys.stdin)
tool_name = payload.get("tool_name", "unknown")
tool_input = payload.get("tool_input", {})
transcript_path = payload.get("transcript_path", "")
cwd = payload.get("cwd", "")
session_id = payload.get("session_id", "")

# --- Parse the transcript ---
entries = []
with open(transcript_path, "r") as f:
    for line in f:
        line = line.strip()
        if line:
            entries.append(json.loads(line))

# --- Extract the original user prompt (first user message) ---
original_prompt = ""
for entry in entries:
    if entry.get("type") == "user":
        msg = entry.get("message", {})
        content = msg.get("content", "")
        if isinstance(content, str):
            original_prompt = content
        elif isinstance(content, list):
            for block in content:
                if isinstance(block, dict) and block.get("type") == "text":
                    original_prompt = block["text"]
        break

# --- Extract Claude's stated reasoning (last assistant text before tool_use) ---
reasoning = ""
for entry in reversed(entries):
    if entry.get("type") == "assistant":
        msg = entry.get("message", {})
        content = msg.get("content", [])
        if isinstance(content, list):
            for block in content:
                if isinstance(block, dict) and block.get("type") == "text":
                    reasoning = block["text"]
                    break
        if reasoning:
            break

# --- Extract recent tool calls (last N tool_use blocks) ---
recent_tools = []
for entry in reversed(entries):
    if entry.get("type") == "assistant":
        msg = entry.get("message", {})
        content = msg.get("content", [])
        if isinstance(content, list):
            for block in content:
                if isinstance(block, dict) and block.get("type") == "tool_use":
                    recent_tools.append(block["name"])
    if len(recent_tools) >= 5:
        break
recent_tools.reverse()

# --- Extract git branch ---
git_branch = ""
for entry in reversed(entries):
    if entry.get("gitBranch"):
        git_branch = entry["gitBranch"]
        break

# --- Extract project name from cwd ---
project_name = cwd.split("/")[-1] if cwd else "unknown"

# --- Format the tool action (human-readable summary) ---
if tool_name == "Bash":
    action = tool_input.get("command", str(tool_input))
elif tool_name == "Edit":
    fp = tool_input.get("file_path", "?")
    action = f"Edit {fp}"
elif tool_name == "Write":
    fp = tool_input.get("file_path", "?")
    action = f"Create/overwrite {fp}"
elif tool_name == "Read":
    fp = tool_input.get("file_path", "?")
    action = f"Read {fp}"
else:
    action = json.dumps(tool_input)[:300]

# --- Build the context object ---
context = {
    "tool_name": tool_name,
    "action": action[:500],
    "project": f"{project_name} ({git_branch})" if git_branch else project_name,
    "session_task": original_prompt[:200],
    "reasoning": reasoning[:500],
    "recent_tools": " → ".join(recent_tools[-4:]),
    "full_input": json.dumps(tool_input),
    "full_reasoning": reasoning,
    "session_id": session_id,
}

json.dump(context, sys.stdout)
```

### Notification Format by Platform

#### ntfy.sh

ntfy supports a title (up to ~256 chars) and message body (no hard limit). Buttons are action objects.

```bash
# Tier 1 + Tier 2 in a single notification
curl -H "Content-Type: application/json" -d "{
  \"topic\": \"${NTFY_TOPIC}\",
  \"title\": \"Claude: ${TOOL_NAME} — ${PROJECT}\",
  \"message\": \"${ACTION}\n\nWhy: ${REASONING}\n\nRecent: ${RECENT_TOOLS}\nTask: ${SESSION_TASK}\",
  \"priority\": 4,
  \"actions\": [
    {\"action\":\"http\",\"label\":\"Approve\", ...},
    {\"action\":\"http\",\"label\":\"Reject\", ...},
    {\"action\":\"http\",\"label\":\"More Info\", ...}
  ]
}" https://ntfy.sh
```

The "More Info" button POSTs `"moreinfo"` to the response topic. The hook script, on receiving this, sends a *second* notification with tier 3 detail.

#### Telegram

Telegram supports markdown formatting and up to 4096 characters per message. Inline keyboard buttons.

```
🔐 Permission Request

Tool: `Bash`
Command: `npm install lodash`
Project: my-project (main)

Why: "I need to install lodash to use its deep-clone
functionality for the data transformation pipeline."

Recent: Read → Edit → Read → [this]
Task: "Add utility functions for data processing"

[✅ Approve]  [❌ Reject]  [ℹ️ More Info]
```

On "More Info", the bot sends a follow-up message in the same chat with the full details. This feels very natural in Telegram — it's just a conversation thread.

---

## Part 5: The "Ask for More Context" Flow

A binary approve/reject isn't enough. Sometimes you look at the notification and think: "I don't understand why it needs to do this. Explain yourself." There are three approaches to handling this, each with different tradeoffs.

### Approach 1: Proactive Context (Pre-Generate Everything)

Don't wait for the user to ask. Parse the transcript when the hook fires and include all context in the first notification. Make "More Info" just show the full untruncated version of what you've already parsed.

**Flow:**
```
Hook fires
  → Parse transcript for reasoning, recent activity, original prompt
  → Send rich notification with context summary
  → User reads context, taps Approve or Reject
```

**Pros:**
- Single round-trip. Fastest possible response time.
- No extra infrastructure needed.
- Works with any notification service.

**Cons:**
- Can't ask Claude to clarify or elaborate — you only get what's already in the transcript.
- The transcript text might not explain the specific reasoning for *this* tool call clearly.
- Fixed context — what if you want to ask "what happens if I reject this?"

### Approach 2: Deny-and-Explain Loop (Use Claude Itself)

When the user taps "More Info", deny the permission with a message that asks Claude to explain itself. Claude receives the denial, explains its reasoning, and retries the tool call. The next permission request notification now includes Claude's explanation.

**Flow:**
```
Hook fires
  → Send notification: "Bash: npm install lodash"
  → User taps "More Info"
  → Hook returns: {"behavior": "deny", "message": "Before I approve this, explain
     why you need to install lodash specifically. What functionality do you need
     from it? Are there alternatives already in the project's dependencies?
     Then try again."}
  → Claude receives denial + message
  → Claude explains: "I need lodash for deep cloning because the project uses
     nested objects in the transform pipeline. I checked package.json and there's
     no existing deep-clone utility. structuredClone() would work but isn't
     available in the target Node version (14.x)."
  → Claude retries: Bash(npm install lodash)
  → Hook fires again
  → Send NEW notification with Claude's explanation included
     (it's now in the transcript, so the context extractor picks it up)
  → User reads explanation, taps Approve
```

**Pros:**
- Gets *Claude's own explanation* in Claude's own words. This is the best possible context.
- Claude can adapt its explanation to your question — you can ask specific things.
- No extra API calls, no external services — just the normal conversation flow.
- The denial message is freeform — you can ask anything: "what will this do?", "are there alternatives?", "what happens if I say no?"

**Cons:**
- Multi-round-trip. User waits for Claude to respond, gets a second notification.
- Burns extra tokens (Claude has to explain and retry).
- The "deny message" needs to be clear enough that Claude understands it should explain and retry, not give up.
- If Claude decides not to retry, the flow breaks. (In practice, Claude almost always retries when given a clear instruction to explain and try again.)

**This is the most interesting approach** because it mirrors how a human would interact at the terminal: "Wait, why do you need that?" → Claude explains → "OK, go ahead."

### Approach 3: Sidecar LLM Summarizer

When the user taps "More Info", the hook script reads the transcript and sends it to a fast/cheap LLM (like Haiku) with a prompt: "Summarize what Claude is doing and why it needs to run this command. Be concise."

**Flow:**
```
Hook fires
  → Send notification: "Bash: npm install lodash"
  → User taps "More Info"
  → Hook reads transcript, sends last ~20 entries to Haiku
  → Haiku returns: "Claude is building a data transformation pipeline.
     It needs lodash for deep cloning nested objects. The project doesn't
     have an existing clone utility and targets Node 14 which lacks
     structuredClone()."
  → Send second notification with Haiku's summary
  → User taps Approve or Reject
```

**Pros:**
- Gets a clear, tailored summary focused specifically on the pending permission.
- Faster than the deny-and-explain loop (doesn't require Claude to re-process).
- Can ask the summarizer specific questions.

**Cons:**
- Extra API cost (small — Haiku is cheap, and transcripts are small).
- Extra complexity (need an API key available to the hook script).
- The summarizer might miss context that Claude's own explanation would include.
- Adds latency for the "More Info" step.

### Recommended: Combine Approaches 1 and 2

The best design layers these:

1. **Default**: Use Approach 1. Parse the transcript proactively, include Claude's last explanation and recent activity in every notification. Most of the time, this is enough context to decide.

2. **Fallback**: When the user taps "More Info", use Approach 2. Deny with a freeform message asking Claude to explain. The denial message could be:
   - A pre-written generic prompt: "Please explain why you need this permission and try again."
   - Or, in Telegram, the user can *type* their question — "what will this command change?" — and that becomes the denial message.

This gives you fast one-tap approval for obvious cases and a conversational back-and-forth for unclear ones.

### Telegram Makes "More Info" Natural

Telegram is particularly well-suited for Approach 2 because:

- The user can type a reply in the chat instead of just tapping a button
- The bot can relay that reply as the denial message to Claude
- Claude's explanation appears as a new message in the same thread
- The updated notification (with Approve/Reject buttons) is a new message below the explanation
- The whole exchange reads like a natural conversation:

```
Bot:    🔐 Permission Request
        Tool: Bash
        Command: npm install lodash
        [Approve] [Reject] [Ask]

You:    Why do you need lodash? Can't you use structuredClone?

Bot:    Claude says:
        "structuredClone isn't available in Node 14 which is
        the project's target runtime. I checked package.json
        and there's no existing deep clone utility. lodash's
        cloneDeep is the standard solution for this environment."

        Tool: Bash
        Command: npm install lodash
        [Approve] [Reject] [Ask]

You:    [taps Approve]

Bot:    ✅ Approved at 3:47 PM
```

With ntfy, the "More Info" flow is less fluid — you'd get a second notification with the explanation and new approve/reject buttons, but there's no threaded conversation. It works, but isn't as elegant.

### Implementation Detail: The Deny-and-Explain Script

Here's how the hook script handles the "More Info" response:

```bash
# ... (after receiving "moreinfo" from the response topic) ...

case "$DECISION" in
  approved)
    echo '{"behavior": "allow"}'
    ;;
  rejected)
    echo '{"behavior": "deny", "message": "Rejected remotely"}'
    ;;
  moreinfo)
    # Deny with a message that asks Claude to explain and retry
    echo '{"behavior": "deny", "message": "I am reviewing this remotely and need more context before approving. Please explain in detail: (1) why you need to run this specific command, (2) what it will change, and (3) what happens if it is not run. Then try again."}'
    ;;
esac
```

When Claude receives this denial, it will:
1. Read the message explaining what you want to know
2. Provide a detailed explanation
3. Attempt the same tool call again
4. The hook fires again — this time, the transcript contains Claude's explanation
5. Your new notification includes that explanation in the context

The hook script should detect "retry after explanation" (e.g., by checking if the preceding transcript entry contains the explanation pattern) and potentially flag it differently: "Claude explained and is retrying. See explanation above."

---

## Part 6: Remaining Open Questions

### 1. Blocking vs. Non-Blocking Hooks

The biggest architectural question: **does the hook block Claude Code while waiting for your response?**

- **Blocking** (as shown above): The hook script waits for your phone response before returning. Claude Code is paused until you respond or it times out. Simple but means Claude is idle.
- **Non-blocking**: The hook immediately denies (or defers), and some separate process monitors a queue. When you approve remotely, it... does what exactly? Claude Code has already moved on.

The blocking approach is simpler and more correct. Claude Code is designed for the hook to return a decision. A 5-minute timeout is reasonable — you get a push notification, glance at your phone, tap approve, and Claude continues within seconds.

### 2. Context Richness

Covered in detail in **Part 4** above. In short: the `transcript_path` field in the hook payload gives access to the full session history. Parse it to extract Claude's reasoning, the original prompt, recent tool calls, and project context. Structure this into three tiers (banner → expanded → full detail) to match the notification UX.

### 3. "Ask for More Context" Flow

Covered in detail in **Part 5** above. The recommended approach combines proactive context extraction (always include what's in the transcript) with a deny-and-explain loop (when the user wants more info, deny the permission with a message asking Claude to explain and retry). Telegram's threaded chat makes this particularly natural.

### 4. Security Considerations

- **Topic guessing (ntfy.sh)**: Anyone who guesses your topic name can read or publish. Use long random topic names, or self-host with auth.
- **Bot token security (Telegram)**: Your bot token is in the script. Store it as an environment variable or in a secrets file.
- **Man-in-the-middle**: Someone could potentially send a fake "approved" to your response topic. Mitigations: use unique per-request response topics, self-host ntfy with HTTPS + auth, or use Telegram's built-in authentication.
- **Timeout behavior**: Default-deny on timeout is the safe choice. You can always re-run.

### 5. Multiple Concurrent Requests

If Claude hits multiple permission prompts quickly (or you're running multiple Claude sessions):

- Each request needs a unique ID to avoid cross-talk
- The notification should include session context (which project, which task)
- The phone UI should make it clear which request you're approving

### 6. Hook Limitations to Be Aware Of

From Claude Code's current implementation:

- **Race conditions**: If the hook takes >1-2 seconds, the permission dialog may flash briefly on the terminal before the hook result arrives. Cosmetic issue, not functional.
- **VS Code extension**: The VS Code integration reportedly ignores `permissionDecision: "ask"` from PreToolUse hooks. CLI should work fine.
- **Self-modification**: Hooks can't prevent Claude from editing the hook script itself (Claude has Edit/Write access). Consider making the script read-only or storing it outside the project.

### 7. What About Claude Code's Built-in Notification Hook?

Claude Code already has a `Notification` event type that fires when it wants to notify the user (including for permission prompts). This is separate from `PermissionRequest` — it's informational, not decisional. You could use it as a secondary alert ("Claude is waiting for permission") without the approve/deny flow, as a belt-and-suspenders approach alongside the PermissionRequest hook.

---

## Part 7: Implementation Roadmap

If you wanted to build this for real, here's a phased approach:

### Phase 1: Proof of Concept (an afternoon)

- Install ntfy app on your phone, subscribe to a test topic
- Write a minimal `permission-gate.sh` that sends a notification and waits for response
- Configure it as a `PermissionRequest` hook in `~/.claude/settings.json`
- Test with Claude Code — ask it to do something that needs permission
- Verify the full round-trip: notification arrives, tap approve, Claude continues

### Phase 2: Polish (a weekend)

- Add request context (tool name, input preview) to the notification
- Implement "More Info" flow (second notification with full details)
- Add timeout handling (default-deny after 5 minutes)
- Add per-request unique response topics to avoid cross-talk
- Handle edge cases: what if the notification fails to send? What if ntfy is down?
- Add a local log file of all permission decisions for audit

### Phase 3: Slack Integration

- Create a Slack app with Socket Mode, Block Kit buttons, and DM support
- Build the daemon process (Python Bolt or Node.js Bolt) that maintains the Socket Mode WebSocket and relays button clicks / DM messages to the hook script via local SQLite or file
- Implement the "Ask" flow: button opens thread, user types question, daemon relays to hook script, hook denies with the question, Claude explains and retries
- Support message updating after response (remove buttons, show "Approved by @you")
- See [SLACK-DEEP-DIVE.md](./SLACK-DEEP-DIVE.md) for full architecture and working code examples

### Phase 4: Production Quality

- Self-host ntfy for security and reliability
- Add session context to notifications (project name, recent Claude activity)
- Support multiple concurrent Claude Code sessions
- Create an installer script that sets up the hook and configures the notification service
- Write tests for the hook script

### Phase 5: Extras (if you want to go further)

- Auto-learning: track which permissions you always approve, suggest adding them to the allow list
- Time-based rules: allow more during work hours, restrict at night
- Per-project permission profiles
- "Approve for this session" button that temporarily adds a permission
- Integration with Claude Code's managed settings for team-wide policies

---

## Part 8: Quick-Start Recipe

For the impatient — here's the minimum to get a working proof of concept:

### 1. Install ntfy on your phone

- Android: [Google Play](https://play.google.com/store/apps/details?id=io.heckel.ntfy) or [F-Droid](https://f-droid.org/en/packages/io.heckel.ntfy/)
- iOS: [App Store](https://apps.apple.com/us/app/ntfy/id1625396347)
- Subscribe to a topic (e.g. `claude-perms-a8f3k2x9`)

### 2. Create the hook script

Save as `~/.claude/permission-gate.sh` and `chmod +x` it:

```bash
#!/usr/bin/env bash
set -euo pipefail

NTFY_TOPIC="claude-perms-YOURSECRETTOKEN"  # change this
REQUEST=$(cat)
TOOL=$(echo "$REQUEST" | jq -r '.tool_name // "unknown tool"')
INPUT=$(echo "$REQUEST" | jq -r '.tool_input // "{}"' | head -c 500)
RID=$(head -c 8 /dev/urandom | xxd -p)
RESP_TOPIC="claude-resp-${RID}"

# Send notification
curl -s -H "Content-Type: application/json" -d "{
  \"topic\": \"${NTFY_TOPIC}\",
  \"title\": \"Claude: ${TOOL}\",
  \"message\": $(echo "$INPUT" | jq -Rs .),
  \"priority\": 4,
  \"actions\": [
    {\"action\":\"http\",\"label\":\"Approve\",\"url\":\"https://ntfy.sh/${RESP_TOPIC}\",\"method\":\"POST\",\"body\":\"approved\"},
    {\"action\":\"http\",\"label\":\"Reject\",\"url\":\"https://ntfy.sh/${RESP_TOPIC}\",\"method\":\"POST\",\"body\":\"rejected\"}
  ]
}" https://ntfy.sh > /dev/null

# Wait up to 5 min for response
RESP=$(curl -s --max-time 300 "https://ntfy.sh/${RESP_TOPIC}/json?poll=1&since=now" | jq -r '.message // empty' | head -1)

if [ "$RESP" = "approved" ]; then
  echo '{"behavior": "allow"}'
else
  echo '{"behavior": "deny", "message": "Denied remotely (or timed out)"}'
fi
```

### 3. Configure Claude Code

Add to `~/.claude/settings.json`:

```json
{
  "permissions": {
    "allow": [
      "Read", "Glob", "Grep", "Task", "WebSearch",
      "Bash(npm test*)", "Bash(npm run*)",
      "Bash(git status*)", "Bash(git diff*)", "Bash(git log*)"
    ]
  },
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/permission-gate.sh"
          }
        ]
      }
    ]
  }
}
```

### 4. Test it

Run Claude Code and ask it to do something that isn't in the allow list. You should get a notification on your phone.

---

## Summary

The building blocks already exist:

| Piece | Solution | Status |
|---|---|---|
| Hook into Claude Code | `PermissionRequest` hook | Built into Claude Code |
| Rich context for decisions | Parse `transcript_path` JSONL for reasoning, original prompt, recent activity | Available via hook payload |
| Send notification to phone | ntfy.sh (simple) or Slack (rich) | Existing services |
| Interactive approve/reject/ask | ntfy HTTP action buttons or Slack Block Kit buttons | Supported natively |
| Get response back | ntfy response topics or Slack Socket Mode daemon | Supported natively |
| "Ask for more context" flow | Deny with question → Claude explains → retries with explanation in transcript | Works with Claude Code's existing behavior |
| Gate Claude Code on the response | Hook script blocks and returns allow/deny | Standard hook behavior |

### Recommended path for us

| Phase | What | Notification Channel |
|---|---|---|
| **1. Proof of concept** | Minimal hook script, basic approve/reject | ntfy.sh only |
| **2. Rich context** | Transcript parsing, context extractor, "More Info" | Hybrid: Slack (context) + ntfy (buttons) |
| **3. Conversational approval** | Slack daemon, "Ask" flow with threads, message editing | Slack full interactive (optional upgrade) |
| **4. Polish** | Self-hosted ntfy, concurrent session support, audit log | Both |

Note: Phase 2's hybrid approach gives you the best UX without running a daemon. You only need Phase 3 if you want the full conversational "ask for more context" flow inside Slack itself. The deny-and-explain loop (Part 5) works with the hybrid approach too — it just uses ntfy's buttons instead of Slack's.

### What we ruled out

- **WhatsApp** — Business verification (weeks), mandatory webhook server, template pre-approval, per-message cost, 24-hour messaging window, max 3 buttons. Fundamentally designed for B2C, not developer tooling.
- **Telegram** — Technically strong but we don't use it. Would mean adding another app.
- **Native app / PWA** — Massive overkill for something a hook script + notification service handles.

The gap isn't technical — it's just glue. A shell script bridges Claude Code's hook system to a push notification service. The proof of concept is an afternoon's work. The Slack integration with conversational "ask for more context" is a weekend project.
