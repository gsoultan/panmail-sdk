# Proto

The wire contract for sending, copied **verbatim** from the gateway so the two
cannot drift. Three files, which is the whole import closure of
`EmailService.SendEmail`:

| File | Why it is here |
| --- | --- |
| `email_service.proto` | `EmailService`, `SendEmailRequest`, `SendEmailResponse` |
| `common.proto` | `Attachment` |
| `event.proto` | `EmailEventType`, the enum `SendEmailResponse.status` uses |

You do **not** need these to use the SDKs — they speak the protocol already.
They are here for the case the SDKs do not cover: generating your own client,
in a language none of the four SDKs serve, or in a codebase that already has a
protobuf toolchain and would rather use it.

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
