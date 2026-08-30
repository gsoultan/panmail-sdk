#!/usr/bin/env bash
# Runs everything CI runs, for whichever of the four toolchains are installed.
#
# Nobody has all four to hand, and a contributor who has only Go should not be
# blocked — but they should be told what went unchecked rather than left to
# assume a green run covered everything. Missing toolchains are skipped and
# listed at the end; anything that actually ran and failed fails this script.
set -uo pipefail

cd "$(dirname "$0")/.."

pass=0 fail=0
skipped=()
failed=()

have() { command -v "$1" >/dev/null 2>&1; }

run() {
    local name=$1
    shift
    printf '  %-34s' "$name"
    if output=$("$@" 2>&1); then
        printf 'ok\n'
        pass=$((pass + 1))
    else
        printf 'FAILED\n'
        printf '%s\n' "$output" | sed 's/^/      /'
        fail=$((fail + 1))
        failed+=("$name")
    fi
}

echo 'Go'
if have go; then
    run 'gofmt (must print nothing)' bash -c '[ -z "$(gofmt -l .)" ]'
    run 'go vet' go vet ./...
    run 'go test -race' go test -race ./...
    if have golangci-lint; then
        # golangci-lint takes a lock that is global to the machine rather than
        # to the project, so a run in another checkout blocks this one. That is
        # not a finding about this code, and reporting it as one would teach a
        # contributor to ignore the linter's output.
        if lint_output=$(golangci-lint run ./... 2>&1); then
            printf '  %-34sok\n' 'golangci-lint'
            pass=$((pass + 1))
        elif printf '%s' "$lint_output" | grep -q 'parallel golangci-lint is running'; then
            printf '  %-34sskipped\n' 'golangci-lint'
            skipped+=('golangci-lint (another run holds the machine-wide lock)')
        else
            printf '  %-34sFAILED\n' 'golangci-lint'
            printf '%s\n' "$lint_output" | sed 's/^/      /'
            fail=$((fail + 1))
            failed+=('golangci-lint')
        fi
    else
        skipped+=('golangci-lint (not installed)')
    fi
else
    skipped+=('Go (no go)')
fi

echo 'PHP'
if have php && [ -x php/vendor/bin/phpunit ]; then
    run 'phpunit' bash -c 'cd php && vendor/bin/phpunit'
    run 'phpstan (src at max)' bash -c 'cd php && composer analyse'
elif have php; then
    skipped+=('PHP (run: cd php && composer install)')
else
    skipped+=('PHP (no php)')
fi

echo 'Java'
if have mvn; then
    run 'mvn test' bash -c 'cd java && mvn --batch-mode test'
else
    skipped+=('Java (no mvn)')
fi

echo 'Node'
if have bun; then
    run 'tsc --noEmit' bash -c 'cd node && bun x tsc -p tsconfig.json --noEmit'
    run 'bun test' bash -c 'cd node && bun test src'
    run 'build' bash -c 'cd node && bun run build'
    run 'published types (consumer)' bash -c 'cd node && bun run check:types'
    if have node; then
        # The suite runs under bun. This is the package on the runtime
        # package.json promises.
        run "smoke on $(node --version)" bash -c 'cd node && node smoke.mjs'
    else
        skipped+=('node smoke (no node)')
    fi
else
    skipped+=('Node (no bun)')
fi

echo
if [ ${#skipped[@]} -gt 0 ]; then
    echo 'Skipped:'
    printf '  - %s\n' "${skipped[@]}"
fi
if [ "$fail" -gt 0 ]; then
    echo
    echo "FAILED: ${failed[*]}"
    echo "$pass passed, $fail failed"
    exit 1
fi
echo "$pass passed, nothing failed"
