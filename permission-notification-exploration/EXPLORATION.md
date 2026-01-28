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

### The Key Insight

The `PermissionRequest` hook fires **when Claude Code is already waiting for a yes/no from the user**. If your hook script can:

1. Send a notification to your phone
2. Wait for your response
3. Return `allow` or `deny`

...then Claude Code will act on your remote decision as if you'd typed it at the terminal.

---

## Part 2: Notification Delivery Options

See [notification-options/README.md](../permission-notification-research/README.md) for a detailed comparison of 8 notification services. Here's the short version:

### Top Two Options

#### ntfy.sh — Best for simplicity and self-hosting

- Send: `curl -d "message" ntfy.sh/your-topic`
- Native approve/reject buttons via `http` actions
- Response comes back via a second topic
- Self-hostable, open source, free
- No account needed for prototyping

#### Telegram Bot — Best for rich interaction

- Inline keyboard buttons (approve/reject/more info)
- Can edit the message after response ("Approved at 3:45 PM")
- Threaded follow-up conversation possible
- Free, no message limits
- Requires Telegram app + bot setup

### Why Not the Others?

- **Pushover** — No native multi-action buttons. Would need an external web page.
- **Slack/Discord** — Good if your team already uses them, but heavyweight for a personal tool.
- **Email/SMS** — Too slow and no native interactivity.
- **Native app / PWA** — Massive overkill. Weeks of development for something ntfy does in 2 minutes.

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

## Part 4: Open Questions and Challenges

### 1. Blocking vs. Non-Blocking Hooks

The biggest architectural question: **does the hook block Claude Code while waiting for your response?**

- **Blocking** (as shown above): The hook script waits for your phone response before returning. Claude Code is paused until you respond or it times out. Simple but means Claude is idle.
- **Non-blocking**: The hook immediately denies (or defers), and some separate process monitors a queue. When you approve remotely, it... does what exactly? Claude Code has already moved on.

The blocking approach is simpler and more correct. Claude Code is designed for the hook to return a decision. A 5-minute timeout is reasonable — you get a push notification, glance at your phone, tap approve, and Claude continues within seconds.

### 2. Context Richness

A permission prompt on the terminal shows you exactly what Claude wants to do. A phone notification has limited space. Options:

- **Short summary in the notification** — Tool name + first ~200 chars of input. Enough for most decisions.
- **"More Info" button** — Tapping sends a follow-up notification with the full context, or opens a web page with the complete request details.
- **Web dashboard** — A small web app that shows the full request, recent history, what Claude has been doing. Opens from the notification. Most effort but best experience.

### 3. "More Info" Flow

When the user taps "More Info":

- Option A: Send a second notification with the full context, then wait for approve/reject on a third round
- Option B: Open a web page with the full context + approve/reject buttons
- Option C: In Telegram, send a follow-up message in the chat with expanded details and a new set of buttons

Option C (with Telegram) is the most natural. Option B works with any notification service but requires running a small web server.

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

## Part 5: Implementation Roadmap

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

### Phase 3: Production Quality (a week-ish)

- Self-host ntfy for security and reliability
- Build a small web dashboard showing pending/recent permission requests
- Add session context to notifications (project name, recent Claude activity)
- Support multiple concurrent Claude Code sessions
- Add a Telegram bot option for richer interaction
- Create an installer script that sets up the hook and configures the notification service
- Write tests for the hook script

### Phase 4: Extras (if you want to go further)

- Auto-learning: track which permissions you always approve, suggest adding them to the allow list
- Time-based rules: allow more during work hours, restrict at night
- Per-project permission profiles
- "Approve for this session" button that temporarily adds a permission
- Integration with Claude Code's managed settings for team-wide policies

---

## Part 6: Quick-Start Recipe

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
| Send notification to phone | ntfy.sh or Telegram Bot | Existing services, trivial to integrate |
| Get response back | ntfy response topics or Telegram callbacks | Supported natively |
| Approve/reject from phone | Action buttons in notification | Supported by ntfy and Telegram |
| Gate Claude Code on the response | Hook script blocks and returns allow/deny | Standard hook behavior |

The gap isn't technical — it's just glue. A single shell script (~30 lines) bridges Claude Code's hook system to a push notification service. The proof of concept is an afternoon's work. The polished version is a weekend project.
