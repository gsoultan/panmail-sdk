# Contributing

Two clients, one library. A change to how the client behaves is a change to
both — if you add a guard in Go, the same guard belongs in PHP, with the same
reasoning written down in each. The test suites are deliberately parallel for
the same reason: the test names read almost the same in both languages, so a
gap in one is visible next to the other.

## Running everything

| Language | Setup | Test |
| --- | --- | --- |
| Go | none — stdlib only | `go test -race ./...` |
| PHP | `cd php && composer install` | `composer test` |

Before opening a pull request:

```sh
./scripts/check.sh     # everything below, for whichever toolchains you have
```

It skips what is not installed and says so at the end, because nobody has every
toolchain to hand and a green run that quietly covered half of them is worse
than no run at all. The individual commands, if you want one of them on its own:

```sh
gofmt -l .            # must print nothing
go vet ./...
go test -race ./...
golangci-lint run ./...
(cd php  && composer test && composer analyse)
```

## What a change needs

**A test that fails before it and passes after.** For a bug fix, say what the
root cause was in one sentence in the commit message.

**The same behaviour in both languages,** or an explicit note in the pull
request about why one is different.

**Comments that say why, not what.** The existing code explains the reasoning
behind decisions that look arbitrary — why the key is not in `Authorization`,
why a rate limit is retried but a full queue is not, why redirects are refused.
Match that: a future reader who does not know the gateway should be able to
tell a deliberate choice from an accident.

## What the clients must agree on

`testdata/event-types.json` and `testdata/webhook-events.json` are the gateway's
`EmailEventType` and `WebhookTriggerEvent` enums, both generated from the protos
by `scripts/sync-status.py` and read by every suite. They are different
vocabularies — the first names the state a message is in and comes back from a
send, the second names why a webhook fired — and conflating them is how a
receiver ends up matching on a string the gateway never sends. Run that
after `sync-proto.sh`; the constants themselves stay hand-written, because
there is no code generator in this build and adding one to keep fifteen strings
in step would be the wrong trade.

`testdata/content-types.json` is the extension-to-content-type mapping, and
every client reads it in its own test suite. It exists because the clients had
already drifted on it once. If you add an extension, add it there — not to a
table per client — and check it is one Go's `mime` package knows natively, or the Go
client's answer starts depending on the host's mime database.

That is the pattern to reach for whenever the clients have to agree on a value: a
fixture all of them read beats a copy each and a promise.

## The protos

`proto/` is a verbatim copy of the gateway's. Do not edit it by hand — run
`scripts/sync-proto.sh /path/to/panmail` and commit what it produces.

`buf lint proto` checks the copy is still valid and self-consistent.
`scripts/check-proto-drift.sh` checks it still *matches* — it fetches the
gateway's protos over https and diffs them. It runs weekly, on any change to
`proto/` or the generators, and as part of `check.sh`.

**What it does not catch is behaviour.** A filter rule quarantining a message
changed what a send means with every proto byte-identical; nothing here would
have noticed.

The panmail repo's `internal/sdkcontract` runs in its ordinary CI test pass, as
of its #17 on 2026-09-04 — no build tag, the SDK in its `go.mod` like any other
test dependency. Four tests: a send through the real auth stack, every field the
SDK sends being decoded by the gateway, and a send refused both without the
`email:send` scope and with an unknown key. `TestTheGatewayDecodesEveryFieldTheSDKSends`
is the one that catches a field name drifting.

Two things follow from that. It guards the **published** SDK, not `main` — so a
change here is unguarded until it is tagged and the gateway's `go.mod` moves.
And it is **Go only**: PHP builds the same wire body by hand, and nothing
compares it to the gateway. That is the gap that is actually left.

## The wire contract

`docs/WIRE.md` is the specification the clients implement. A change to
what goes on the wire belongs there first.

## Releasing

A tag is the release. Go is served by the module proxy from the tag, and PHP by
Packagist from the root `composer.json`; neither needs a workflow or a
credential, because both read the tag directly. Move the `Unreleased` section
of `CHANGELOG.md` under the new version, date it, then tag:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

**A tag cannot be taken back.** The proxy keeps a Go module version for good
once it has served it, so a tag is a release the moment it is pushed. The
release workflow checks afterwards that `CHANGELOG.md` has a dated section for
the tag. It cannot stop a tag, but it makes one with no release notes loud
rather than silent.

A prerelease tag (`v0.2.0-rc.1`) is a prerelease for both: `go get` does not
pick it as latest, and Composer installs it only for a project whose
`minimum-stability` allows it.

Packagist needs the repository submitted once, at packagist.org, before
`composer require` resolves. After that it picks up new tags from GitHub.

## Coverage

Both gate a floor in CI:

| | how | floor | where it sits |
| --- | --- | --- | --- |
| Go | `go tool cover` | 95% | 98.4% |
| PHP | xdebug + `scripts/coverage-floor.py` | 95% | 99.1% |

They are floors, not targets. The last few percent of any of these clients are
error branches that need a broken socket to reach, and contorting the code to
reach them buys nothing — a marshal that cannot fail because its input was
validated a function earlier is not a gap.

What the floors are for is the other kind: before anyone measured, Go had every
`Unwrap` at zero and two public options never called, and PHP had the guard
against a `JsonException` escaping its own hierarchy untested. Neither was hard
to test, only easy to forget.

PHP coverage needs a driver: `composer coverage` with xdebug or pcov loaded.

## Static analysis

Go is linted by `golangci-lint` (config in `.golangci.yml`) and PHP by PHPStan,
at `max` for `src` and a lower level for `tests` — a test reaching into a mixed
from a decoded body is doing what a test should; shipped code is where mixed
has to be pinned down.

Both are configured to say why, not just what. If a rule is excluded there is a
comment giving the reason, because the alternative is a future contributor
deleting an exclusion nobody can defend, or keeping one that stopped applying.
