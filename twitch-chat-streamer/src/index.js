#!/usr/bin/env node

import { TwitchChatClient } from './twitch-client.js';
import { createInterface } from 'readline';

// ANSI color codes for terminal output
const colors = {
  reset: '\x1b[0m',
  bright: '\x1b[1m',
  dim: '\x1b[2m',
  red: '\x1b[31m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  magenta: '\x1b[35m',
  cyan: '\x1b[36m',
};

/**
 * Parse command line arguments
 */
function parseArgs() {
  const args = process.argv.slice(2);
  const options = {
    channels: [],
    debug: false,
    json: false,
    showEvents: false,
  };

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];

    if (arg === '-h' || arg === '--help') {
      printHelp();
      process.exit(0);
    } else if (arg === '-d' || arg === '--debug') {
      options.debug = true;
    } else if (arg === '-j' || arg === '--json') {
      options.json = true;
    } else if (arg === '-e' || arg === '--events') {
      options.showEvents = true;
    } else if (arg === '-c' || arg === '--channel') {
      if (args[i + 1] && !args[i + 1].startsWith('-')) {
        options.channels.push(args[++i].toLowerCase());
      }
    } else if (!arg.startsWith('-')) {
      // Treat positional arguments as channel names
      options.channels.push(arg.toLowerCase());
    }
  }

  return options;
}

/**
 * Print help message
 */
function printHelp() {
  console.log(`
${colors.bright}Twitch Chat Streamer${colors.reset}
Stream Twitch.tv chat messages in realtime.

${colors.bright}USAGE:${colors.reset}
  node src/index.js [OPTIONS] [CHANNELS...]

${colors.bright}ARGUMENTS:${colors.reset}
  CHANNELS    Channel names to join (without #)

${colors.bright}OPTIONS:${colors.reset}
  -c, --channel <name>  Add a channel to join (can be used multiple times)
  -d, --debug           Enable debug output
  -j, --json            Output messages as JSON
  -e, --events          Show all events (joins, subs, raids, etc.)
  -h, --help            Show this help message

${colors.bright}EXAMPLES:${colors.reset}
  node src/index.js xqc
  node src/index.js -c shroud -c pokimane
  node src/index.js --json summit1g
  npm start -- xqc --events

${colors.bright}INTERACTIVE COMMANDS:${colors.reset}
  /join <channel>       Join a channel
  /part <channel>       Leave a channel
  /channels             List joined channels
  /quit                 Exit the application
`);
}

/**
 * Print a status message
 */
function printStatus(message, type = 'info') {
  const colorMap = {
    info: colors.cyan,
    success: colors.green,
    warning: colors.yellow,
    error: colors.red,
  };
  console.log(`${colorMap[type] || colors.reset}[*] ${message}${colors.reset}`);
}

/**
 * Main application
 */
