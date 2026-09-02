# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The three language packages share a version number and are released together.

## [Unreleased]

The first release, in Go, PHP and Node. Nothing has been tagged yet, so
everything here is still unreleased.

A Java client existed during development and was removed before any release.
Publishing it meant Maven Central, which meant verifying the
`io.github.gsoultan` namespace with Sonatype — approval measured in days, for
one language out of four, while Go and PHP need no credentials at all and npm
needs one token. The cost was in the release path rather than in the code, and
it is in the git history if it is ever worth paying.

### Security

- **A quadratic-backtracking regex trimmed the base URL's trailing slashes** in
  the Node client (`/\/+$/`). Anchored at
  the end but not the start, so on a URL that is mostly slashes the engine
  retries from every slash: 80,000 of them took 2.3 seconds in Node, against
  nothing for a loop. Found by CodeQL on its first run, and rated high. The base
  URL comes from whoever embeds the library rather than from a request, so this
  was unlikely rather than reachable — but Go and PHP were already doing it
  linearly with `TrimRight` and `rtrim`, so the other two now match.
- **`http://` is refused unless the host is loopback.** All four accepted
  plaintext to any host, which sends the tenant-wide API key in the clear.
  `localhost`, `127.0.0.0/8` and `::1` stay allowed so local development is
  unaffected; anything else must be `https`. A side effect worth having: the
  cloud metadata endpoint `http://169.254.169.254/` is now refused too.
- **A base URL carrying credentials is now refused by all three.** The URL
  travels into every error the client reports, and errors get logged. Node
  leaked a password that way: undici refuses such a URL, but by throwing a
  `TypeError` quoting the whole URL, which the client put in a
  `TransportError` message. Go redacted it and PHP accepted it silently —
  three different answers to one question. Userinfo is deprecated in RFC 3986
  for this exact reason.
- **The PHP client was vulnerable to CRLF header injection.** `CurlTransport`
  built each header line by joining the name and value with a colon, so a
  carriage return or newline in a caller-supplied header value ended the line
  early and everything after it became a header of its own. An application
  passing user-controlled data into a tracing or correlation header could have
  arbitrary headers added to its request. Reproduced against a real socket:
  `['X-Trace' => "abc\r\nX-Injected: yes"]` arrived at the server as two
  headers.

  Go and Node were not injectable — `net/http` and undici both refuse the
  value. All three now validate header names and values when the client is
  constructed, so the failure is the same shape, with the same type, at the
  same point, in every language.

- **The API key is no longer forwarded across an HTTP redirect.** The key
  travels in `X-API-Key`, and a custom header is not on any HTTP client's list
  of credentials to drop when a redirect crosses to another host — that list
  knows about `Authorization` and `Cookie`. Go's `http.Client` and Node's
  `fetch` both followed redirects by default and both forwarded the key to the
  new host. All three clients now refuse a redirect, including one built
  around a caller-supplied HTTP client.

### Fixed

- **PHP: a refusal whose `code` or `message` was structured rather than a
  string raised "Array to string conversion" twice and left the caller holding
  `panmail: Array: Array`.** `classify()` cast a `mixed` straight to string —
  the same hazard already guarded on the send-result path, missed on the
  refusal path. Found by PHPStan.
- **PHP: the `@param list<string>` on `Message`'s recipients was not true.**
  The signature takes any `array`, and `array_filter` — the ordinary way to
  drop one recipient — returns a non-list. `array_values` was already
  compensating; the annotation claimed a guarantee nothing enforced, which is
  what made the call look redundant.

- **Node: the timeout now covers the response body, not just the headers.**
  `fetch()` settles as soon as the response line arrives, and the abort timer
  was cleared at that point, so a gateway that sent headers and then stalled
  held the caller open with no bound at all.
- **Node and PHP: a response is bounded at 1 MiB,** as Go already was. A
  proxy error page where a send result was expected could otherwise be buffered
  whole.
- **PHP: a malformed response no longer escapes the documented exception
  hierarchy.** `JSON_THROW_ON_ERROR` raised a bare `JsonException`, which is
  not a `PanmailException`, so it slipped past every `catch` a caller wrote
  from the list in the docblock. A non-JSON or non-object body is now a
  `TransportException`, and an unencodable message an `InvalidMessageException`.
- **PHP: `curl_close()` removed.** It has had no effect since PHP 8.0 and is
  deprecated in 8.5, where it made the suite emit a deprecation.
- **Go: the `X-API-Key` header is now stripped from `WithHeader` case
  insensitively,** matching the other two clients.

### Added

- `docs/WIRE.md` has a section on delivery events, and says plainly that
  webhooks are not specified and not covered by the SDKs. `SECURITY.md` gained
  a "what the SDKs do not cover" section saying the same: with no signature
  scheme, a webhook endpoint is an unauthenticated write path, and an event
  should be confirmed against the `messageId` you stored at send time. Tracked
  in #12.
