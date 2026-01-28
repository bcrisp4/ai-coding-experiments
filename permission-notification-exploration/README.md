# Permission Notification Exploration

Exploring how to build a system that notifies you on your phone when Claude Code needs permission to do something, so you can approve or reject remotely.

## The Problem

Running Claude Code autonomously means choosing between babysitting the terminal, pre-approving everything, or dangerously skipping permissions. This project explores a middle ground: pre-approve common operations, get push notifications for anything unexpected, and approve/reject/ask-for-context from your phone.

## What's Here

- **[EXPLORATION.md](./EXPLORATION.md)** — Full exploration covering:
  - How Claude Code's hook system works (PermissionRequest, PreToolUse, etc.)
  - The hook payload schema and session transcript JSONL format
  - Rich context extraction from the transcript (Claude's reasoning, original prompt, recent activity)
  - Three-tier notification design (banner, expanded, full detail)
  - "Ask for more context" flow: deny-and-explain loop where you reject with a question, Claude explains and retries
  - End-to-end architecture diagrams (ntfy.sh and Slack options)
  - Why WhatsApp doesn't work for this
  - Phased implementation roadmap
  - Quick-start recipe for a proof of concept

- **[SLACK-DEEP-DIVE.md](./SLACK-DEEP-DIVE.md)** — Comprehensive research on Slack as the interactive notification platform:
  - Slack app setup (free workspace works), OAuth scopes, Socket Mode
  - Block Kit formatting for rich permission notifications
  - Interactive buttons (approve/reject/ask) with styled primary/danger buttons
  - Socket Mode daemon architecture (why it's needed, how it works)
  - The "type a question" flow using Slack threads
  - Working Python and Node.js daemon examples
  - Comparison with Telegram and ntfy.sh

- **[Notification Options Research](../permission-notification-research/README.md)** — Detailed comparison of 8 notification delivery services including Pushover, ntfy.sh, Telegram Bot API, Discord, Slack, PWA, native apps, and email/SMS.

## Key Findings

1. **Claude Code's `PermissionRequest` hook** fires a shell command when permission is needed. The hook blocks Claude Code until the script returns allow/deny.

2. **The hook payload includes `transcript_path`** — the full path to the session's JSONL transcript. By parsing this, the hook can extract Claude's stated reasoning, your original prompt, and recent activity to include in the notification.

3. **Slack is the best interactive option** if you already use it. Block Kit buttons, threaded conversations for the "ask for context" flow, message editing after response. Requires a persistent daemon process for Socket Mode.

4. **ntfy.sh is the best starting point** for a quick proof of concept. Zero setup, binary approve/reject via HTTP action buttons.

5. **The "ask for more context" flow** works by denying the permission with a message asking Claude to explain, then Claude explains and retries. The next notification includes the explanation. This mirrors natural terminal interaction.

6. **WhatsApp was ruled out** — business verification, template pre-approval, mandatory webhook server, per-message cost, 24-hour messaging window. Designed for B2C, not developer tooling.

## Status

Research and exploration only. No working implementation yet.
