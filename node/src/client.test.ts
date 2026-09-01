import { readFileSync } from 'node:fs';

import { describe, expect, test } from 'bun:test';
import { PanmailClient } from './client.js';
import {
  ApiError,
  AuthError,
  BacklogFullError,
  InvalidMessageError,
  RateLimitedError,
  TransportError,
} from './errors.js';
import { Status, type Message } from './message.js';

const BASE = 'https://mail.example.com';

interface Call {
  url: string;
  init: RequestInit;
  body: Record<string, unknown>;
}

/** A fetch that records what it was called with and answers from a script. */
function gateway(...responses: Response[]) {
  const calls: Call[] = [];
  const queue = [...responses];

  const fetch = (async (url: string | URL | Request, init: RequestInit = {}) => {
    calls.push({
      url: String(url),
      init,
      body: JSON.parse(String(init.body ?? '{}')) as Record<string, unknown>,
    });
    // The last scripted response repeats, so a test that retries need not
    // spell out every attempt.
    return queue.length > 1 ? queue.shift()! : queue[0]!.clone();
  }) as unknown as typeof globalThis.fetch;

  return { fetch, calls };
}

const accepted = (messageId = 'msg_01') =>
  new Response(JSON.stringify({ messageId, status: Status.Pending }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });

const refusal = (status: number, code: string, message: string, headers: HeadersInit = {}) =>
  new Response(JSON.stringify({ code, message }), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  });

const hello = (over: Partial<Message> = {}): Message => ({
  providerId: '3f1c2b7a-0000-4000-8000-000000000001',
  from: 'app@example.com',
  to: ['user@example.net'],
  subject: 'Hello',
  text: 'hi there',
  ...over,
});

const client = (fetch: typeof globalThis.fetch, options = {}) =>
  new PanmailClient(BASE, 'test-key', { fetch, ...options });

describe('send', () => {
  test('returns the queued message id and status', async () => {
    const g = gateway(accepted());

    const result = await client(g.fetch).send(hello());

    expect(result.messageId).toBe('msg_01');
    expect(result.status).toBe(Status.Pending);
  });

  // The key goes in X-API-Key. Authorization carries a dashboard session, and
  // a key sent there is rejected as a malformed session, not as a bad key.
  test('authenticates with the api key header, not Authorization', async () => {
    const g = gateway(accepted());

    await client(g.fetch).send(hello());

    const headers = g.calls[0]!.init.headers as Record<string, string>;
    expect(headers['X-API-Key']).toBe('test-key');
    expect(headers['Authorization']).toBeUndefined();
    expect(headers['Content-Type']).toBe('application/json');
  });

  test('posts to the connect procedure', async () => {
    const g = gateway(accepted());

    await client(g.fetch).send(hello());

    expect(g.calls[0]!.url).toBe(`${BASE}/panmail.v1.EmailService/SendEmail`);
    expect(g.calls[0]!.init.method).toBe('POST');
  });

  test('carries the whole message', async () => {
    const g = gateway(accepted());

    await client(g.fetch).send(
      hello({
        cc: ['cc@example.net'],
        bcc: ['bcc@example.net'],
        html: '<p>hi there</p>',
        templateId: 'welcome',
        templateData: { name: 'Ada', count: 2 },
      }),
    );

    expect(g.calls[0]!.body).toMatchObject({
      providerId: '3f1c2b7a-0000-4000-8000-000000000001',
      from: 'app@example.com',
      to: ['user@example.net'],
      cc: ['cc@example.net'],
      bcc: ['bcc@example.net'],
      subject: 'Hello',
      bodyHtml: '<p>hi there</p>',
      bodyText: 'hi there',
      templateId: 'welcome',
      templateData: { name: 'Ada', count: 2 },
    });
  });

  // A bytes field is base64 in protobuf JSON, and the gateway decodes it as
  // such. Sending raw bytes instead would arrive as mojibake.
  test('base64-encodes attachment content and guesses the type', async () => {
    const g = gateway(accepted());

    await client(g.fetch).send(
      hello({ attachments: [{ filename: 'receipt.pdf', content: '%PDF-1.4' }] }),
    );

    const attachments = g.calls[0]!.body.attachments as Array<Record<string, string>>;
    expect(attachments).toHaveLength(1);
    expect(Buffer.from(attachments[0]!.content!, 'base64').toString()).toBe('%PDF-1.4');
    expect(attachments[0]!.contentType).toBe('application/pdf');
  });

  // A UTF-8 CSV guessed as latin-1 is mojibake in a spreadsheet. All four
  // clients send the same header for the same file.
  test('text attachments declare their charset', async () => {
    const g = gateway(accepted());

    await client(g.fetch).send(
      hello({ attachments: [{ filename: 'report.csv', content: 'a,b\n1,2' }] }),
    );

    const attachments = g.calls[0]!.body.attachments as Array<Record<string, string>>;
    expect(attachments[0]!.contentType).toBe('text/csv; charset=utf-8');
  });

  test('refuses bad messages without calling the gateway', async () => {
    const g = gateway(accepted());
    const c = client(g.fetch);

    const bad: Array<[string, Message]> = [
      ['no provider', { providerId: '', from: 'a@example.com', to: ['b@example.net'], text: 'x' }],
      ['no from', { providerId: 'p', from: '', to: ['b@example.net'], text: 'x' }],
      ['no recipients', { providerId: 'p', from: 'a@example.com', text: 'x' }],
      ['no body or template', { providerId: 'p', from: 'a@example.com', to: ['b@example.net'] }],
      [
        'attachment without a filename',
        {
          providerId: 'p',
          from: 'a@example.com',
          to: ['b@example.net'],
          text: 'x',
          attachments: [{ filename: '', content: 'x' }],
        },
      ],
    ];

    for (const [name, message] of bad) {
      await expect(c.send(message), name).rejects.toBeInstanceOf(InvalidMessageError);
    }
    expect(g.calls).toHaveLength(0);
  });
});

