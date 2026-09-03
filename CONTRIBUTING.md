# Contributing

Three clients, one library. A change to how the client behaves is a change to
all three — if you add a guard in Go, the same guard belongs in PHP and Node,
with the same reasoning written down in each. The test suites are deliberately
parallel for the same reason: the test names read almost the same in every
language, so a gap in one is visible next to the other two.

## Running everything

| Language | Setup | Test |
| --- | --- | --- |
| Go | none — stdlib only | `go test -race ./...` |
| PHP | `cd php && composer install` | `composer test` |
| Node | `cd node && bun install` | `bun test src` |

Before opening a pull request:

```sh
./scripts/check.sh     # everything below, for whichever toolchains you have
```

It skips what is not installed and says so at the end, because nobody has all
four to hand and a green run that quietly covered two of them is worse than no
run at all. The individual commands, if you want one of them on its own:

```sh
gofmt -l .            # must print nothing
go vet ./...
go test -race ./...
golangci-lint run ./...
(cd php  && composer test && composer analyse)
(cd node && bun x tsc -p tsconfig.json --noEmit && bun test src)
(cd node && bun run build && bun run smoke && bun run check:types)
```

The last one is not the same as the one above it. The suite runs under bun;
`smoke` runs the built package on whatever `node` is on your PATH, because
`engines` promises Node 18 and bun is not Node. CI runs it on the floor and on
the current release.

## What a change needs

**A test that fails before it and passes after.** For a bug fix, say what the
root cause was in one sentence in the commit message.

**The same behaviour in all three languages,** or an explicit note in the pull
request about why one is different.

**Comments that say why, not what.** The existing code explains the reasoning
behind decisions that look arbitrary — why the key is not in `Authorization`,
why a rate limit is retried but a full queue is not, why redirects are refused.
Match that: a future reader who does not know the gateway should be able to
tell a deliberate choice from an accident.

## What the four must agree on

`testdata/event-types.json` is the gateway's `EmailEventType` enum, generated
from the proto by `scripts/sync-status.py` and read by every suite. Run that
after `sync-proto.sh`; the constants themselves stay hand-written, because
there is no code generator in this build and adding one to keep fifteen strings
in step would be the wrong trade.

`testdata/content-types.json` is the extension-to-content-type mapping, and
every client reads it in its own test suite. It exists because the four had
already drifted on it once. If you add an extension, add it there — not to four
tables — and check it is one Go's `mime` package knows natively, or the Go
client's answer starts depending on the host's mime database.

That is the pattern to reach for whenever the four have to agree on a value: a
fixture all of them read beats four copies and a promise.

## The protos

`proto/` is a verbatim copy of the gateway's. Do not edit it by hand — run
`scripts/sync-proto.sh /path/to/panmail` and commit what it produces.

`buf lint proto` checks the copy is still valid and self-consistent. It does
**not** check the copy still matches the gateway's, and nothing in this
repository does. The panmail repo has a contract test —
`internal/sdkcontract` — that sends through the real auth stack with this
client and would catch a field name drifting, but it sits behind a
`//go:build sdkcontract` tag and needs a `go.work` pointing at a local
checkout, so it runs when somebody runs it. It is also Go-only.

Publishing the SDK is what fixes that: the gateway's own comment says to drop
the build tag and depend on the module normally once it is tagged. Until then,
re-run `sync-proto.sh` when the gateway's protos change and do not rely on
anything to tell you that you should have.

## The wire contract

`docs/WIRE.md` is the specification the three clients implement. A change to
what goes on the wire belongs there first.

## Releasing

The three packages share a version number, and `node/package.json` is the only
manifest that carries one — Go takes it from the tag and PHP from Packagist.
Bump it there, move the `Unreleased` section of `CHANGELOG.md` under the new
version, then tag:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

The tag publishes to npm. Go is served by the module proxy from the tag, and
PHP by Packagist from the root `composer.json`. The release workflow refuses a
tag that disagrees with the manifest, because a version number cannot be reused
once a registry has it.

**A tag is a release for Go and PHP the moment it is pushed**, with no workflow
and no credentials involved — the proxy and Packagist read the tag directly. So
do not tag until `NPM_TOKEN` is in place: a tag without it ships two of the
three languages and fails the third, which is precisely the version skew a
shared version number exists to prevent. A Go module version cannot be
withdrawn once the proxy has served it.

A prerelease version (`0.1.0-rc.1`) goes to npm's `next` dist-tag, and a stable
one to `latest`, decided from the manifest rather than from a flag someone has
to remember. npm refuses a prerelease with no `--tag` at all, and answering
that with `latest` would hand a release candidate to everyone running
`npm install`.

### Publishing to npm

There is no publishing secret. npm authenticates the release workflow over
OIDC, against a *trusted publisher* configured on the package: this
repository, and the workflow **filename** `release.yml`. Nothing long-lived
exists to leak or rotate.

That is not a stylistic choice. An automation token cannot publish to an
account that requires 2FA for writes, and npm no longer issues granular tokens
with the bypass that used to make CI work — so a token-based release is not
available, whatever you would prefer.

**Bootstrapping it takes one manual publish.** A trusted publisher is
configured in a package's settings, and a package nobody has ever published
has no settings, so the first version has to go up from a laptop where npm can
prompt for a one-time password:

```sh
cd node
npm login                  # prompts for 2FA
bun run build
npm publish --access public --tag next    # --tag next for a prerelease
```

Then on npmjs.com → the package → Settings → Trusted Publisher:

| Field | Value |
| --- | --- |
| Organization or user | `gsoultan` |
| Repository | `panmail-sdk` |
| Workflow filename | `release.yml` — the filename, not a path |
| Allowed actions | `npm publish` |

After that every release publishes from the tag with no human in it. Delete the
`NPM_TOKEN` secret if one is still set; it is unused and an unused credential
is only a liability.

The workflow pins node 24 rather than 22 because of npm, not node: trusted
publishing needs npm >= 11.5.1 and node 22 still ships 10.9.8. The package's
own floor is node 18, and that is tested by the `node-runtime` job in CI.

## Coverage

Two of the three gate a floor in CI:

| | how | floor | where it sits |
| --- | --- | --- | --- |
| Go | `go tool cover` | 95% | 98.4% |
| PHP | xdebug + `scripts/coverage-floor.py` | 95% | 99.1% |
| Node | reported, not gated | — | 100% lines |

They are floors, not targets. The last few percent of any of these clients are
error branches that need a broken socket to reach, and contorting the code to
reach them buys nothing — a marshal that cannot fail because its input was
validated a function earlier is not a gap.

What the floors are for is the other kind: before anyone measured, Go had every
`Unwrap` at zero and two public options never called, and PHP had the guard
against a `JsonException` escaping its own hierarchy untested. Neither was hard
to test, only easy to forget.

Node reports coverage and gates on nothing, deliberately — bun enforces per
file, and the only floor that would pass is low enough to be meaningless. The
reason is written in `node/bunfig.toml` so nobody replaces it with a number
that does not hold.

PHP coverage needs a driver: `composer coverage` with xdebug or pcov loaded.

## Static analysis

Go is linted by `golangci-lint` (config in `.golangci.yml`) and PHP by PHPStan,
at `max` for `src` and a lower level for `tests` — a test reaching into a mixed
from a decoded body is doing what a test should; shipped code is where mixed
has to be pinned down.

Both are configured to say why, not just what. If a rule is excluded there is a
comment giving the reason, because the alternative is a future contributor
deleting an exclusion nobody can defend, or keeping one that stopped applying.
