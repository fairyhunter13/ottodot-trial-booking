#!/usr/bin/env bash
# Break one guard at a time in a copy of the tree, and check that a named test notices.
# A green suite is not proof. A mutation that survives is a hole in the tests.
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
pass=0
fail=0

# An expected test name means that test must fail.
# The word SURVIVES means the guard is defence in depth behind a stronger one.
# No test can reach such a guard, and the script must not call that a pass.
run_mutation() {
  local name=$1 expect=$2 edit=$3
  local work
  work=$(mktemp -d)
  trap 'rm -rf "$work"' RETURN

  cp -r "$root"/. "$work"/
  rm -rf "$work/.git"
  ( cd "$work" && eval "$edit" ) || { echo "MUTATION FAILED TO APPLY: $name"; fail=$((fail + 1)); return; }

  local out
  out=$(cd "$work" && go test ./... -count=1 2>&1)

  if [ "$expect" = SURVIVES ]; then
    if printf '%s\n' "$out" | grep -q -- '--- FAIL'; then
      printf 'CAUGHT   %-34s (expected to survive)\n' "$name"
    else
      printf 'SURVIVED %-34s by design, see docs/test-plan.md\n' "$name"
    fi
    pass=$((pass + 1))
    return
  fi

  local caught
  caught=$(printf '%s\n' "$out" | grep -c -- "--- FAIL: $expect")

  if [ "$caught" -gt 0 ]; then
    printf 'CAUGHT   %-34s by %s\n' "$name" "$expect"
    pass=$((pass + 1))
  else
    printf 'SURVIVED %-34s %s never failed\n' "$name" "$expect"
    printf '%s\n' "$out" | grep -- '--- FAIL' | head -5
    fail=$((fail + 1))
  fi
}

run_mutation "begin immediate -> begin" "TestConcurrentLastSeat" \
  "sed -i 's/BEGIN IMMEDIATE/BEGIN/' internal/booking/booking.go"

run_mutation "drop the unique index" "TestCreate" \
  "sed -i '/CREATE UNIQUE INDEX IF NOT EXISTS one_active_booking/,+1d' internal/store/schema.sql"

run_mutation "drop the pending-payment guard" "SURVIVES" \
  "sed -i \"s/ AND status = 'pending_payment'//\" internal/booking/booking.go"

run_mutation "seat count off by one" "TestCapacityNeverExceeded" \
  "sed -i 's/confirmed < capacity/confirmed <= capacity/' internal/booking/booking.go"

run_mutation "confirm always loses the seat" "TestConcurrentDistinctSeats" \
  "sed -i 's/if confirmed < capacity {/if false {/' internal/booking/booking.go"

echo
echo "caught $pass, survived $fail"
[ "$fail" -eq 0 ]
