# Contributing

Four clients, one library. A change to how the client behaves is a change to
all four — if you add a guard in Go, the same guard belongs in PHP, Java and
Node, with the same reasoning written down in each. The test suites are
deliberately parallel for the same reason: the test names read almost the same
in every language, so a gap in one is visible next to the other three.

## Running everything

| Language | Setup | Test |
| --- | --- | --- |
| Go | none — stdlib only | `go test -race ./...` |
| PHP | `cd php && composer install` | `composer test` |
| Java | JDK 17+ | `cd java && mvn --batch-mode test` |
| Node | `cd node && bun install` | `bun test src` |

Before opening a pull request:

```sh
gofmt -l .            # must print nothing
go vet ./...
go test -race ./...
(cd php  && composer test)
(cd java && mvn --batch-mode test)
(cd node && bun x tsc -p tsconfig.json --noEmit && bun test src)
(cd node && bun run build && bun run smoke)
```

The last one is not the same as the one above it. The suite runs under bun;
`smoke` runs the built package on whatever `node` is on your PATH, because
`engines` promises Node 18 and bun is not Node. CI runs it on the floor and on
the current release.

## What a change needs

**A test that fails before it and passes after.** For a bug fix, say what the
root cause was in one sentence in the commit message.

**The same behaviour in all four languages,** or an explicit note in the pull
request about why one is different.

**Comments that say why, not what.** The existing code explains the reasoning
behind decisions that look arbitrary — why the key is not in `Authorization`,
why a rate limit is retried but a full queue is not, why redirects are refused.
Match that: a future reader who does not know the gateway should be able to
tell a deliberate choice from an accident.

## The protos

`proto/` is a verbatim copy of the gateway's, so the two cannot drift. Do not
edit it by hand — run `scripts/sync-proto.sh /path/to/panmail` and commit what
it produces. `buf lint proto` checks the copy is still valid; drift from the
real gateway is caught by the contract test in the panmail repo.

## The wire contract

`docs/WIRE.md` is the specification the four clients implement. A change to
what goes on the wire belongs there first.

## Releasing

The four packages share a version number. Bump it in `node/package.json` and
`java/pom.xml`, move the `Unreleased` section of `CHANGELOG.md` under the new
version, then tag:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

The tag publishes to npm and Maven Central. Go is served by the module proxy
from the tag, and PHP by Packagist from the root `composer.json`. The release
workflow refuses a tag that disagrees with either manifest, because a version
number cannot be reused once a registry has it.

Publishing needs these repository secrets: `NPM_TOKEN`,
`MAVEN_CENTRAL_USERNAME`, `MAVEN_CENTRAL_PASSWORD`, `MAVEN_GPG_PRIVATE_KEY`,
`MAVEN_GPG_PASSPHRASE`.
