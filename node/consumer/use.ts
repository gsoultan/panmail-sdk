// A consumer's view of the package: the published entry point and nothing
// else — no src/, no internal paths.
//
// This compiles against dist/*.d.ts with skipLibCheck off, which is the point.
// The declarations reference ambient types the package does not ship (fetch,
// Headers, ReadableStream), so they are only usable by a consumer who has Node
// types or the DOM lib. That is a real constraint on anyone installing this,
// and it is declared as an optional peer dependency rather than left to be
// discovered. Here it is held to.
import {
  ApiError,
  AuthError,
  BacklogFullError,
  PanmailClient,
  PanmailError,
  RateLimitedError,
  Status,
  TransportError,
  type Attachment,
  type ClientOptions,
  type Message,
  type Result,
} from '../dist/index.js';

const options: ClientOptions = {
  timeoutMs: 5_000,
  rateLimitRetries: 2,
  headers: { 'X-Request-Id': 'abc' },
};

const client = new PanmailClient('https://mail.example.com', 'key', options);

const attachment: Attachment = { filename: 'receipt.pdf', content: new Uint8Array([1, 2]) };

const message: Message = {
  providerId: '3f1c2b7a-0000-4000-8000-000000000001',
  from: 'app@example.com',
  to: ['user@example.net'],
  subject: 'Your receipt',
  html: '<p>Thanks.</p>',
  attachments: [attachment],
};

// Every refusal the README tells a caller to branch on has to be reachable
// from the published surface, and carry the fields it is documented to carry.
export async function send(): Promise<string> {
  try {
    const result: Result = await client.send(message);
    return result.status === Status.Pending ? result.messageId : result.status;
  } catch (error) {
    if (error instanceof RateLimitedError) return `retry after ${error.retryAfter}s`;
    if (error instanceof BacklogFullError) return `backlog: ${error.code} ${error.status}`;
    if (error instanceof AuthError) return `auth: ${error.message}`;
    if (error instanceof ApiError) return `api: ${error.code}`;
    if (error instanceof TransportError) return `transport: ${String(error.cause)}`;
    if (error instanceof PanmailError) return error.name;
    throw error;
  }
}
