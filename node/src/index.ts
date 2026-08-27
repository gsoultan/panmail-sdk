export { PanmailClient, DEFAULT_TIMEOUT_MS, type ClientOptions } from './client.js';
export { Status, toWire, type Attachment, type Message, type Result } from './message.js';
export {
  ApiError,
  AuthError,
  BacklogFullError,
  InvalidMessageError,
  PanmailError,
  RateLimitedError,
  TransportError,
} from './errors.js';
