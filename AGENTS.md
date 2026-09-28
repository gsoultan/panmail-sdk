# Working on panmail-sdk

A change here is worked as a pair. Take the **Driver** profile that owns the code
you touch, then re-read your own diff as the **Challenger** — the profile whose
budget the change most likely breaks — and answer its vetoes before calling it
done. Name both in the summary: `Driver: api · Challenger: sec`.

[`CONTRIBUTING.md`](CONTRIBUTING.md) is how to run things. This is who says no.

| Profile | Owns | Vetoes | Proof |
| --- | --- | --- | --- |
| **api** | The exported surface in both languages: names, types, zero values, doc comments, and Go and PHP agreeing on all of it. | Surface a sender does not need. A behaviour in one client and not the other with no written reason. A removal or rename outside a release that says so. | The same test names in both suites. README and package examples that match the code. |
| **wire** | `docs/WIRE.md`, the copies in `proto/`, the fixtures in `testdata/`, procedure paths and JSON field names. | A wire change not written in `docs/WIRE.md` first. A hand-edited proto. A constant not checked against a generated fixture. A claim about the gateway nobody checked against its source. | `scripts/check-proto-drift.sh`, `scripts/sync-status.py` leaving no diff, the fixture tests. |
| **sec** | The API key — its header, https, redirects, userinfo — response bounds, and what the clients decode from the gateway and hand back. | Anything that could carry the key to another host. A read or loop bounded only by what the gateway sends. Decoding configuration or credentials a caller does not need. Docs that suggest a broader scope than the job takes. | The redirect, https and userinfo tests, the bound tests, the "decodes only" tests. |
| **ops** | Retries, timeouts, error classification, and what is safe to repeat. | Retrying a send whose outcome is unknown. Reading a code by the meaning it has on another procedure. A wait that ignores the caller's deadline. | Classification tests per call, the no-retry tests, the deadline tests. |

Standing truths: a fast path that skips a check is a vulnerability; a loop that
trusts the other side to stop is unbounded; every bug fix ships a test that
fails before it and passes after, with the root cause named in one sentence.
