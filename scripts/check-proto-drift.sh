#!/usr/bin/env bash
# Compares the published proto copies against the gateway's.
#
# proto/ is a verbatim copy, and `buf lint` only proves the copy is still valid
# — not that it still matches. Twice now the gateway moved and this repository
# found out because somebody went looking: once a whole enum gained four values,
# once the webhook event vocabulary turned out never to have been what the docs
# claimed. Going looking is not a control.
#
# What this does NOT catch: behaviour that changes without the protos changing.
# A filter rule quarantining a message altered what a send means, with every
# proto byte-identical. Nothing here would have noticed, and nothing here
# pretends to.
#
# Run it locally against a checkout, or with no argument to fetch from GitHub:
#
#     scripts/check-proto-drift.sh                    # fetches the gateway's main
#     scripts/check-proto-drift.sh /path/to/panmail   # compares a local checkout
set -uo pipefail

cd "$(dirname "$0")/.."

readonly FILES=(common.proto event.proto email_service.proto webhook.proto)
readonly RAW="https://raw.githubusercontent.com/gsoultan/panmail/main/api/panmail/v1"

gateway="${1:-}"
scratch=""

if [[ -n "$gateway" ]]; then
    source_dir="$gateway/api/panmail/v1"
    if [[ ! -d "$source_dir" ]]; then
        echo "no protos at $source_dir" >&2
        exit 2
    fi
else
    scratch="$(mktemp -d)"
    # shellcheck disable=SC2064  # expand now, so the path is captured
    trap "rm -rf '$scratch'" EXIT
    source_dir="$scratch"

    for file in "${FILES[@]}"; do
        if ! curl -fsS -o "$scratch/$file" "$RAW/$file"; then
            echo "could not fetch $file from the gateway" >&2
            exit 2
        fi
    done
fi

drifted=()
for file in "${FILES[@]}"; do
    printf '  %-22s' "$file"
    if diff -q "$source_dir/$file" "proto/panmail/v1/$file" >/dev/null 2>&1; then
        printf 'identical\n'
    else
        printf 'DRIFTED\n'
        drifted+=("$file")
        diff -u "proto/panmail/v1/$file" "$source_dir/$file" | sed 's/^/      /'
    fi
done

if [[ ${#drifted[@]} -gt 0 ]]; then
    cat >&2 <<MESSAGE

${#drifted[@]} proto(s) no longer match the gateway: ${drifted[*]}

The copies here are stale, which means this SDK's documented contract is
describing a gateway that no longer exists. To fix:

    scripts/sync-proto.sh /path/to/panmail   # re-copy
    scripts/sync-status.py                   # regenerate the enum fixtures

Then run the suites: a value the gateway added and the clients have not
followed fails the fixture tests in all three languages, which is the point.
MESSAGE
    exit 1
fi

echo
echo "the published protos match the gateway"
