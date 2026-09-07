# Test plan

The build plan is in [development-plan.md](development-plan.md).

## Edge cases

Each case carries an id. The id is the subtest name, so the test output is the coverage list. No
document has to claim the coverage.

```
go test ./internal/... -v -run 'TestCreate|TestPayStates'
```

### Create

| Id | Case | Expected |
| --- | --- | --- |
| EC-01 | first booking | `pending_payment` |
| EC-02 | duplicate against a pending row | refused |
| EC-03 | duplicate against a confirmed row | refused |
| EC-04 | retry after a failed payment | allowed |
| EC-05 | retry after a lost seat | allowed |
| EC-06 | class is already full | refused early |
| EC-07 | unknown child | 404 |
| EC-08 | unknown class | 404 |
| EC-09 | child of another parent | 403 |
| EC-10 | two identical creates at the same instant | one row, the rest refused as duplicates, never a 500 |
| EC-20 | the class already started | refused |

### Pay

| Id | Case | Expected |
| --- | --- | --- |
| EC-11 | success on the last seat | `confirmed` |
| EC-12 | failure | `payment_failed`, roster untouched, attempt row written |
| EC-13 | the same pay form submitted twice | second submit changes nothing and charges nothing |
| EC-14 | success after the seat is gone | `cancelled`, `seat_taken`, `refund_required` |
| EC-15 | pay on a `payment_failed` row | refused |
| EC-16 | pay on a `cancelled` row | refused |
| EC-17 | unknown booking | 404 |
| EC-18 | many payers, one seat | exactly one winner |
| EC-19 | two pays on the same booking at once | one confirm, one charge |
| EC-21 | a class of capacity 2 | stops at 2, so 4 is not hardcoded |
| EC-22 | one goroutine creates while another confirms the last seat | either order, invariant holds |
| EC-23 | two payers, two free seats | **both** confirm |

An earlier draft of this plan missed `EC-23`. Every capacity test above still passes when the guard
refuses everything. So without `EC-23`, an over-strict lock reads as correct.

### Roster and money

Pending, failed and cancelled rows never appear on a roster. An unknown class returns a 404. A
`payment_failed` row must not carry `refund_required`, and a `seat_taken` row must carry it.

## Three layers, two datasets

### Two datasets, and why they are not one

The demo seed and the test data answer different questions. When one file serves both, a change to
the demo breaks a test that has nothing to do with the demo.

- `seed.sql` is the **demo** dataset. It exists for `go run .` and for the video.
- Each test builds its own data **in Go**, from an empty schema. A test states the state it needs,
  and nothing more.

I do not trust the seed either. `TestSeedMeetsBrief` loads `seed.sql` and asserts that the four
asked cases are really in it:

- a class with free seats
- a class with exactly 3 confirmed students
- a child already booked, so a test can reach a duplicate attempt
- a `payment_failed` row

So a broken demo fails a test, and no other test depends on the demo.

### The fixture builder

`internal/fixture`. The builder is the reason 23 cases do not become 23 setup blocks.

```go
f := fixture.New(t)                    // temp db, schema applied, empty
c := f.Class(4, 3)                     // capacity 4, already holding 3 confirmed students
s := f.Student(f.Parent())
b := f.Booking(s, c, "pending_payment")
```

### The invariant net

`fixture.AssertInvariants` runs after every test through `t.Cleanup`. The helper is two SQL queries:
no class holds more confirmed rows than its capacity, and no child holds two live rows for one
class. A test that breaks an invariant fails, even when that test examined something else. One
helper therefore covers cases nobody wrote a test for.

### Concurrency tests that fail for the right reason

- The DSN carries `busy_timeout(5000)`, `journal_mode(WAL)` and `foreign_keys(1)`. Without the busy
  timeout a second writer gets `SQLITE_BUSY` at once. The test then reports a lock error, and not a
  lost race. `TestSchemaPragmas` asserts these three, so a typo in the DSN fails a test.
- Every goroutine waits on one closed channel, so they all start together and never queue.
- The concurrency tests run under `-race` and `-count=5`, because a race that appears one run in
  three is still a race.

### Layer 1 — package tests (`internal/booking`)

Against a real SQLite file, no HTTP.

