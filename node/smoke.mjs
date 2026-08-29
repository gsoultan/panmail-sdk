// Runs the built package on the Node it was started with.
//
// The test suite is written for bun:test and runs under bun, which means
// nothing in CI ever executed this package on Node — and package.json promises
// `engines: { node: ">=18" }` to everyone who installs it. Bun is not Node: it
// has its own fetch, its own streams and its own idea of what is global. This
// script is the promise being kept rather than assumed.
//
// It deliberately exercises the version-sensitive paths rather than just
// importing: global fetch, ReadableStream.getReader, TextDecoder, and undici's
// handling of `redirect: 'error'`. Those are the four things that would break
// first on an older runtime.
//
// Usage: node smoke.mjs   (after `bun run build`)

import assert from 'node:assert/strict';
import http from 'node:http';

import {
  PanmailClient,
  RateLimitedError,
  TransportError,
  Status,
} from './dist/index.js';

const listen = async (handler) => {
  const server = http.createServer(handler);
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { server, url: `http://127.0.0.1:${server.address().port}` };
};

const message = {
  providerId: '3f1c2b7a-0000-4000-8000-000000000001',
  from: 'app@example.com',
  to: ['user@example.net'],
  subject: 'Hello',
  text: 'hi there',
};

let failures = 0;
const check = async (name, fn) => {
  try {
    await fn();
    console.log(`  ok    ${name}`);
  } catch (error) {
    failures++;
    console.log(`  FAIL  ${name}\n        ${error?.message ?? error}`);
  }
};

console.log(`smoke: node ${process.version}`);

await check('a send is accepted and carries the api key', async () => {
  let seenKey;
  const { server, url } = await listen((req, res) => {
    seenKey = req.headers['x-api-key'];
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ messageId: 'msg_01', status: Status.Pending }));
  });
  try {
    const result = await new PanmailClient(url, 'test-key').send(message);
    assert.equal(result.messageId, 'msg_01');
    assert.equal(result.status, Status.Pending);
    assert.equal(seenKey, 'test-key');
  } finally {
    server.close();
  }
});

await check('a rate limit arrives as a typed refusal with its delay', async () => {
  const { server, url } = await listen((_req, res) => {
    res.writeHead(429, { 'Content-Type': 'application/json', 'Retry-After': '2' });
    res.end(JSON.stringify({ code: 'resource_exhausted', message: 'over the send rate' }));
  });
  try {
    await assert.rejects(
      () => new PanmailClient(url, 'test-key').send(message),
      (error) => error instanceof RateLimitedError && error.retryAfter === 2,
    );
  } finally {
    server.close();
  }
});

// Exercises ReadableStream.getReader() and TextDecoder on this runtime.
await check('an oversized response is refused rather than buffered', async () => {
  const { server, url } = await listen((_req, res) => {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    const chunk = 'x'.repeat(64 * 1024);
    for (let written = 0; written < 4 * 1024 * 1024; written += chunk.length) {
      res.write(chunk);
    }
    res.end();
  });
  try {
    await assert.rejects(
      () => new PanmailClient(url, 'test-key').send(message),
      (error) => error instanceof TransportError && /exceeded/.test(error.message),
    );
  } finally {
    server.close();
  }
});

// Exercises undici's `redirect: 'error'` on this runtime. The security
// property the SDK documents is worth nothing if this version ignores it.
await check('a redirect is refused and the api key does not follow it', async () => {
  let leaked;
  const elsewhere = await listen((req, res) => {
    leaked = req.headers['x-api-key'];
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end('{"messageId":"leaked"}');
  });
  const redirecting = await listen((_req, res) => {
    res.writeHead(307, { Location: `${elsewhere.url}/steal` });
    res.end();
  });
  try {
    await assert.rejects(
      () => new PanmailClient(redirecting.url, 'secret-key').send(message),
      (error) => error instanceof TransportError,
    );
    assert.equal(leaked, undefined, `the api key reached the redirect target: ${leaked}`);
  } finally {
    elsewhere.server.close();
    redirecting.server.close();
  }
});

if (failures > 0) {
  console.log(`\nsmoke: ${failures} failed on node ${process.version}`);
  process.exit(1);
}
console.log(`\nsmoke: all passed on node ${process.version}`);
