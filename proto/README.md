# Proto

The wire contract, copied **verbatim** from the gateway so the two cannot
drift: the import closures of the two procedures the SDKs call,
`EmailService.SendEmail` and `EmailProviderService.ListEmailProviders`, and the
webhook vocabulary.

| File | Why it is here |
| --- | --- |
| `email_service.proto` | `EmailService`, `SendEmailRequest`, `SendEmailResponse` |
| `common.proto` | `Attachment` |
| `event.proto` | `EmailEventType`, the enum `SendEmailResponse.status` uses |
| `webhook.proto` | `WebhookTriggerEvent`, what a webhook delivery carries |
| `email_provider_service.proto` | `ListEmailProvidersRequest`, `ListEmailProvidersResponse` — and, because the file is copied whole, the rest of `EmailProviderService`, which the SDKs never call |
| `email_provider.proto` | `EmailProvider` and its vendor configurations |
| `provider_type.proto` | `ProviderType` |

You do **not** need these to use the SDKs — they speak the protocol already.
They are here for the case the SDKs do not cover: generating your own client,
in a language neither SDK serves, or in a codebase that already has a protobuf
toolchain and would rather use it.

`buf.yaml` exempts `email_provider_service.proto` from three RPC naming rules:
`TestEmailProviderConfig` reuses the create request, and the file is the
gateway's to change, not this copy's.

## Generating

```bash
buf generate                       # with a buf.gen.yaml of your own
protoc -I proto --python_out=. proto/panmail/v1/*.proto
```

## The go_package option

`option go_package` in these files names the **gateway's** own generated
package, which is private. It is inert unless you generate Go, and if you do,
override it:

```bash
protoc -I proto --go_opt=Mpanmail/v1/email_service.proto=your/module/path ...
```

The Go SDK in this repo does not use these files at all — it speaks the Connect
protocol's JSON mode directly, which is why it has no protobuf dependency.

## Keeping them current

They are copies. When the gateway's proto changes, re-copy:

```bash
./scripts/sync-proto.sh /path/to/panmail
```

The contract tests in the gateway repo are what actually catch drift — see the
note in `docs/WIRE.md`.
