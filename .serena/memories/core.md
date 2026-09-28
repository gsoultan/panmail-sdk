# panmail-sdk — core

Go and PHP clients for a panmail gateway. They speak the Connect protocol's JSON
mode over plain HTTP — no protobuf runtime, no generated code. The wire contract
is `docs/WIRE.md`, and a change to what goes on the wire is written there first.

## Read next

- `mem:wire-contract` — the procedures called, the scopes they need, and what the
  gateway actually does, checked against its source rather than assumed.
- `mem:provider-listing` — why ListProviders is shaped the way it is, and the
  gateway gaps found while building it.
- `mem:testing-and-verification` — parallel suites, generated fixtures, mutation
  checks, coverage floors.
- `mem:working-in-this-checkout` — the owner edits here at the same time; how to
  commit without colliding.

## Standing rules

- Two clients, one library: the same behaviour in Go and PHP, or a written reason
  for the difference (`CONTRIBUTING.md`).
- `AGENTS.md` is the roster — api, wire, sec, ops. A non-trivial change names its
  Driver and Challenger, and answers the Challenger's vetoes.
- `proto/` is a verbatim copy of the gateway's `api/panmail/v1`. Re-copy with
  `scripts/sync-proto.sh`, regenerate fixtures with `scripts/sync-status.py`;
  never hand-edit either.
- No AI attribution in commits, pull requests or comments.
