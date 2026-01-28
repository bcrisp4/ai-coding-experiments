# Permission Notification Exploration

Exploring how to build a system that notifies you on your phone when Claude Code needs permission to do something, so you can approve or reject remotely.

## The Problem

Running Claude Code autonomously means choosing between babysitting the terminal, pre-approving everything, or dangerously skipping permissions. This project explores a middle ground: pre-approve common operations, get push notifications for anything unexpected.

## What's Here

- **[EXPLORATION.md](./EXPLORATION.md)** — Full exploration covering:
  - How Claude Code's hook system works (PermissionRequest, PreToolUse, etc.)
  - How to wire a hook to send push notifications and wait for a response
  - End-to-end architecture diagrams (ntfy.sh and Telegram options)
  - Open questions (blocking vs non-blocking, context richness, security, concurrency)
  - Phased implementation roadmap
  - Quick-start recipe for a proof of concept

- **[Notification Options Research](../permission-notification-research/README.md)** — Detailed comparison of 8 notification delivery services including Pushover, ntfy.sh, Telegram Bot API, Discord, Slack, PWA, native apps, and email/SMS.

## Key Finding

The building blocks already exist. Claude Code's `PermissionRequest` hook fires a shell command when permission is needed. That shell command can send a push notification (via ntfy.sh or Telegram), wait for your response, and return allow/deny. The proof of concept is roughly 30 lines of bash.

## Status

Research and exploration only. No working implementation yet.
