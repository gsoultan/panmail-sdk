# Wire contract — what the gateway really does

The gateway is public at github.com/gsoultan/panmail and is not cloned beside
this repo. Read it with `gh api -H 'Accept: application/vnd.github.raw'
repos/gsoultan/panmail/contents/<path>`, or a shallow clone into /tmp.

## Where the truth lives in the gateway

- `internal/auth/middlewares/policy.go` — the exhaustive procedure → role / API-key
  scope table. An empty scope means API keys cannot call the procedure at all.
- `internal/auth/entities/scope.go` — every scope. `email:send` is the only default.
- `pkg/db/paging.go` — list page tokens are base64 offsets.
- `internal/sdkcontract` — gateway-side tests holding the *published* Go SDK (the
  version in its go.mod) to the wire. Send only, as of 2026-09-28.

## Procedures the clients call

| Procedure | Scope |
| --- | --- |
| `/panmail.v1.EmailService/SendEmail` | `email:send` |
| `/panmail.v1.EmailProviderService/ListEmailProviders` | `providers:read` |

## Behaviour that is not in the protos

- connect-go decodes JSON with `DiscardUnknown`, which drops unknown enum *names*
  as well as unknown fields: `"type": "SMTP"` is decoded as no filter, silently.
  Re-check any enum filter against the results.
- protojson writes an enum value its descriptor cannot name as a bare number.
  `provider_type.proto` reserves 2–5 because stored rows may carry them, so a
  decoder that insists on a string fails a whole list over one old row.
- proto3 JSON omits zero values: an absent enum is `*_UNSPECIFIED`, not `""`.
- The same Connect code means different things on different procedures:
  `failed_precondition` is a suppressed recipient only on a send. The clients
  classify per call (`classifySend` vs `classify`).
- The key travels in `X-API-Key`, never `Authorization` (that is a dashboard
  session). Redirects are refused; responses are bounded at 1 MiB.
