# Graph Report - panmail-sdk  (2026-10-10)

## Corpus Check
- 72 files · ~51,428 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 14 file(s) not represented in the graph (top: .proto 7, (none) 4, .neon 2)

## Summary
- 617 nodes · 1302 edges · 39 communities (23 shown, 16 thin omitted)
- Extraction: 92% EXTRACTED · 8% INFERRED · 0% AMBIGUOUS · INFERRED: 101 edges (avg confidence: 0.89)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `788e40ab`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- testing.T
- ApiException
- errors.go
- WebhookTest
- webhook_test.go
- ClientTest
- php/composer.json
- composer.json
- Message
- Client
- panmail-sdk
- webhook.go
- check.sh
- check-proto-drift.sh
- Result
- sync-status.py
- CurlTransport
- TransportException
- sync-proto.sh
- bounds_test.go
- github.com/gsoultan/panmail-sdk
- Contributing
- ProvidersTest
- provider_test.go
- time.Duration
- [0.1.0-rc.2] — 2026-09-26
- PHPUnit\Framework\TestCase
- InvalidMessageException
- attachment.go
- php/README.md
- WebhookEvent
- Wire contract — what the gateway really does
- Added
- panmail-sdk — core
- Provider listing — decisions and the gaps behind them
- Result
- testing-and-verification.md
- working-in-this-checkout.md

## God Nodes (most connected - your core abstractions)
1. `client()` - 55 edges
2. `ClientTest` - 52 edges
3. `hello()` - 35 edges
4. `ProvidersTest` - 30 edges
5. `Client` - 28 edges
6. `respond()` - 26 edges
7. `FakeTransport` - 25 edges
8. `WebhookTest` - 22 edges
9. `accepts()` - 21 edges
10. `lists()` - 20 edges

## Surprising Connections (you probably didn't know these)
- `Changed` --references--> `RateLimitedException`  [INFERRED]
  CHANGELOG.md → php/src/Exception/RateLimitedException.php
- `A refusal is not a failure` --references--> `SuppressedRecipientException`  [INFERRED]
  docs/WIRE.md → php/src/Exception/SuppressedRecipientException.php
- `Security` --references--> `CurlTransport`  [INFERRED]
  CHANGELOG.md → php/src/Transport/CurlTransport.php
- `Proto` --references--> `Attachment`  [INFERRED]
  proto/README.md → php/src/Attachment.php
- `Added` --references--> `Client`  [INFERRED]
  CHANGELOG.md → php/src/Client.php

## Import Cycles
- None detected.

## Communities (39 total, 16 thin omitted)

### Community 0 - "testing.T"
Cohesion: 0.10
Nodes (65): TestALongProxyPageIsCutToALogLine(), TestSendDoesNotDrainABodyThatNeverEnds(), New(), accepts(), client(), contentTypeFixture(), hello(), refuse() (+57 more)

### Community 1 - "ApiException"
Cohesion: 0.18
Nodes (7): Added, ApiException, AuthException, BacklogFullException, RateLimitedException, SuppressedRecipientException, The two capacity refusals

### Community 2 - "errors.go"
Cohesion: 0.16
Nodes (9): classify(), classifySend(), effectiveCode(), retryAfter(), APIError, AuthError, BacklogFullError, call (+1 more)

### Community 3 - "WebhookTest"
Cohesion: 0.09
Nodes (5): Added, WebhookException, Webhook, WebhookEvent, WebhookTest

### Community 4 - "webhook_test.go"
Cohesion: 0.28
Nodes (18): signLikeTheGateway(), TestANonPositiveToleranceFallsBackToTheDefault(), TestTriggerEventConstantsMatchTheSharedFixture(), TestVerifyWebhookAcceptsWhatTheGatewaySent(), TestVerifyWebhookFallsBackToTheEventHeader(), TestVerifyWebhookNeedsASecret(), TestVerifyWebhookRefusesABodyThatWasReformatted(), TestVerifyWebhookRefusesAnUnsignedDelivery() (+10 more)

### Community 6 - "php/composer.json"
Cohesion: 0.08
Nodes (23): autoload, autoload-dev, psr-4, psr-4, config, sort-packages, description, license (+15 more)

### Community 7 - "composer.json"
Cohesion: 0.09
Nodes (21): archive, comment, exclude, autoload, psr-4, config, sort-packages, description (+13 more)

### Community 10 - "panmail-sdk"
Cohesion: 0.06
Nodes (29): Bounded cost, Non-functional, Out of scope, panmail-sdk — Product Requirements, Problem, Requirements, Safety of the key, Sending (+21 more)

