/**
 * Parse Twitch IRC messages into structured data
 * Twitch IRC follows the IRCv3 specification with tags
 */

/**
 * Parse IRC tags into an object
 * @param {string} tagsString - Raw tags string from IRC message
 * @returns {Object} Parsed tags
 */
function parseTags(tagsString) {
  if (!tagsString) return {};

  const tags = {};
  const parts = tagsString.split(';');

  for (const part of parts) {
    const [key, value] = part.split('=');
    // Unescape IRC tag values
    tags[key] = value
      ? value
          .replace(/\\s/g, ' ')
          .replace(/\\n/g, '\n')
          .replace(/\\r/g, '\r')
          .replace(/\\:/g, ';')
          .replace(/\\\\/g, '\\')
      : true;
  }

  return tags;
}

/**
 * Parse the prefix (source) of an IRC message
 * @param {string} prefixString - Raw prefix string
 * @returns {Object} Parsed prefix with nick, user, host
 */
function parsePrefix(prefixString) {
  if (!prefixString) return null;

  const match = prefixString.match(/^(?:(\w+)!)?(?:(\w+)@)?(.+)$/);
  if (!match) return { raw: prefixString };

  return {
    nick: match[1] || null,
    user: match[2] || null,
    host: match[3] || null,
  };
}

/**
 * Parse a raw IRC message into a structured object
 * Format: [@tags] [:prefix] command [params] [:trailing]
 * @param {string} rawMessage - Raw IRC message
 * @returns {Object} Parsed message
 */
export function parseMessage(rawMessage) {
  const message = {
    raw: rawMessage,
    tags: {},
    prefix: null,
    command: null,
    params: [],
    channel: null,
    content: null,
  };

  let remaining = rawMessage.trim();

  // Parse tags (starts with @)
  if (remaining.startsWith('@')) {
    const spaceIndex = remaining.indexOf(' ');
    message.tags = parseTags(remaining.slice(1, spaceIndex));
    remaining = remaining.slice(spaceIndex + 1);
  }

  // Parse prefix (starts with :)
  if (remaining.startsWith(':')) {
    const spaceIndex = remaining.indexOf(' ');
    message.prefix = parsePrefix(remaining.slice(1, spaceIndex));
    remaining = remaining.slice(spaceIndex + 1);
  }

  // Parse command and params
  const trailingIndex = remaining.indexOf(' :');
  let paramsSection;
  let trailing = null;

  if (trailingIndex !== -1) {
    paramsSection = remaining.slice(0, trailingIndex);
    trailing = remaining.slice(trailingIndex + 2);
  } else {
    paramsSection = remaining;
  }

  const paramsParts = paramsSection.split(' ').filter(Boolean);
  message.command = paramsParts[0];
  message.params = paramsParts.slice(1);

  if (trailing !== null) {
    message.params.push(trailing);
  }

  // Extract channel and content for chat messages
  if (message.command === 'PRIVMSG' && message.params.length >= 2) {
    message.channel = message.params[0].replace('#', '');
    message.content = message.params[1];
  }

  return message;
}

/**
 * Format a chat message for display
 * @param {Object} message - Parsed message object
 * @returns {string} Formatted display string
 */
export function formatChatMessage(message) {
  if (message.command !== 'PRIVMSG') return null;

  const username = message.tags['display-name'] || message.prefix?.nick || 'Unknown';
  const timestamp = new Date().toLocaleTimeString();
  const content = message.content;

  // Check for special message types
  const isSubscriber = message.tags.subscriber === '1';
  const isMod = message.tags.mod === '1';
  const isVip = message.tags.vip === '1';
  const isBroadcaster = message.tags.badges?.includes('broadcaster');

  // Build badges string
  const badges = [];
  if (isBroadcaster) badges.push('[BROADCASTER]');
  if (isMod) badges.push('[MOD]');
  if (isVip) badges.push('[VIP]');
  if (isSubscriber) badges.push('[SUB]');

  const badgeStr = badges.length > 0 ? badges.join('') + ' ' : '';

  return `[${timestamp}] ${badgeStr}${username}: ${content}`;
}

/**
 * Check if a message is a chat message
 * @param {Object} message - Parsed message object
 * @returns {boolean}
 */
export function isChatMessage(message) {
  return message.command === 'PRIVMSG';
}
