# Testing and verification

- `scripts/check.sh` runs what CI runs (gofmt, vet, `go test -race`, golangci-lint,
  proto drift against the gateway, phpunit, PHPStan at max) and lists what it
  skipped. `buf lint proto` is separate — CI's proto job runs it.
- Coverage floors are 95% in both. PHP needs a driver:
  `XDEBUG_MODE=coverage vendor/bin/phpunit --coverage-clover=… --coverage-filter src`,
  then `scripts/coverage-floor.py`.
- The suites are parallel on purpose: the same test names in Go and PHP, so a gap
  in one is visible next to the other.
- Values both clients must agree on live in `testdata/` fixtures that every suite
  reads. The enum fixtures are generated from the protos by
  `scripts/sync-status.py`; its pattern must match digits, or `POP3`-style names
  vanish in silence.
- golangci-lint's `errorlint` applies to tests too: use `errors.As`, never a type
  assertion or `==` on an error.
- **Prove a test catches its bug** by removing the guard in a scratch copy
  (`rsync` the repo to /tmp minus `.git`) and watching the test fail. For PHP,
  copy `php/vendor` — do not symlink it, or Composer's autoloader resolves
  `Panmail\` back to the real `src` and every mutation looks like it survived.
  Check with `php -r 'require "vendor/autoload.php"; echo (new
  ReflectionClass(Panmail\Client::class))->getFileName();'`.