// Both capacity refusals are resource_exhausted. Retry-After is the only thing
// separating them, so this is the test that fails if the gateway stops sending
// it.
describe('classifying refusals', () => {
  test('a rate limit carries its delay', async () => {
    const g = gateway(
      refusal(429, 'resource_exhausted', 'over the send rate', { 'Retry-After': '7' }),
    );

    const error = (await client(g.fetch)
      .send(hello())
      .catch((e: unknown) => e)) as RateLimitedError;

    expect(error).toBeInstanceOf(RateLimitedError);
    expect(error.retryAfter).toBe(7);
  });

  test('the same code without a delay is a full queue', async () => {
    const g = gateway(refusal(429, 'resource_exhausted', 'queue too deep'));

    await expect(client(g.fetch).send(hello())).rejects.toBeInstanceOf(BacklogFullError);
  });

  test('an unreadable delay is still a rate limit', async () => {
    const g = gateway(
      refusal(429, 'resource_exhausted', 'over the send rate', {
        'Retry-After': 'Wed, 21 Oct 2026 07:28:00 GMT',
      }),
    );

    const error = (await client(g.fetch)
      .send(hello())
      .catch((e: unknown) => e)) as RateLimitedError;

    expect(error).toBeInstanceOf(RateLimitedError);
    expect(error.retryAfter).toBe(0);
  });

  test.each([
    ['unauthenticated', 401],
    ['permission_denied', 403],
  ])('%s is an auth error', async (code, status) => {
    const g = gateway(refusal(status, code, 'no'));

    await expect(client(g.fetch).send(hello())).rejects.toBeInstanceOf(AuthError);
  });

  test('anything else keeps its code', async () => {
    const g = gateway(refusal(400, 'invalid_argument', 'provider_id is not a uuid'));

    const error = (await client(g.fetch)
      .send(hello())
      .catch((e: unknown) => e)) as ApiError;

    expect(error).toBeInstanceOf(ApiError);
    expect(error.code).toBe('invalid_argument');
    expect(error.message).toContain('uuid');
  });

  // A proxy in front of the gateway answers with its own error page. The
  // status is still worth classifying.
  test('a body that is not an envelope falls back to the status', async () => {
    const g = gateway(new Response('<html>401 Unauthorized</html>', { status: 401 }));

    await expect(client(g.fetch).send(hello())).rejects.toBeInstanceOf(AuthError);
  });
});

