#!/usr/bin/env python3
"""Regenerates testdata/webhook-signatures.json.

The vectors are computed here, in Python, rather than by any of the clients.
That is the point: a client matching them is agreeing with an implementation
that is not its own, and the same file is read by all three test suites, so the
one calculation where disagreeing means silently rejecting real deliveries
cannot drift.

They were checked against the gateway's own sign() in
internal/webhook/worker/delivery.go when they were written. If the gateway
changes its scheme, this file changes with it — and three test suites go red
until the clients follow.
"""

import collections
import hashlib
import hmac
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parent.parent
FIXTURE = ROOT / "testdata" / "webhook-signatures.json"

COMMENT = [
    "Known-good signatures for the gateway's webhook scheme, and the only place",
    "the three clients' HMAC implementations are checked against each other.",
    "",
    "The scheme is: HMAC-SHA256 over the timestamp, a dot, then the raw body,",
    "hex encoded and prefixed 'sha256='. It is implemented in the gateway at",
    "internal/webhook/worker/delivery.go; these vectors were computed",
    "independently by scripts/sync-webhook-vectors.py, so a client matching",
    "them is agreeing with something other than itself.",
    "",
    "'body with a dot' exists because the timestamp and body are joined by one:",
    "a body that starts with what looks like a prefix must not be confusable",
    "with a different timestamp. 'whitespace matters' exists because the",
    "signature covers bytes, not the JSON value they decode to. 'past the",
    "32-bit boundary' is 2^31, where a client storing seconds in an int32 stops",
    "agreeing with everyone else in 2038.",
]

CASES = [
    ("plain ascii", "whsec_test", 1700000000, '{"event":"mail.bounced"}'),
    ("empty body", "whsec_test", 1700000000, ""),
    ("zero timestamp", "whsec_test", 0, '{"a":1}'),
    ("negative timestamp", "whsec_test", -1, '{"a":1}'),
    ("non-ascii body", "whsec_test", 1700000000, '{"subject":"héllo → wörld 🙂"}'),
    ("non-ascii secret", "clé-secrète-🔑", 1700000000, '{"a":1}'),
    ("body with a dot", "whsec_test", 1700000000, '1700000000.{"a":1}'),
    ("whitespace matters", "whsec_test", 1700000000, '{ "a" : 1 }'),
    ("past the 32-bit boundary", "whsec_test", 2147483648, '{"a":1}'),
]


def main() -> int:
    vectors = []
    for name, secret, timestamp, body in CASES:
        mac = hmac.new(
            secret.encode("utf-8"),
            f"{timestamp}.".encode("utf-8") + body.encode("utf-8"),
            hashlib.sha256,
        )
        vectors.append(
            collections.OrderedDict(
                name=name,
                secret=secret,
                timestamp=timestamp,
                body=body,
                signature="sha256=" + mac.hexdigest(),
            )
        )

    fixture = collections.OrderedDict()
    fixture["_comment"] = COMMENT
    fixture["vectors"] = vectors
    FIXTURE.write_text(
        json.dumps(fixture, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
    )
    print(f"wrote {len(vectors)} vectors to {FIXTURE.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
