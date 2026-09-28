# Graph Report - panmail-sdk  (2026-09-28)

## Corpus Check
- 64 files · ~47,949 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 535 nodes · 1087 edges · 31 communities (17 shown, 12 thin omitted)
- Extraction: 95% EXTRACTED · 5% INFERRED · 0% AMBIGUOUS · INFERRED: 51 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `c82b8b7c`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- testing.T
- ClientTest.php
- errors.go
- WebhookTest
- webhook_test.go
- FakeTransport
- php/composer.json
- composer.json
- Message
- Client
- panmail-sdk
- attachmentJSON
- check.sh
- check-proto-drift.sh
- Result
- coverage-floor.py
- sync-status.py
- sync-webhook-vectors.py
- sync-proto.sh
- github.com/gsoultan/panmail-sdk
- The panmail wire contract
- ProvidersTest
- provider_test.go
- Response
- [0.1.0-rc.1] — 2026-09-02
- ProviderType
- Provider
- Proto
- php/README.md

## God Nodes (most connected - your core abstractions)
1. `FakeTransport` - 55 edges
2. `client()` - 53 edges
3. `ClientTest` - 51 edges
4. `hello()` - 33 edges
5. `ProvidersTest` - 30 edges
6. `Client` - 27 edges
7. `respond()` - 24 edges
8. `WebhookTest` - 22 edges
9. `accepts()` - 21 edges
10. `lists()` - 20 edges

## Surprising Connections (you probably didn't know these)
- `lists()` --calls--> `respond()`  [INFERRED]
  provider_test.go → client_test.go
- `TestListProvidersStopsAtThePageLimit()` --calls--> `respond()`  [INFERRED]
  provider_test.go → client_test.go
- `lists()` --calls--> `writeJSON()`  [INFERRED]
  provider_test.go → client_test.go
- `TestListProvidersStopsAtThePageLimit()` --calls--> `writeJSON()`  [INFERRED]
  provider_test.go → client_test.go
- `TestListProviders()` --calls--> `client()`  [INFERRED]
  provider_test.go → client_test.go

## Import Cycles
- None detected.

## Communities (31 total, 12 thin omitted)

### Community 0 - "testing.T"
Cohesion: 0.11
Nodes (67): New(), accepts(), client(), contentTypeFixture(), hello(), refuse(), respond(), TestAClientIsSafeToUseFromManyGoroutines() (+59 more)

### Community 1 - "ClientTest.php"
Cohesion: 0.05
Nodes (14): JsonException, ApiException, AuthException, BacklogFullException, InvalidMessageException, RateLimitedException, SuppressedRecipientException, TransportException (+6 more)

### Community 2 - "errors.go"
Cohesion: 0.10
Nodes (20): decodeError(), Client, isLoopback(), sleep(), validateBaseURL(), classify(), classifySend(), effectiveCode() (+12 more)

### Community 3 - "WebhookTest"
Cohesion: 0.07
Nodes (8): PanmailException, WebhookException, TriggerEvent, Webhook, WebhookEvent, WebhookTest, PHPUnit\Framework\Attributes\DataProvider, RuntimeException

### Community 4 - "webhook_test.go"
Cohesion: 0.15
Nodes (28): encoding/json.RawMessage, time.Time, TriggerEvent, WebhookError, WebhookEvent, WebhookOption, webhookOptions, signWebhook() (+20 more)

### Community 6 - "php/composer.json"
Cohesion: 0.08
Nodes (23): autoload, autoload-dev, psr-4, psr-4, config, sort-packages, description, license (+15 more)

### Community 7 - "composer.json"
Cohesion: 0.09
Nodes (21): archive, comment, exclude, autoload, psr-4, config, sort-packages, description (+13 more)

### Community 10 - "panmail-sdk"
Cohesion: 0.06
Nodes (29): Working on panmail-sdk, Contributing, Coverage, Releasing, Running everything, Static analysis, The protos, The wire contract (+21 more)

### Community 11 - "attachmentJSON"
Cohesion: 0.32
Nodes (4): Attachment, attachmentJSON, Message, sendRequest

### Community 12 - "check.sh"
Cohesion: 0.83
Nodes (3): have(), run(), check.sh script

### Community 13 - "check-proto-drift.sh"
Cohesion: 0.50
Nodes (3): FILES, RAW, check-proto-drift.sh script

### Community 22 - "The panmail wire contract"
Cohesion: 0.08
Nodes (24): A refusal is not a failure, Authentication, Bcc is the envelope, not a header, cURL, Delivery events and webhooks, Errors, Generating your own client, HTTP JSON (+16 more)

### Community 24 - "provider_test.go"
Cohesion: 0.29
Nodes (21): ids(), lists(), page(), provider(), TestListProviders(), TestListProvidersAsksForAPageAndNoFilterByDefault(), TestListProvidersDecodesOnlyWhatASenderNeeds(), TestListProvidersFollowsEveryPage() (+13 more)

### Community 25 - "Response"
Cohesion: 0.12
Nodes (3): ProviderType, Response, Transport

### Community 26 - "[0.1.0-rc.1] — 2026-09-02"
Cohesion: 0.12
Nodes (15): [0.1.0-rc.1] — 2026-09-02, [0.1.0-rc.2] — 2026-09-26, Added, Added, Added, Added, Changed, Changed (+7 more)

### Community 27 - "ProviderType"
Cohesion: 0.33
Nodes (7): enumName, listProvidersRequest, listProvidersResponse, Provider, ProviderFilter, ProviderType, Client

### Community 29 - "Proto"
Cohesion: 0.40
Nodes (4): Generating, Keeping them current, Proto, The go_package option

## Knowledge Gaps
- **95 isolated node(s):** `name`, `description`, `type`, `license`, `homepage` (+90 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 187 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **12 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `VerifyWebhook()` connect `webhook_test.go` to `errors.go`, `WebhookTest`?**
  _High betweenness centrality (0.262) - this node is a cross-community bridge._
- **Why does `TriggerEvent` connect `WebhookTest` to `webhook_test.go`?**
  _High betweenness centrality (0.258) - this node is a cross-community bridge._
- **Why does `ClientTest` connect `FakeTransport` to `Message`, `ClientTest.php`, `Response`, `Client`?**
  _High betweenness centrality (0.104) - this node is a cross-community bridge._
- **Are the 20 inferred relationships involving `FakeTransport` (e.g. with `.testAJsonScalarPageIsATransportError()` and `.testANameThatCannotBeEncodedIsRefusedBeforeTheGatewayIsCalled()`) actually correct?**
  _`FakeTransport` has 20 INFERRED edges - model-reasoned connections that need verification._
- **Are the 19 inferred relationships involving `client()` (e.g. with `TestARefusalOverTheResponseBoundIsStillClassified()` and `TestListProviders()`) actually correct?**
  _`client()` has 19 INFERRED edges - model-reasoned connections that need verification._
- **What connects `name`, `description`, `type` to the rest of the system?**
  _95 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `testing.T` be split into smaller, more focused modules?**
  _Cohesion score 0.10865191146881288 - nodes in this community are weakly interconnected._