describe('retries', () => {
  test('does not retry by default', async () => {
    const g = gateway(
      refusal(429, 'resource_exhausted', 'over the send rate', { 'Retry-After': '1' }),
    );

    await client(g.fetch).send(hello()).catch(() => undefined);

    expect(g.calls).toHaveLength(1);
  });

  test('waits out a rate limit when asked', async () => {
    const g = gateway(
      refusal(429, 'resource_exhausted', 'over the send rate', { 'Retry-After': '1' }),
      accepted('msg_after_wait'),
    );

    const result = await client(g.fetch, { rateLimitRetries: 1 }).send(hello());

    expect(result.messageId).toBe('msg_after_wait');
    expect(g.calls).toHaveLength(2);
  });

  // A full queue is not something waiting on a timer fixes, and retrying makes
  // the wait longer for everything already queued.
  test('does not retry a full queue', async () => {
    const g = gateway(refusal(429, 'resource_exhausted', 'queue too deep'));

    await client(g.fetch, { rateLimitRetries: 3 })
      .send(hello())
      .catch(() => undefined);

    expect(g.calls).toHaveLength(1);
  });

  // A transport error is the one outcome where the client cannot know whether
  // the gateway took the message, so it is never repeated.
  test('does not retry a transport error', async () => {
    let calls = 0;
    const fetch = (async () => {
      calls++;
      throw new Error('connection reset');
    }) as unknown as typeof globalThis.fetch;

    await expect(
      client(fetch, { rateLimitRetries: 3 }).send(hello()),
    ).rejects.toBeInstanceOf(TransportError);
    expect(calls).toBe(1);
  });
});

describe('constructor', () => {
  test.each([
    ['no base url', '', 'key'],
    ['no scheme', 'mail.example.com', 'key'],
    ['wrong scheme', 'ftp://mail.example.com', 'key'],
    ['no api key', BASE, ''],
  ])('rejects %s', (_name, baseUrl, apiKey) => {
    expect(() => new PanmailClient(baseUrl, apiKey)).toThrow(InvalidMessageError);
  });

  test('accepts a base url with a trailing slash', async () => {
    const g = gateway(accepted());

    await new PanmailClient(`${BASE}/`, 'test-key', { fetch: g.fetch }).send(hello());

    expect(g.calls[0]!.url).toBe(`${BASE}/panmail.v1.EmailService/SendEmail`);
  });

  test('a caller-supplied header cannot override the api key', async () => {
    const g = gateway(accepted());

    await client(g.fetch, {
      headers: { 'x-api-key': 'smuggled', 'X-Trace': 'abc' },
    }).send(hello());

    const headers = g.calls[0]!.init.headers as Record<string, string>;
    expect(headers['X-API-Key']).toBe('test-key');
    expect(headers['x-api-key']).toBeUndefined();
    expect(headers['X-Trace']).toBe('abc');
  });
});

describe('bounding the response', () => {
  /** Sends headers and a first chunk, then stalls until the signal aborts. */
  const stalls = () =>
    (async (_url: string | URL | Request, init: RequestInit = {}) => {
      const signal = init.signal as AbortSignal | undefined;
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new TextEncoder().encode('{"messageId":"m1"'));
          signal?.addEventListener('abort', () => controller.error(new Error('aborted')));
          // The rest of the body never arrives.
        },
      });
      return new Response(stream, { status: 200 });
    }) as unknown as typeof globalThis.fetch;

  test('the timeout covers the body, not just the headers', async () => {
    const started = Date.now();

    await expect(client(stalls(), { timeoutMs: 150 }).send(hello())).rejects.toBeInstanceOf(
      TransportError,
    );

    // Without the fix this never settles: fetch() resolves on the headers and
    // the body read is unbounded.
    expect(Date.now() - started).toBeLessThan(2_000);
  });

  test('refuses a response too large to be a send result', async () => {
    const oversized = (async () =>
      new Response('x'.repeat((1 << 20) + 1), { status: 200 })) as unknown as typeof globalThis.fetch;

    await expect(client(oversized).send(hello())).rejects.toThrow(/exceeded/);
  });
});

