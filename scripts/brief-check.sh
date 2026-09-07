#!/usr/bin/env bash
# Every line of the Ottodot brief is one row below. A row that stops being true fails the build.
#
#   ./scripts/brief-check.sh            every row must hold
#   ./scripts/brief-check.sh --honest   every row must fail when its own break is applied
#   ./scripts/brief-check.sh --prose    print the prose findings, and change nothing
#
# The honest pass is the reason to trust the rest. A row that passes whatever the repo says is
# decoration, so each row carries the exact edit that must make it fail.
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
log=.brief-check.log
mode=${1:-check}

# ---------------------------------------------------------------- helpers ----
# Each helper runs inside the tree it reads, so every path below is relative.

has() { grep -qF -- "$2" "$1"; }

# The block from one heading to the next heading of the same level or higher.
body() {
  awk -v h="$2" '
    BEGIN { match(h, /^#+/); n = RLENGTH }
    $0 == h { on = 1; next }
    on && /^#/ { match($0, /^#+/); if (RLENGTH <= n) exit }
    on { print }
  ' "$1"
}

section() {
  local text
  text=$(body "$1" "$2")
  [ "${#text}" -ge "${3:-40}" ]
}

section_has() {
  local file=$1 heading=$2 text w
  shift 2
  text=$(body "$file" "$heading")
  for w in "$@"; do
    printf '%s\n' "$text" | grep -qF -- "$w" || return 1
  done
}

# The index route registers as GET /{$} and the readme writes it GET /?parent=. Both mean GET /.
norm_route() { sed 's/{\$}//; s/?.*$//'; }

routes() {
  grep -o 'mux.HandleFunc("[^"]*"' internal/web/handlers.go |
    sed 's/.*("//; s/"$//' | norm_route | sort
}

doc_routes() {
  body README.md '## What I built' |
    sed -n 's/^| `\([^`]*\)`.*/\1/p' | norm_route | sort
}

route_present() { routes | grep -qxF -- "$1"; }

tables() {
  grep -o 'CREATE TABLE IF NOT EXISTS [a-z_]*' internal/store/schema.sql |
    awk '{ print $NF }' | sort
}

table_present() { tables | grep -qxF -- "$1"; }

# A named test ran and passed. No row asserts that a string sits in a source file.
passed() { grep -q -- "--- PASS: .*$1" "$log"; }

all_ec() {
  local i
  for i in 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 20 21 22 23; do
    passed "EC-$i" || return 1
  done
}

readme_go() { grep -o 'Go [0-9]\+\.[0-9]\+' README.md | head -1 | awk '{ print $2 }'; }
gomod_go() { awk '/^go /{ print $2 }' go.mod | cut -d. -f1,2; }

# Every command the readme asks a reviewer to run is also a step in CI.
verify_in_ci() {
  local c
  while read -r c; do
    [ -n "$c" ] || continue
    has .github/workflows/ci.yml "$c" || return 1
  done < <(body README.md '## How to run' | sed -n '/^\(go vet\|go test\|\.\/scripts\)/p')
}

no_empty_section() {
  local h
  while read -r h; do
    section README.md "$h" 40 || return 1
  done < <(grep '^## ' README.md)
}

# ------------------------------------------------------------------ prose ----
# The structural rules only. The word rules need the approved Dictionary, which is copyright ASD
# and is not redistributable, so no check here can prove that a word is approved.
read -r -d '' PROSE_AWK <<'AWK'
function flush(   b, parts, n, i, w, c) {
  if (buf == "") return
  b = buf; buf = ""
  gsub(/`[^`]*`/, "C", b)          # rule 8.6: an identifier counts as one word
  gsub(/\([^)]*\)/, "P", b)        # rule 8.6: text in parentheses counts as one word
  gsub(/https?:\/\/[^ ]+/, "U", b)
  gsub(/[*_]/, "", b)
  if (rule == "semi") {
    if (index(b, ";")) print FILENAME ":" start ": semicolon"
    return
  }
  if (rule == "tense") {
    if (b ~ /(has|have|had) been/ || b ~ /(has|have|had) [a-z]+ed[ .,]/ || b ~ /would have/)
      print FILENAME ":" start ": compound tense"
    return
  }
  if (rule == "this") {
    if (b ~ /(^|[.!?] )This (is|was|are|were|means|makes|does|do|can|will|would|has|had|gives)/)
      print FILENAME ":" start ": bare This"
    return
  }
  n = split(b, parts, /[.!?] |[.!?]$/)
  for (i = 1; i <= n; i++) {
    c = split(parts[i], w, / +/)
    if (c > 25) print FILENAME ":" start ": " c " words"
  }
}
/^```/ { fence = !fence; flush(); next }
fence  { next }
/^#/   { flush(); next }
/^\|/  { flush(); next }
/^ *$/ { flush(); next }
/^ *([-*]|[0-9]+\.) / { flush(); sub(/^ *([-*]|[0-9]+\.) /, "") }
{ if (buf == "") start = FNR; buf = (buf == "" ? $0 : buf " " $0) }
END { flush() }
AWK

DOCS="README.md AI_USAGE.md docs/development-plan.md docs/test-plan.md docs/demo-script.md"

prose_find() { awk -v rule="$1" "$PROSE_AWK" $DOCS; }
prose() { [ -z "$(prose_find "$1")" ]; }

# ------------------------------------------------------------------- rows ----
ids=(); needs=(); checks=(); breaks=()
row() { ids+=("$1"); needs+=("$2"); checks+=("$3"); breaks+=("$4"); }

# H - header and timebox
row H1 "cap your time at 4 hours" \
  "section README.md '## Time spent' 20 && body README.md '## Time spent' | grep -q '[0-9]'" \
  "sed -i 's/^About .*/Not recorded./' README.md"
row H2 "leave notes on what you would do next" \
  "section README.md '## What I would do next' 100" \
  "sed -i '/^## What I would do next/,/^## /{/^## /!d}' README.md"
row H3 "how you steer it, question it" \
  "section AI_USAGE.md '## One place where I disagreed with, corrected, or rejected the output' 200" \
  "sed -i '/^## One place where I disagreed/,/^## /{/^## /!d}' AI_USAGE.md"

# C - context
row C1 "live online science and math classes" \
  "passed 'TestSeedMeetsBrief/C1'" \
  "sed -i 's#PASS: TestSeedMeetsBrief/C1#FAIL: TestSeedMeetsBrief/C1#' $log"
row C2 "an accurate roster before class starts" \
  "route_present 'GET /classes/{id}/roster' && passed 'EC-20'" \
  "sed -i 's#PASS: TestCreate/EC-20#FAIL: TestCreate/EC-20#' $log"
row C3 "capped at 4 students per class" \
  "passed 'TestSeedMeetsBrief/C3'" \
  "sed -i 's#PASS: TestSeedMeetsBrief/C3#FAIL: TestSeedMeetsBrief/C3#' $log"

# B - what to build
row B0 "implement trial booking only, and no regular enrollment" \
  "section_has README.md '## What I deliberately cut' 'enrollment'" \
  "sed -i 's/Regular enrollment/Regular signup/' README.md"
row B1 "a parent chooses a child and picks an available class" \
  "route_present 'GET /' && passed 'TestSeedMeetsBrief/B1'" \
  "sed -i 's#PASS: TestSeedMeetsBrief/B1#FAIL: TestSeedMeetsBrief/B1#' $log"
row B2 "a parent submits a trial booking" \
  "route_present 'POST /bookings'" \
  "sed -i '/POST \/bookings\"/d' internal/web/handlers.go"
row B3 "a mock payment step, or the payment result recorded" \
  "route_present 'POST /bookings/{id}/pay' && table_present payment_attempts" \
  "sed -i 's/CREATE TABLE IF NOT EXISTS payment_attempts/CREATE TABLE IF NOT EXISTS charges/' internal/store/schema.sql"
row B4 "the booking status shown after submission" \
  "route_present 'GET /bookings/{id}'" \
  "sed -i '/GET \/bookings\/{id}\"/d' internal/web/handlers.go"
row B5 "an admin or teacher sees the roster, or a roster API" \
  "route_present 'GET /classes/{id}/roster' && route_present 'GET /api/classes/{id}/roster'" \
  "sed -i '/GET \/api\/classes/d' internal/web/handlers.go"
row B6 "prevent duplicate confirmed bookings for the same child and class" \
  "has internal/store/schema.sql one_active_booking && passed 'EC-02' && passed 'EC-03'" \
  "sed -i 's/one_active_booking/one_booking_maybe/' internal/store/schema.sql"
row B7 "prevent overbooking beyond 4 confirmed students" \
  "passed 'EC-18' && passed 'EC-21'" \
  "sed -i 's#PASS: TestConcurrentLastSeat/EC-18#FAIL: TestConcurrentLastSeat/EC-18#' $log"
row B8 "handle payment failure without adding the child to the roster" \
  "passed 'EC-12'" \
  "sed -i 's#PASS: TestPayStates/EC-12#FAIL: TestPayStates/EC-12#' $log"
row B9 "handle the last-seat race condition" \
  "passed 'TestScriptedLastSeat' && passed 'EC-14'" \
  "sed -i 's#PASS: TestScriptedLastSeat#FAIL: TestScriptedLastSeat#' $log"

# L - the required last-seat scenario
row L1 "the 4 steps of the scenario, in order" \
  "passed 'TestScriptedLastSeat'" \
  "sed -i 's#PASS: TestScriptedLastSeat#FAIL: TestScriptedLastSeat#' $log"
row L2 "at most one confirmed booking for the last seat" \
  "passed 'EC-18' && passed 'TestHTTPLastSeatRace'" \
  "sed -i 's#PASS: TestHTTPLastSeatRace#FAIL: TestHTTPLastSeatRace#' $log"
row L3 "readme: the approach you chose" \
  "section_has README.md '### How two users compete for the last seat' '**The approach.**'" \
  "sed -i 's/\*\*The approach\.\*\*//' README.md"
row L4 "readme: why you chose it" \
  "section_has README.md '### How two users compete for the last seat' '**Why.**'" \
  "sed -i 's/\*\*Why\.\*\*//' README.md"
row L5 "readme: what tradeoffs you accepted" \
  "section_has README.md '### How two users compete for the last seat' '**The tradeoffs I accepted.**'" \
  "sed -i 's/\*\*The tradeoffs I accepted\.\*\*//' README.md"

# D - backend design requirements, all in the readme
row D1 "your data model or schema" \
  "section_has README.md '### Data model' parents students trial_classes bookings payment_attempts" \
  "sed -i 's/payment_attempts/charge_log/g' README.md"
row D2 "the key API endpoints" \
  "[ \"\$(routes)\" = \"\$(doc_routes)\" ]" \
  "sed -i 's#mux.HandleFunc(\"POST /bookings\", s.create)#&\n\tmux.HandleFunc(\"GET /undocumented\", s.index)#' internal/web/handlers.go"
row D3 "booking statuses used" \
  "section_has README.md '### Booking statuses' pending_payment confirmed payment_failed cancelled" \
  "sed -i 's/cancelled/dropped/g' README.md"
row D4 "how you prevent duplicate bookings" \
  "section README.md '### How duplicates are prevented' 200" \
  "sed -i '/^### How duplicates are prevented/,/^#/{/^#/!d}' README.md"
row D5 "how you handle payment failure" \
  "section README.md '### How payment failure is handled' 200" \
  "sed -i '/^### How payment failure is handled/,/^#/{/^#/!d}' README.md"
row D6 "how you handle two users competing for the last seat" \
  "section README.md '### How two users compete for the last seat' 400" \
  "sed -i '/^### How two users compete for the last seat/,/^#/{/^#/!d}' README.md"
row D7 "which checks belong in the UI, backend, database or background job" \
  "section_has README.md '### Which check belongs where' UI backend database 'background job'" \
  "sed -i 's/| background job |/| a cron |/' README.md"

# M - the suggested model
row M1 "a small synthetic dataset that applies" \
  "passed 'TestSeedMeetsBrief'" \
  "sed -i 's#PASS: TestSeedMeetsBrief#FAIL: TestSeedMeetsBrief#' $log"
row M2 "parents" "table_present parents" \
  "sed -i 's/IF NOT EXISTS parents/IF NOT EXISTS guardians/' internal/store/schema.sql"
row M3 "students" "table_present students" \
  "sed -i 's/IF NOT EXISTS students/IF NOT EXISTS kids/' internal/store/schema.sql"
row M4 "trial_classes" "table_present trial_classes" \
  "sed -i 's/IF NOT EXISTS trial_classes/IF NOT EXISTS sessions/' internal/store/schema.sql"
row M5 "bookings" "table_present bookings" \
  "sed -i 's/IF NOT EXISTS bookings/IF NOT EXISTS reservations/' internal/store/schema.sql"
row M6 "payment_attempts" "table_present payment_attempts" \
  "sed -i 's/IF NOT EXISTS payment_attempts/IF NOT EXISTS charges/' internal/store/schema.sql"
row M7 "keep the model small" \
  "[ \"\$(tables | wc -l)\" -eq 5 ]" \
  "printf 'CREATE TABLE IF NOT EXISTS invoices (id TEXT PRIMARY KEY);\n' >> internal/store/schema.sql"

# S - seed data and edge cases
row S1 "seed data or setup steps, so the demo runs quickly" \
  "section_has README.md '## How to run' 'go run .'" \
  "sed -i 's/^go run \./start the server/' README.md"
row S2 "a class with available seats" \
  "passed 'TestSeedMeetsBrief/S2'" \
  "sed -i 's#PASS: TestSeedMeetsBrief/S2#FAIL: TestSeedMeetsBrief/S2#' $log"
row S3 "a class with exactly 3 confirmed students" \
  "passed 'TestSeedMeetsBrief/S3'" \
  "sed -i 's#PASS: TestSeedMeetsBrief/S3#FAIL: TestSeedMeetsBrief/S3#' $log"
row S4 "a duplicate booking attempt for the same child and class" \
  "passed 'TestSeedMeetsBrief/S4'" \
  "sed -i 's#PASS: TestSeedMeetsBrief/S4#FAIL: TestSeedMeetsBrief/S4#' $log"
row S5 "a payment failure case" \
  "passed 'TestSeedMeetsBrief/S5'" \
  "sed -i 's#PASS: TestSeedMeetsBrief/S5#FAIL: TestSeedMeetsBrief/S5#' $log"

# T - what to submit
row T1 "the module path agrees with the public repo link" \
  "grep -qx 'module github.com/fairyhunter13/ottodot-trial-booking' go.mod" \
  "sed -i 's#module github.com/fairyhunter13/ottodot-trial-booking#module example.com/trial#' go.mod"
row T2 "README.md" "[ -s README.md ]" "rm -f README.md"
row T3 "your implementation" \
  "[ -f main.go ] && passed 'TestParentJourney'" \
  "rm -f main.go"
row T3b "the Go version the readme claims is the one go.mod requires" \
  "[ -n \"\$(readme_go)\" ] && [ \"\$(readme_go)\" = \"\$(gomod_go)\" ]" \
  "sed -i 's/^go 1\./go 9./' go.mod"
row T4 "synthetic data or setup instructions" \
  "[ -s internal/store/seed.sql ] && section README.md '## Seed data' 200" \
  "sed -i '/^## Seed data/,/^## /{/^## /!d}' README.md"
row T5 "tests or clear verification steps, and CI runs every one of them" \
  "verify_in_ci" \
  "sed -i '/scripts\/mutate.sh/d' .github/workflows/ci.yml"
row T6 "AI_USAGE.md" "[ -s AI_USAGE.md ]" "rm -f AI_USAGE.md"
row T7 "zip files are not accepted" \
  "! find . -name '*.zip' -not -path './.git/*' | grep -q ." \
  "touch submission.zip"

# R - the readme sections the brief names
row R1 "how to run your solution" "section README.md '## How to run' 100" \
  "sed -i '/^## How to run/,/^## /{/^## /!d}' README.md"
row R2 "what you built" "section README.md '## What I built' 100" \
  "sed -i '/^## What I built/,/^## /{/^## /!d}' README.md"
row R3 "time spent" "section README.md '## Time spent' 20" \
  "sed -i '/^## Time spent/,\$d' README.md"
row R4 "assumptions you made" "section README.md '## Assumptions' 100" \
  "sed -i '/^## Assumptions/,/^## /{/^## /!d}' README.md"
row R5 "key architecture and backend decisions" "section README.md '## Backend design' 400" \
  "sed -i '/^## Backend design/,/^## Tests/{/^## /!d}' README.md"
row R6 "what you deliberately cut" "section README.md '## What I deliberately cut' 100" \
  "sed -i '/^## What I deliberately cut/,/^## /{/^## /!d}' README.md"
row R7 "what you would monitor after release" "section README.md '## What I would monitor after release' 100" \
  "sed -i '/^## What I would monitor after release/,/^## /{/^## /!d}' README.md"
row R8 "what you would do next with more time" "section README.md '## What I would do next' 100" \
  "sed -i '/^## What I would do next/,/^## /{/^## /!d}' README.md"

# A - the AI_USAGE.md sections the brief names
row A1 "which AI tools you used" "section AI_USAGE.md '## Which tools I used' 40" \
  "sed -i '/^## Which tools I used/,/^## /{/^## /!d}' AI_USAGE.md"
row A2 "what you used AI for" "section AI_USAGE.md '## What I used it for' 40" \
  "sed -i '/^## What I used it for/,/^## /{/^## /!d}' AI_USAGE.md"
row A3 "one place where AI helped you move faster" \
  "section AI_USAGE.md '## One place where AI moved me faster' 100" \
  "sed -i '/^## One place where AI moved me faster/,/^## /{/^## /!d}' AI_USAGE.md"
row A4 "one place where you disagreed with, corrected or rejected the output" \
  "section AI_USAGE.md '## One place where I disagreed with, corrected, or rejected the output' 100" \
  "sed -i '/^## One place where I disagreed/,/^## /{/^## /!d}' AI_USAGE.md"
row A5 "what you would change about your AI workflow" \
  "section AI_USAGE.md '## What I would change about my AI workflow' 100" \
  "sed -i '/^## What I would change about my AI workflow/,/^## /{/^## /!d}' AI_USAGE.md"
row A6 "how you verified the final implementation" \
  "section AI_USAGE.md '## How I verified the final implementation' 100" \
  "sed -i '/^## How I verified the final implementation/,\$d' AI_USAGE.md"

# E - what the brief says it evaluates
row E1 "backend and data-model judgment: every added field is explained" \
  "section_has README.md '### Data model' capacity starts_at reason refund_required" \
  "sed -i 's/refund_required/refund_flag/g' README.md"
row E2 "correctness under payment and double-booking edge cases" \
  "all_ec" \
  "sed -i 's#PASS: TestCreate/EC-05#FAIL: TestCreate/EC-05#' $log"
row E3 "a working full-stack or backend-led flow" \
  "passed 'TestParentJourney'" \
  "sed -i 's#PASS: TestParentJourney#FAIL: TestParentJourney#' $log"
row E4 "sensible tests or verification, run by CI" \
  "has .github/workflows/ci.yml 'go vet' && has .github/workflows/ci.yml 'go test ./... -race' && has .github/workflows/ci.yml 'scripts/mutate.sh'" \
  "sed -i '/go vet/d' .github/workflows/ci.yml"
row E5 "clear scope control" \
  "section README.md '## What I deliberately cut' 200" \
  "sed -i '/^## What I deliberately cut/,/^## /{/^## /!d}' README.md"
row E6 "clear communication: no readme section is empty" \
  "no_empty_section" \
  "sed -i '/^## Assumptions/,/^## /{/^## /!d}' README.md"

# P - the structural writing rules, over the 5 documents
row P1 "no semicolon" "prose semi" \
  "sed -i '3i A short line; and a second clause.' README.md"
row P2 "no compound or perfect tense" "prose tense" \
  "sed -i '3i The last seat has been taken by a payer.' README.md"
row P3 "no descriptive sentence over 25 words" "prose long" \
  "sed -i '3i one two three four five six seven eight nine ten and one two three four five six seven eight nine ten and one two three four.' README.md"
row P4 "no sentence opens with a bare This" "prose this" \
  "sed -i '3i This is the reason the row exists.' README.md"

# ------------------------------------------------------------------- main ----
build_log() {
  ( cd "$root" && go test ./... -v -count=1 > "$log" 2>&1 )
  if ! grep -q -- '--- PASS' "$root/$log"; then
    echo "the test run produced no PASS line, so no row can read it" >&2
    tail -5 "$root/$log" >&2
    exit 1
  fi
}

if [ "$mode" = --prose ]; then
  cd "$root" || exit 1
  for r in semi tense this long; do prose_find "$r"; done
  exit 0
fi

build_log
trap 'rm -f "$root/$log"' EXIT

if [ "$mode" = --honest ]; then
  honest=0
  decoration=0
  for i in "${!ids[@]}"; do
    work=$(mktemp -d)
    cp -r "$root"/. "$work"/ 2>/dev/null
    rm -rf "$work/.git"
    if ( cd "$work" && eval "${breaks[$i]}" ) >/dev/null 2>&1 &&
      ! ( cd "$work" && eval "${checks[$i]}" ) >/dev/null 2>&1; then
      printf 'HONEST     %-4s %s\n' "${ids[$i]}" "${needs[$i]}"
      honest=$((honest + 1))
    else
      printf 'DECORATION %-4s %s\n' "${ids[$i]}" "${needs[$i]}"
      printf '           its break: %s\n' "${breaks[$i]}"
      decoration=$((decoration + 1))
    fi
    rm -rf "$work"
  done
  echo
  echo "${#ids[@]} rows: $honest honest, $decoration decoration"
  [ "$decoration" -eq 0 ]
  exit $?
fi

ok=0
missing=0
for i in "${!ids[@]}"; do
  if ( cd "$root" && eval "${checks[$i]}" ) >/dev/null 2>&1; then
    printf 'OK   %-4s %s\n' "${ids[$i]}" "${needs[$i]}"
    ok=$((ok + 1))
  else
    printf 'MISS %-4s %s\n' "${ids[$i]}" "${needs[$i]}"
    printf '     its check: %s\n' "${checks[$i]}"
    missing=$((missing + 1))
  fi
done
echo
echo "${#ids[@]} rows: $ok ok, $missing missing"
[ "$missing" -eq 0 ]