- **All fifteen of the gateway's event types are now exposed as `Status`
  constants**, in every client. Four were: `Pending`, `Sent`, `Delivered`,
  `Dropped`. `docs/WIRE.md` told readers they would see
  `EMAIL_EVENT_TYPE_BOUNCED` on the event stream, and no client had a constant
  for it — so anyone handling a bounce wrote the string by hand. Missing along
  with it: opened, clicked, the hard and soft bounce split, spam reports,
  unsubscribes, rejections, deferrals and complaints.
  `testdata/event-types.json` is generated from the proto by
  `scripts/sync-status.py` and asserted by all three suites, so the next value
  the gateway adds fails three test runs rather than none.
- **The thread-safety the docs promise is now tested.** Go's `Client` says
  "Safe for concurrent use" and nothing exercised a client from more than one
  goroutine. It now sends from eight under `-race`, which makes the claim
  enforceable rather than aspirational — verified by adding a write to the
  shared headers map, which the race detector catches immediately. Node reuses
  one client across fifty concurrent sends: not a data race there, but the
  headers object and endpoint are shared across every await.

  PHP has no equivalent and no such claim, so it has no such test.
- **Coverage is measured, and what it found is now tested.** Go was at 88.5%:
  `Error()` and `Unwrap()` on every refusal type at zero, and both
  `WithHTTPClient` and `WithTimeout` never exercised at all. `Unwrap` mattered
  most — Go is the only one of the four where the Connect code is not on the
  refusal itself, so unwrapping to `*APIError` is the documented way to reach
  it, and nothing held that. Now 98.4%, with the remainder three genuinely
  unreachable defensive branches.

  The same sweep found the documented `application/octet-stream` fallback
  untested in all three, and a non-JSON 200 untested in Node.

  PHP was 94.9%, including the guard that stops a `JsonException` escaping its
  own exception hierarchy, which had no test at all; now 99.1%.

  CI gates Go and PHP at 95%. Node reports coverage without a threshold,
  because bun enforces per file and the only floor that would pass is low
  enough to let line coverage fall seventeen points in silence.
- `scripts/check.sh` runs everything CI runs, for whichever of the three
  toolchains are installed, and lists what it skipped rather than letting a
  partial run look complete.
- `testdata/content-types.json`: the extension-to-content-type mapping all
  three clients guess from, written down once and read by all three suites. The
  four had already drifted on it — Go returned `text/csv; charset=utf-8` where
  the others returned `text/csv` — and a per-language test for one extension
  fixed that case without preventing the next. Changing the fixture now fails
  Go, PHP and Node together.
- CodeQL over Go and TypeScript, on every push and weekly, with the
  `security-and-quality` queries. PHP is not analysed by CodeQL, which is why
  PHPStan runs at `max` on the shipped PHP.
- Static analysis in CI: `golangci-lint` for Go and PHPStan for PHP, the latter
  at `max` on shipped code. Both earned their place on the first run.
- Node: `noUnusedLocals`, `noUnusedParameters`, `noImplicitOverride`,
  `noImplicitReturns` and `noFallthroughCasesInSwitch`. `noImplicitOverride`
  found `TransportError` shadowing the `cause` that `Error` has carried since
  ES2022 — it now says `override`, so a reader can tell it was deliberate.

- `LICENSE` — MIT, which the three package manifests already declared.
- `SECURITY.md`, `CONTRIBUTING.md`, this changelog, and `.editorconfig`.
- PHP: `CurlTransportTest` drives the default transport against a real server.
  It was the code path used in production and the only one with no coverage.
- A `composer.json` at the repository root, which is the only one Packagist
  reads. `php/composer.json` stays as the development definition, and
  `PackageDefinitionTest` fails if the two drift apart.
- Release automation: a `v*` tag publishes to npm with provenance, after
  checking the tag agrees with `node/package.json`. Go and PHP are served from
  the tag itself, by the module proxy and Packagist. The same workflow runs as
  a dry run from the Actions tab, so the publish path is exercised before a tag
  makes it irreversible.
- npm package metadata: `repository`, `homepage`, `bugs`, `author`,
  `sideEffects`, and a `prepublishOnly` build so a publish cannot ship a stale
  `dist/`.
- `bun.lock` is committed. CI installs with `--frozen-lockfile`, which without
  a lockfile silently resolved fresh versions on every run.
- CI: least-privilege `permissions`, cancellation of superseded runs, Go tested
  at the 1.21 floor go.mod declares, PHP at 8.4 and 8.5, and a build of the
  `dist/` that npm publishes.
- `.github/dependabot.yml`, watching the GitHub Actions most closely — they are
  the dependency with access to a release.
- Node: `consumer/` compiles against the published `dist/*.d.ts` with
  `skipLibCheck` off, which is the only check that sees the API as a consumer
  does — the ordinary typecheck reads `src/`. It caught that the declarations
  reference `fetch`, `Headers` and `ReadableStream`, ambient types the package
  does not ship; `@types/node` is now declared as an optional peer dependency
  rather than left to be discovered on install.
- Node: `smoke.mjs` runs the built package on a real Node across the range
  `engines` declares. The suite runs under bun, which is not Node — it has its
  own fetch, streams and globals — so nothing was checking that the package
  works on the runtime it promises. The script exercises the version-sensitive
  paths deliberately: global `fetch`, `ReadableStream.getReader`, `TextDecoder`
  and undici's handling of `redirect: 'error'`. Verified passing on 18.20.8.