describe('redirects', () => {
  test('are refused rather than followed with the api key attached', async () => {
    // The real fetch, against real servers: a mock cannot show what undici
    // does with the Authorization-shaped problem of a custom auth header.
    const http = await import('node:http');
    let leaked: string | undefined;

    const elsewhere = http.createServer((req, res) => {
      leaked = req.headers['x-api-key'] as string | undefined;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end('{"messageId":"leaked"}');
    });
    await new Promise<void>((r) => elsewhere.listen(0, r));
    const elsewherePort = (elsewhere.address() as { port: number }).port;

    const redirecting = http.createServer((_req, res) => {
      res.writeHead(307, { Location: `http://127.0.0.1:${elsewherePort}/steal` });
      res.end();
    });
    await new Promise<void>((r) => redirecting.listen(0, r));
    const port = (redirecting.address() as { port: number }).port;

    try {
      const c = new PanmailClient(`http://127.0.0.1:${port}`, 'secret-key');
      await expect(c.send(hello())).rejects.toBeInstanceOf(TransportError);
      expect(leaked).toBeUndefined();
    } finally {
      elsewhere.close();
      redirecting.close();
    }
  });
});

// The mapping all four clients share, read from one file rather than repeated
// here. A change to one implementation that the others do not follow turns
// three languages red instead of sending the gateway four different
// Content-Type headers for the same attachment.
describe('the shared content-type fixture', () => {
  const fixture = JSON.parse(
    readFileSync(new URL('../../testdata/content-types.json', import.meta.url), 'utf8'),
  ) as { types: Record<string, string> };

  const extensions = Object.entries(fixture.types);
  test('is not empty', () => {
    expect(extensions.length).toBeGreaterThan(0);
  });

  for (const [extension, expected] of extensions) {
    test(`.${extension} is sent as ${expected}`, async () => {
      const g = gateway(accepted());

      await client(g.fetch).send(
        hello({ attachments: [{ filename: `attachment.${extension}`, content: 'x' }] }),
      );

      const attachments = g.calls[0]!.body.attachments as Array<{ contentType: string }>;
      expect(attachments[0]!.contentType).toBe(expected);
    });
  }
});

// A carriage return or newline ends a header line, so a value carrying one is
// a second header the caller never wrote. fetch refuses to send it, but only
// at send time and as a TransportError — the one failure this client reports
// as an unknown outcome, when in truth nothing was sent. The constructor
// refuses it instead.
describe('headers that would split the request', () => {
  const bad: Array<[string, string, string]> = [
    ['crlf in value', 'X-Trace', 'abc\r\nX-Injected: yes'],
    ['bare lf', 'X-Trace', 'abc\nX-Injected: yes'],
    ['nul in value', 'X-Trace', 'abc\u0000def'],
    ['crlf in name', 'X-Bad\r\nX-Injected', 'yes'],
    ['space in name', 'X Trace', 'yes'],
    ['empty name', '', 'yes'],
  ];

  for (const [label, name, value] of bad) {
    test(`${label} is refused`, () => {
      expect(() => new PanmailClient(BASE, 'k', { headers: { [name]: value } })).toThrow(
        InvalidMessageError,
      );
    });
  }

  test('a tab is legal in a header value and stays legal', () => {
    expect(() => new PanmailClient(BASE, 'k', { headers: { 'X-Trace': 'a\tb' } })).not.toThrow();
  });
});

// A url carrying a password travels into every error this client reports, and
// an error is a thing that gets logged. undici refuses such a url anyway, but
// by throwing a TypeError that quotes the whole thing — password included.
describe('credentials in the base url', () => {
  for (const baseUrl of ['https://user:hunter2@mail.example.com', 'https://user@mail.example.com']) {
    test(`${baseUrl} is refused`, () => {
      let thrown: unknown;
      try {
        new PanmailClient(baseUrl, 'k');
      } catch (error) {
        thrown = error;
      }
      expect(thrown).toBeInstanceOf(InvalidMessageError);
      expect(String((thrown as Error).message)).not.toContain('hunter2');
    });
  }
});