| Test | Asserts |
| --- | --- |
| `TestCreate` | table-driven over the create cases, each with its expected error |
| `TestPayStates` | table-driven over the pay cases, each with its end status |
| `TestDoublePayIsNoop` | EC-13: pay twice → one confirm, one seat, no second charge |
| `TestScriptedLastSeat` | the brief's 4 steps in order: B confirms, A gets `seat_taken`, class holds 4 |
| `TestConcurrentLastSeat` | EC-18: 3 confirmed, 16 goroutines pay at once → exactly 1 winner |
| `TestConcurrentSameBooking` | EC-19: 8 goroutines pay one booking → 1 confirm, 1 charge |
| `TestConcurrentDuplicateCreate` | EC-10: 8 goroutines create the same child and class → 1 row |
| `TestCapacityNeverExceeded` | EC-21: 20 children against a class of 2 and of 4 |
| `TestConcurrentDistinctSeats` | EC-23: 2 payers, 2 seats, both confirm |
| `TestConcurrentCreateAndConfirm` | EC-22 |
| `TestSeedMeetsBrief` | the demo dataset really holds the four asked cases |
| `TestSchemaPragmas` | `foreign_keys` on, journal mode WAL |

### Layer 2 — endpoint tests (`internal/web`)

Through `httptest`, over the real router and the real templates. A package test never sees a broken
template or a wrong status code, and a reviewer meets both.

| Test | Asserts |
| --- | --- |
| `TestRoutesStatusCodes` | table-driven: every row of the route table, its code and its body text |
| `TestEveryTemplateRenders` | each page returns 200 and holds a known string, so no template panics |
| `TestRosterJSON` | exact JSON shape, confirmed children only |
| `TestHTTPLastSeatRace` | the brief's scenario through two HTTP clients |

### Layer 3 — one integration test

`TestParentJourney` walks the demo path over HTTP. It opens the page, books a seat, and pays with a
failure. It then books again, pays with a success, reads the roster, and finds the child. The test
walks the same path as the video, so a green test proves the demo works.

## Proof that a guard holds, and not only that a test passes

A green suite is not proof. A test that passes against broken code proves nothing, so each guard
carries a mutation that must break a named test.

`scripts/mutate.sh` copies the tree to a temp directory. It applies one edit, runs the suite, and
records which tests failed. It then deletes the copy. The script never edits the working tree.

| Mutation | The guard it removes | Result |
| --- | --- | --- |
| `BEGIN IMMEDIATE` → `BEGIN` | atomic count and write | caught by `TestConcurrentLastSeat` |
| drop `one_active_booking` | duplicate prevention | caught by `TestCreate` |
| `count < capacity` → `count <= capacity` | the seat count itself | caught by `TestCapacityNeverExceeded` |
| confirm always returns `seat_taken` | the happy path | caught by `TestConcurrentDistinctSeats` |
| drop `AND status='pending_payment'` | second confirm of one booking | **survives, by design** |

The confirm-always-fails mutation catches an over-strict guard. A lock that refuses every booking
satisfies every capacity test. So the suite must also fail when the code refuses too much.

The status-guard mutation survives, and the script reports it rather than hiding it. `Pay` reads the
booking status inside the same `BEGIN IMMEDIATE` transaction that later writes it, and that
transaction holds the write lock for its whole life. So the read and the write are already one step.
The status guard in the `UPDATE` is a second guard behind a stronger first one. No test can see
behind the first guard to reach the second, so I wrote no test that only pretends to. The guard
stays, because it keeps `settle` safe for a caller outside that transaction.

Any other mutation that the suite survives is a hole in the tests, and not a pass. When one appears,
I write the missing test first. I then repeat the run.

## The fix loop

Fix a failing case in the one layer the layer table names, and never in two layers at once. The
order is fixed:

1. Write the failing test.
2. Fix the code.
3. Run `go test ./... -race`.
4. Re-run `scripts/mutate.sh` in full, because a fix in one place often weakens a guard somewhere
   else.

## The brief checker

`scripts/mutate.sh` asks whether a test holds its guard. `scripts/brief-check.sh` asks a different
question: does the repo still answer each line of the brief?

Every line of the brief is one row. A row names a check, and it names the break that must make that
check fail. `./scripts/brief-check.sh` runs every check against the real tree. `--honest` copies the
tree, applies one row's break inside the copy, and runs that row's check again there. A row that
still passes is decoration, and the run fails.

A row reads a file or the test log, and never a claim. When a row covers an edge case, it asserts
that the named subtest ran and passed. So the checker proves presence and wiring. The checker does not grade
the quality of the prose behind a heading.

Commands: `go vet ./...`, `go test ./... -race -count=1`, `scripts/mutate.sh`,
`scripts/brief-check.sh` and `scripts/brief-check.sh --honest`. GitHub Actions runs all five on
push, so the result does not depend on one machine.
