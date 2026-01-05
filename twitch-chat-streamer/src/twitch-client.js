import WebSocket from 'ws';
import { EventEmitter } from 'events';
import { parseMessage, formatChatMessage, isChatMessage } from './message-parser.js';

const TWITCH_IRC_URL = 'wss://irc-ws.chat.twitch.tv:443';

/**
 * TwitchChatClient - Connects to Twitch chat via IRC over WebSocket
 * Uses anonymous connection (justinfan) for read-only access
 */
export class TwitchChatClient extends EventEmitter {
  constructor(options = {}) {
    super();
    this.ws = null;
    this.channels = new Set();
    this.connected = false;
    this.reconnectAttempts = 0;
    this.maxReconnectAttempts = options.maxReconnectAttempts ?? 5;
    this.reconnectDelay = options.reconnectDelay ?? 1000;
    this.pingInterval = null;
    this.debug = options.debug ?? false;
  }

  /**
   * Generate anonymous username for read-only access
   */
  _generateAnonUsername() {
    const randomNum = Math.floor(Math.random() * 80000) + 1000;
    return `justinfan${randomNum}`;
  }

  /**
   * Log debug messages
   */
  _log(...args) {
    if (this.debug) {
      console.log('[DEBUG]', ...args);
    }
  }

  /**
   * Send raw IRC command
   */
  _send(message) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this._log('SEND:', message);
      this.ws.send(message);
    }
  }

  /**
   * Handle incoming IRC message
   */
  _handleMessage(rawMessage) {
    // IRC messages can be batched with \r\n
    const messages = rawMessage.split('\r\n').filter(Boolean);

    for (const msg of messages) {
      this._log('RECV:', msg);

      const parsed = parseMessage(msg);

      switch (parsed.command) {
        case 'PING':
          // Respond to PING to stay connected
          this._send(`PONG :${parsed.params[0] || 'tmi.twitch.tv'}`);
          break;

        case 'PRIVMSG':
          // Emit chat message events
          this.emit('message', parsed);
          this.emit('chat', {
            channel: parsed.channel,
            username: parsed.tags['display-name'] || parsed.prefix?.nick,
            content: parsed.content,
            tags: parsed.tags,
            formatted: formatChatMessage(parsed),
          });
          break;

        case 'JOIN':
          this.emit('join', {
            channel: parsed.params[0]?.replace('#', ''),
            username: parsed.prefix?.nick,
          });
          break;

        case 'PART':
          this.emit('part', {
            channel: parsed.params[0]?.replace('#', ''),
            username: parsed.prefix?.nick,
          });
          break;

        case 'NOTICE':
          this.emit('notice', {
            channel: parsed.params[0]?.replace('#', ''),
            message: parsed.params[1],
          });
          break;

        case 'USERNOTICE':
          // Subscriptions, raids, etc.
          this.emit('usernotice', {
            channel: parsed.params[0]?.replace('#', ''),
            type: parsed.tags['msg-id'],
            message: parsed.params[1] || '',
            tags: parsed.tags,
          });
          break;

        case 'CLEARCHAT':
          this.emit('clearchat', {
            channel: parsed.params[0]?.replace('#', ''),
            username: parsed.params[1] || null,
            duration: parsed.tags['ban-duration'] || null,
          });
          break;

        case 'CLEARMSG':
          this.emit('clearmsg', {
            channel: parsed.params[0]?.replace('#', ''),
            targetMsgId: parsed.tags['target-msg-id'],
          });
          break;

        case 'ROOMSTATE':
          this.emit('roomstate', {
            channel: parsed.params[0]?.replace('#', ''),
            tags: parsed.tags,
          });
          break;

        case '001': // Welcome
        case '002':
        case '003':
        case '004':
        case '375': // MOTD start
        case '372': // MOTD
        case '376': // MOTD end
          // Connection messages - ignore
          break;

        case '353': // NAMES list
        case '366': // End of NAMES
          // Names list - ignore for now
          break;

        case 'CAP':
          // Capability acknowledgment
          if (parsed.params.includes('ACK')) {
            this._log('Capabilities acknowledged');
          }
          break;

        default:
          // Emit unknown commands for debugging
          this.emit('raw', parsed);
      }
    }
  }

  /**
   * Connect to Twitch IRC
   */
  connect() {
    return new Promise((resolve, reject) => {
      if (this.connected) {
        resolve();
        return;
      }

      this.ws = new WebSocket(TWITCH_IRC_URL);

      this.ws.on('open', () => {
        this._log('WebSocket connected');

        // Request capabilities for tags and commands
        this._send('CAP REQ :twitch.tv/tags twitch.tv/commands');

        // Anonymous authentication
        const username = this._generateAnonUsername();
        this._send(`NICK ${username}`);

        this.connected = true;
        this.reconnectAttempts = 0;
        this.emit('connected');

        // Rejoin channels after reconnect
        for (const channel of this.channels) {
          this._send(`JOIN #${channel}`);
        }

        resolve();
      });

      this.ws.on('message', (data) => {
        this._handleMessage(data.toString());
      });

      this.ws.on('close', () => {
        this._log('WebSocket closed');
        this.connected = false;
        this.emit('disconnected');
        this._attemptReconnect();
      });

      this.ws.on('error', (error) => {
        this._log('WebSocket error:', error.message);
        this.emit('error', error);
        if (!this.connected) {
          reject(error);
        }
      });
    });
  }

  /**
   * Attempt to reconnect with exponential backoff
   */
  _attemptReconnect() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      this.emit('error', new Error('Max reconnection attempts reached'));
      return;
    }

    this.reconnectAttempts++;
    const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);

    this._log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);
    this.emit('reconnecting', { attempt: this.reconnectAttempts, delay });

    setTimeout(() => {
      this.connect().catch((err) => {
        this._log('Reconnection failed:', err.message);
      });
    }, delay);
  }

  /**
   * Join a channel
   * @param {string} channel - Channel name (without #)
   */
  join(channel) {
    const channelName = channel.toLowerCase().replace('#', '');
    this.channels.add(channelName);

    if (this.connected) {
      this._send(`JOIN #${channelName}`);
    }

    return this;
  }

  /**
   * Leave a channel
   * @param {string} channel - Channel name (without #)
   */
  part(channel) {
    const channelName = channel.toLowerCase().replace('#', '');
    this.channels.delete(channelName);

    if (this.connected) {
      this._send(`PART #${channelName}`);
    }

    return this;
  }

  /**
   * Disconnect from Twitch IRC
   */
  disconnect() {
    if (this.pingInterval) {
      clearInterval(this.pingInterval);
    }

    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }

    this.connected = false;
    this.channels.clear();
  }
}
