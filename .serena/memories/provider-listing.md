# Provider listing — decisions and the gaps behind them

`ListProviders` (Go) / `listProviders` (PHP) reached `main` on 2026-09-28 as
`c82b8b7`, squash-merged from gsoultan/panmail-sdk#15, and shipped the same day
in `v0.1.0-rc.3`.

## Decisions, and why

- **One call returns the whole list.** Providers are configuration, not a feed.
  Pages of 50, followed to the end.
- **Bounded.** More than 200 pages, or a page token handed out twice, is an error,
  never a partial list: a list cut short that looks complete is how a caller
  concludes a provider does not exist.
- **Deduplicated by id.** The gateway pages by offset, newest first, so a provider
  created mid-read arrives on two pages. One deleted mid-read can push another
  off both — not recoverable client-side.
- **Four fields: id, name, type, allowedDomains.** The gateway also returns each
  provider's configuration (credentials cleared). A sender needs none of it, and
  not decoding it leaves a lapse in that clearing nowhere to land. Adding a field
  later is additive; removing one is breaking.
- **The type filter is re-checked** because the gateway ignores unknown enum
  names (see `mem:wire-contract`). A misspelt filter returns none, not all.
- **No retries, no cache.** Listing is safe to repeat; `rateLimitRetries` stays
  send-only because the gateway rate-limits sending, not reading.
- **Deliberate Go/PHP differences:** a structured `type` is an error in Go and
  UNSPECIFIED in PHP (each client's existing handling of malformed fields);
  invalid UTF-8 in the name is an InvalidMessageException in PHP, replaced bytes
  in Go.

## Gateway gaps found (issues drafted 2026-09-28, not filed: auto mode blocked `gh issue create` on the gateway repo)

1. Seven offset-paged lists order by `created_at` alone. A suppression import
   stamps up to 10,000 rows with one timestamp, so paging it can repeat or skip
   rows with no concurrent writes. Fix: `id` tiebreaker now, the event store's
   keyset token (`events.go:471-485`) later.
2. `page_size` has no ceiling in any of those seven stores.
3. `LIKE` patterns are unescaped: provider name, event recipient and subject.
4. `db.DecodeOffset` restarts silently on an unreadable token; a negative one is a
   500 (`internal`) on PostgreSQL.
5. `internal/sdkcontract` has no listing test. No longer blocked: `v0.1.0-rc.3`
   carries ListProviders, so the gateway's `go.mod` can move to it.
