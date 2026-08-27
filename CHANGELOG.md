# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The four language packages share a version number and are released together.

## [Unreleased]

### Security

- **The API key is no longer forwarded across an HTTP redirect.** The key
  travels in `X-API-Key`, and a custom header is not on any HTTP client's list
  of credentials to drop when a redirect crosses to another host — that list
  knows about `Authorization` and `Cookie`. Go's `http.Client` and Node's
  `fetch` both followed redirects by default and both forwarded the key to the
  new host. All four clients now refuse a redirect. Java additionally refuses a
  caller-supplied `HttpClient` that is configured to follow one.

### Fixed

- **Node: the timeout now covers the response body, not just the headers.**
  `fetch()` settles as soon as the response line arrives, and the abort timer
  was cleared at that point, so a gateway that sent headers and then stalled
  held the caller open with no bound at all.
- **Node, Java and PHP: a response is bounded at 1 MiB,** as Go already was. A
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
  insensitively,** matching the other three clients.
- **Java: `timeout()` rejects a null, zero or negative duration** rather than
  failing later inside `HttpClient`.

### Added

- `LICENSE` — MIT, which the three package manifests already declared.
- `SECURITY.md`, `CONTRIBUTING.md`, this changelog, and `.editorconfig`.
- PHP: `CurlTransportTest` drives the default transport against a real server.
  It was the code path used in production and the only one with no coverage.
- A `composer.json` at the repository root, which is the only one Packagist
  reads. `php/composer.json` stays as the development definition, and
  `PackageDefinitionTest` fails if the two drift apart.
- Release automation: a `v*` tag publishes to npm (with provenance) and Maven
  Central, after checking the tag agrees with both manifests. Go and PHP are
  served from the tag itself.
- The Java POM now carries what Maven Central requires — `developers`, `scm`,
  and a `release` profile that attaches sources, javadoc and signatures.
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