// The api key is a tenant-wide sending credential, and http puts it on the wire
// in the clear. Loopback is exempt because a gateway on localhost is how this is
// developed against, and there is no network to listen on.
describe('http away from loopback', () => {
  for (const baseUrl of ['http://mail.example.com', 'http://169.254.169.254/']) {
    test(`${baseUrl} is refused`, () => {
      expect(() => new PanmailClient(baseUrl, 'k')).toThrow(InvalidMessageError);
    });
  }

  for (const baseUrl of [
    'https://mail.example.com',
    'http://localhost:8080',
    'http://LOCALHOST:8080',
    'http://127.0.0.1:9000',
    'http://127.5.5.5:9000',
    'http://[::1]:9000',
  ]) {
    test(`${baseUrl} is allowed`, () => {
      expect(() => new PanmailClient(baseUrl, 'k')).not.toThrow();
    });
  }
});

// The gateway's enum is the source of truth and the fixture is generated from
// it by scripts/sync-status.py. Exposing a value the gateway does not have, or
// missing one it does, is how a caller ends up writing the string by hand.
describe('the shared event-type fixture', () => {
  const fixture = JSON.parse(
    readFileSync(new URL('../../testdata/event-types.json', import.meta.url), 'utf8'),
  ) as { constants: Record<string, string> };

  const camel = (name: string) =>
    name
      .toLowerCase()
      .replace(/_(.)/g, (_, c: string) => c.toUpperCase())
      .replace(/^./, (c) => c.toUpperCase());

  test('every gateway event type is exposed, with the gateway spelling', () => {
    for (const [name, wire] of Object.entries(fixture.constants)) {
      expect(Status[camel(name) as keyof typeof Status]).toBe(wire);
    }
  });

  test('nothing is exposed that the gateway does not have', () => {
    const expected = new Set(Object.values(fixture.constants));
    for (const value of Object.values(Status)) {
      expect(expected).toContain(value);
    }
    expect(Object.keys(Status).length).toBe(Object.keys(fixture.constants).length);
  });
});

// The documented fallback, which the content-type fixture cannot cover because
// it is not an extension mapping: anything unrecognised is sent as
// application/octet-stream, which every mail client offers as a download
// rather than trying to display.
test('an unknown extension falls back to octet-stream', async () => {
  const g = gateway(accepted());

  await client(g.fetch).send(
    hello({ attachments: [{ filename: 'ledger.qqq', content: 'x' }] }),
  );

  const attachments = g.calls[0]!.body.attachments as Array<{ contentType: string }>;
  expect(attachments[0]!.contentType).toBe('application/octet-stream');
});

// A 200 whose body is not the send result — a proxy answering for the gateway.
test('a 200 that is not json is a transport error', async () => {
  const g = gateway(new Response('<html>hello from a proxy</html>', { status: 200 }));

  await expect(client(g.fetch).send(hello())).rejects.toBeInstanceOf(TransportError);
});

// One client across concurrent sends. Node is single-threaded, so this is not
// a data race — it is the interleaving that matters: the headers object and
// the endpoint are shared across every await, and a send that mutated either
// would corrupt whichever send was suspended at the time.
test('one client serves many sends at once', async () => {
  const g = gateway(accepted());
  const c = client(g.fetch, { headers: { 'X-Trace': 'abc' } });

  const results = await Promise.all(Array.from({ length: 50 }, () => c.send(hello())));

  expect(results).toHaveLength(50);
  expect(results.every((r) => r.messageId === 'msg_01')).toBe(true);
  expect(g.calls).toHaveLength(50);
  // Every request carried both headers, unchanged.
  for (const call of g.calls) {
    const headers = call.init.headers as Record<string, string>;
    expect(headers['X-API-Key']).toBe('test-key');
    expect(headers['X-Trace']).toBe('abc');
  }
});