### Community 11 - "webhook.go"
Cohesion: 0.15
Nodes (6): Message, sendRequest, WebhookError, WebhookOption, signWebhook(), WithWebhookTolerance()

### Community 12 - "check.sh"
Cohesion: 0.83
Nodes (3): have(), run(), check.sh script

### Community 13 - "check-proto-drift.sh"
Cohesion: 0.50
Nodes (3): FILES, RAW, check-proto-drift.sh script

### Community 15 - "sync-status.py"
Cohesion: 0.18
Nodes (3): main(), main(), main()

### Community 16 - "CurlTransport"
Cohesion: 0.22
Nodes (3): CurlTransport, CurlTransportTest, Review 2026-10-10 — response bounds and PHP parity

### Community 17 - "TransportException"
Cohesion: 0.18
Nodes (6): [0.1.0-rc.1] — 2026-09-02, Fixed, Fixed, Security, PanmailException, TransportException

### Community 19 - "bounds_test.go"
Cohesion: 0.15
Nodes (4): isLoopback(), validateBaseURL(), TriggerEvent, TestADottedEventNameIsNotTheVocabulary()

### Community 22 - "Contributing"
Cohesion: 0.04
Nodes (39): Working on panmail-sdk, Contributing, Coverage, Releasing, Running everything, Static analysis, The protos, The wire contract (+31 more)

### Community 23 - "ProvidersTest"
Cohesion: 0.06
Nodes (7): Provider, Response, Transport, FakeTransport, {closure#1}(), ProvidersTest, send()

### Community 24 - "provider_test.go"
Cohesion: 0.16
Nodes (28): enumName, listProvidersRequest, listProvidersResponse, Provider, ProviderFilter, ProviderType, Client, ids() (+20 more)

### Community 25 - "time.Duration"
Cohesion: 0.22
Nodes (4): decodeError(), Client, sleep(), RateLimitedError

### Community 26 - "[0.1.0-rc.2] — 2026-09-26"
Cohesion: 0.17
Nodes (11): [0.1.0-rc.2] — 2026-09-26, [0.1.0-rc.3] — 2026-09-28, Added, Changed, Changed, Changed, Changelog, Fixed (+3 more)

### Community 32 - "Wire contract — what the gateway really does"
Cohesion: 0.40
Nodes (4): Behaviour that is not in the protos, Procedures the clients call, Where the truth lives in the gateway, Wire contract — what the gateway really does

### Community 34 - "panmail-sdk — core"
Cohesion: 0.50
Nodes (3): panmail-sdk — core, Read next, Standing rules

### Community 35 - "Provider listing — decisions and the gaps behind them"
Cohesion: 0.50
Nodes (3): Decisions, and why, Gateway gaps found (issues drafted 2026-09-28, not filed: auto mode blocked `gh issue create` on the gateway repo), Provider listing — decisions and the gaps behind them

## Knowledge Gaps
- **4 isolated node(s):** `github.com/gsoultan/panmail-sdk`, `Client`, `FILES`, `RAW`
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 224 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **16 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `ClientTest` connect `ClientTest` to `ApiException`, `Message`, `Client`, `CurlTransport`, `ProvidersTest`, `PHPUnit\Framework\TestCase`?**
  _High betweenness centrality (0.062) - this node is a cross-community bridge._
- **Are the 21 inferred relationships involving `client()` (e.g. with `TestALongProxyPageIsCutToALogLine()` and `TestSendDoesNotDrainABodyThatNeverEnds()`) actually correct?**
  _`client()` has 21 INFERRED edges - model-reasoned connections that need verification._
- **What connects `github.com/gsoultan/panmail-sdk`, `Client`, `FILES` to the rest of the system?**
  _4 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `testing.T` be split into smaller, more focused modules?**
  _Cohesion score 0.09557109557109557 - nodes in this community are weakly interconnected._
- **Why does `Client` connect `Client` to `Added`, `InvalidMessageException`, `ApiException`, `ProvidersTest`?**
  _High betweenness centrality (0.038) - this node is a cross-community bridge._
- **Are the 2 inferred relationships involving `hello()` (e.g. with `TestALongProxyPageIsCutToALogLine()` and `TestSendDoesNotDrainABodyThatNeverEnds()`) actually correct?**
  _`hello()` has 2 INFERRED edges - model-reasoned connections that need verification._
- **Should `WebhookTest` be split into smaller, more focused modules?**
  _Cohesion score 0.08901515151515152 - nodes in this community are weakly interconnected._