async function main() {
  const options = parseArgs();

  if (options.channels.length === 0) {
    console.log(`${colors.yellow}No channels specified. Use --help for usage information.${colors.reset}`);
    console.log(`\nEnter a channel name to start streaming:`);
  }

  const client = new TwitchChatClient({ debug: options.debug });

  // Set up event handlers
  client.on('connected', () => {
    printStatus('Connected to Twitch IRC', 'success');
  });

  client.on('disconnected', () => {
    printStatus('Disconnected from Twitch IRC', 'warning');
  });

  client.on('reconnecting', ({ attempt, delay }) => {
    printStatus(`Reconnecting (attempt ${attempt}) in ${delay}ms...`, 'warning');
  });

  client.on('error', (error) => {
    printStatus(`Error: ${error.message}`, 'error');
  });

  client.on('chat', (msg) => {
    if (options.json) {
      console.log(JSON.stringify(msg));
    } else {
      console.log(msg.formatted);
    }
  });

  if (options.showEvents) {
    client.on('join', ({ channel, username }) => {
      printStatus(`${username} joined #${channel}`, 'info');
    });

    client.on('part', ({ channel, username }) => {
      printStatus(`${username} left #${channel}`, 'info');
    });

    client.on('usernotice', ({ channel, type, tags }) => {
      const displayName = tags['display-name'] || 'Someone';
      switch (type) {
        case 'sub':
        case 'resub':
          const months = tags['msg-param-cumulative-months'] || 1;
          printStatus(`${displayName} subscribed for ${months} months!`, 'success');
          break;
        case 'subgift':
          const recipient = tags['msg-param-recipient-display-name'];
          printStatus(`${displayName} gifted a sub to ${recipient}!`, 'success');
          break;
        case 'raid':
          const viewers = tags['msg-param-viewerCount'];
          printStatus(`${displayName} is raiding with ${viewers} viewers!`, 'success');
          break;
        default:
          printStatus(`[${type}] ${displayName}`, 'info');
      }
    });

    client.on('clearchat', ({ channel, username, duration }) => {
      if (username) {
        printStatus(`${username} was ${duration ? `timed out for ${duration}s` : 'banned'} in #${channel}`, 'warning');
      } else {
        printStatus(`Chat cleared in #${channel}`, 'warning');
      }
    });

    client.on('roomstate', ({ channel, tags }) => {
      const modes = [];
      if (tags['emote-only'] === '1') modes.push('emote-only');
      if (tags['followers-only'] !== '-1') modes.push('followers-only');
      if (tags['slow']) modes.push(`slow(${tags['slow']}s)`);
      if (tags['subs-only'] === '1') modes.push('subs-only');
      if (tags.r9k === '1') modes.push('r9k');
      if (modes.length > 0) {
        printStatus(`#${channel} room modes: ${modes.join(', ')}`, 'info');
      }
    });
  }

  // Connect and join channels
  try {
    await client.connect();

    for (const channel of options.channels) {
      printStatus(`Joining #${channel}...`, 'info');
      client.join(channel);
    }
  } catch (error) {
    printStatus(`Failed to connect: ${error.message}`, 'error');
    process.exit(1);
  }

  // Set up interactive input
  const rl = createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  rl.on('line', (input) => {
    const line = input.trim();

    if (line.startsWith('/')) {
      const [command, ...args] = line.slice(1).split(' ');

      switch (command.toLowerCase()) {
        case 'join':
          if (args[0]) {
            const channel = args[0].toLowerCase().replace('#', '');
            printStatus(`Joining #${channel}...`, 'info');
            client.join(channel);
          } else {
            printStatus('Usage: /join <channel>', 'warning');
          }
          break;

        case 'part':
        case 'leave':
          if (args[0]) {
            const channel = args[0].toLowerCase().replace('#', '');
            printStatus(`Leaving #${channel}...`, 'info');
            client.part(channel);
          } else {
            printStatus('Usage: /part <channel>', 'warning');
          }
          break;

        case 'channels':
        case 'list':
          const channels = Array.from(client.channels);
          if (channels.length > 0) {
            printStatus(`Joined channels: ${channels.map(c => '#' + c).join(', ')}`, 'info');
          } else {
            printStatus('Not joined to any channels', 'warning');
          }
          break;

        case 'quit':
        case 'exit':
          printStatus('Goodbye!', 'success');
          client.disconnect();
          rl.close();
          process.exit(0);
          break;

        case 'help':
          console.log(`
${colors.bright}Interactive Commands:${colors.reset}
  /join <channel>   Join a channel
  /part <channel>   Leave a channel
  /channels         List joined channels
  /quit             Exit the application
`);
          break;

        default:
          printStatus(`Unknown command: /${command}. Type /help for available commands.`, 'warning');
      }
    } else if (line.length > 0 && !options.channels.length && client.channels.size === 0) {
      // If no channels joined and user types something, treat it as a channel name
      const channel = line.toLowerCase().replace('#', '');
      printStatus(`Joining #${channel}...`, 'info');
      client.join(channel);
    }
  });

  // Handle graceful shutdown
  process.on('SIGINT', () => {
    console.log('\n');
    printStatus('Shutting down...', 'info');
    client.disconnect();
    rl.close();
    process.exit(0);
  });
}

main().catch((error) => {
  console.error(`${colors.red}Fatal error: ${error.message}${colors.reset}`);
  process.exit(1);
});
