#!/usr/bin/env bash
# Re-copies the published protos from a panmail checkout.
#
# The published set is deliberately byte-identical to the gateway's, so drift
# is impossible to introduce by accident. Run this after the gateway's proto
# changes, and commit whatever it produces.
set -euo pipefail

gateway="${1:-}"
if [[ -z "$gateway" ]]; then
    echo "usage: $0 /path/to/panmail" >&2
    exit 2
fi

source_dir="$gateway/api/panmail/v1"
target_dir="$(dirname "$0")/../proto/panmail/v1"

if [[ ! -d "$source_dir" ]]; then
    echo "no protos at $source_dir" >&2
    exit 1
fi

for file in common.proto event.proto email_service.proto; do
    cp "$source_dir/$file" "$target_dir/$file"
    echo "synced $file"
done
