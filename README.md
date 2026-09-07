# Ottodot trial booking

One slice of the trial booking flow. A parent picks a child and a trial class, pays a mock payment,
and sees the booking status. A teacher reads the roster. 4 seats per class.

The plans written before the code are in [docs/development-plan.md](docs/development-plan.md) and
[docs/test-plan.md](docs/test-plan.md).

## How to run

```
go run .                       # http://localhost:8080
go run . -addr :9000 -db /tmp/trial.db -keep
```

Go 1.25 or newer. One dependency, `modernc.org/sqlite`, which is pure Go, so there is no cgo and no
database to install. Every start rebuilds and reseeds `data.db`, so the demo state is the same each
time. `-keep` skips the reset.

Verify:

```
go vet ./...
go test ./... -race -count=3
./scripts/mutate.sh
./scripts/brief-check.sh
./scripts/brief-check.sh --honest
```

## What I built

| Route | What it does |
| --- | --- |
| `GET /?parent=` | choose a child and an available trial class |
| `POST /bookings` | submit a booking, which starts as `pending_payment` |
| `GET /bookings/{id}` | the booking status and the mock payment form |
| `POST /bookings/{id}/pay` | record the payment result, then confirm or refuse |
| `GET /classes/{id}/roster` | the teacher roster |
| `GET /api/classes/{id}/roster` | the same roster as JSON |

The payment form has two buttons, "Pay (success)" and "Pay (failure)", so a reviewer reproduces a
payment failure in one click.

## Invariants

Two rules must hold whatever any handler does, so both live in the database and not in Go.

1. **A child holds at most one live booking per class.** A partial unique index enforces it.
2. **A class never holds more confirmed students than its capacity.** A count inside the same write
   transaction that confirms the seat enforces it.

```sql
CREATE UNIQUE INDEX one_active_booking ON bookings(student_id, class_id)
  WHERE status IN ('pending_payment', 'confirmed');
```

A `payment_failed` or `cancelled` row falls out of that index, so a parent can book again after a
failure. The same index catches a repeated form submit. The handler turns it into a 409, and never
a 500.

## Backend design

### Data model

```
parents(id, name, email)
students(id, parent_id, name, grade)
trial_classes(id, subject, starts_at, capacity)
bookings(id, student_id, class_id, status, reason, created_at, updated_at)
payment_attempts(id, booking_id, outcome, amount_cents, refund_required, created_at)
```

Four fields beyond the suggested model, and why each earns its place:

| Field | Why |
| --- | --- |
| `trial_classes.capacity` | the seat count reads a column, so 4 is not hardcoded, and a test runs a class of 2 |
| `trial_classes.starts_at` | the roster has to be right *before class starts*, so a started class refuses a new booking |
| `bookings.reason` | tells a parent that they lost the last seat, apart from a payment that failed |
| `payment_attempts.refund_required` | a parent who pays and loses the seat is owed money, and the debt is recorded even though nothing pays it yet |

### Booking statuses

`pending_payment` → `confirmed` · `payment_failed` · `cancelled`.

A `pending_payment` booking holds **no** seat. Only `confirmed` counts against capacity.

### How duplicates are prevented

The partial unique index above. The rule is stricter than the brief asks. The brief forbids a
duplicate *confirmed* booking. The index also blocks a second *pending* booking for the same child
and class. That kills a real double-submit. It costs a parent the ability to hold two payment
attempts for one class at once, and I took that tradeoff on purpose.

### How payment failure is handled

`Pay` writes the `payment_attempts` row and the booking status in one transaction. A failure sets
`payment_failed`, and the roster query reads `confirmed` only, so the child never appears. The failed
row leaves the unique index, so the parent books the same class again with no cleanup step.

### How two users compete for the last seat

**The approach.** A pending booking holds no seat. The seat is counted at *confirm* time, inside a
`BEGIN IMMEDIATE` transaction, which takes the SQLite write lock before the first read. So the count
and the status change are one step, and a second payer cannot read a stale count.

**Why.** It is the smallest correct answer here. It needs no background job, no expiry sweeper and no
extra table, and the guarantee comes from the database rather than from careful handler code.

**The tradeoffs I accepted.**

- A parent can be charged and still lose the seat. The loser ends `cancelled` with
  `reason = 'seat_taken'`, and the attempt row is flagged `refund_required`. **The refund itself is
  not built.** The debt is recorded, and a real system would drain that flag from a job.
- A seat hold with a TTL never double-charges. But it leaks a seat when a parent abandons a
  checkout, and it needs an expiry job. That is the "next step" and not the build.
- `BEGIN IMMEDIATE` serializes every confirm, for the whole database and not for one class. At this
  size that is free. It is the first thing to change under load.

### Which check belongs where

| Check | Where it lives | Why there |
| --- | --- | --- |
| seats left, full class shown as disabled | UI | a hint only, and it is stale the moment it renders |
| the child belongs to this parent | backend | authorization, and it is not a data shape |
| one live booking per child and class | database | a unique index cannot be raced |
| a class never exceeds its capacity | database transaction | the count and the write must be one step |
| the class has not started | backend | a time comparison, and it is not worth a trigger |
| refunding a `seat_taken` payment | background job | **not built**, and the flag is the queue |

## Tests

Three layers, and the invariants are re-checked after every single test.

| Layer | Package | What it catches |
| --- | --- | --- |
| package tests | `internal/booking` | the rules, and the concurrency |
| endpoint tests | `internal/web` | status codes, templates, the JSON shape |
| one journey test | `internal/web` | the whole demo path over HTTP |

`fixture.AssertInvariants` runs through `t.Cleanup` after every test. It fails a test when any class
is over capacity, or when any child holds two live bookings. The test fails even where it looked at
something else.

Every edge case carries an id, and the id is the subtest name. So the coverage list is the test
output, and not a claim in this file:

```
$ go test ./internal/booking/ -v -run 'TestCreate|TestPayStates'
--- PASS: TestCreate/EC-02_duplicate_against_a_pending_row
--- PASS: TestCreate/EC-05_retry_after_a_lost_seat
--- PASS: TestCreate/EC-09_child_of_another_parent
--- PASS: TestCreate/EC-20_class_has_already_started
--- PASS: TestPayStates/EC-14_success_after_the_seat_is_gone
--- PASS: TestPayStates/EC-16_pay_on_a_cancelled_booking
```

The full list of ids is in [docs/test-plan.md](docs/test-plan.md).

`scripts/brief-check.sh` is a second checker, and it grades this repo against the brief and not
against the code. Each line of the brief is one row. `--honest` breaks each row in a copy of the
tree, and a row that still passes there is decoration.

### Proof that the guards work

A green suite is not proof, because a test that passes against broken code proves nothing.
`scripts/mutate.sh` breaks one guard at a time in a copy of the tree and checks that a named test
notices. Real output:

```
CAUGHT   begin immediate -> begin           by TestConcurrentLastSeat
CAUGHT   drop the unique index              by TestCreate
SURVIVED drop the pending-payment guard     by design, see docs/test-plan.md
CAUGHT   seat count off by one              by TestCapacityNeverExceeded
CAUGHT   confirm always loses the seat      by TestConcurrentDistinctSeats
```

The fourth mutation catches an over-strict guard. A lock that refuses *every* booking passes every
capacity test. So `TestConcurrentDistinctSeats` asserts that two payers for two free seats both win.

The survivor is reported and not hidden. That guard is a second line of defence behind the
transaction. No test can reach past the transaction to see the guard. The reasoning is in
[docs/test-plan.md](docs/test-plan.md).

## Seed data

`go run .` seeds it. `c1` has free seats, `c2` holds exactly 3 confirmed students and is the race
demo, and `c3` is full. One child already holds a seat in `c1`, so the duplicate attempt is one
click. One booking is `payment_failed`. One parent has 3 children, so choosing a child is a real
choice.

`TestSeedMeetsBrief` asserts those cases are really in the seed, so the demo cannot rot silently.

Reproduce the race by hand:

```
go run .
# open http://localhost:8080, book a child into the Science class (c2, 1 seat left)
# open the same class for a second parent in another tab, book, and pay first
# go back to the first tab and pay -> cancelled, seat_taken
curl localhost:8080/api/classes/c2/roster
```

## Assumptions

- No authentication. A link names the parent, because auth is not what this task grades.
- The payment provider is a button. A real one is asynchronous, and the webhook would land where
  `Pay` is now.
- One process, one SQLite file. The invariants move to Postgres unchanged: the partial index is the
  same, and `BEGIN IMMEDIATE` becomes `SELECT ... FOR UPDATE` on the class row.
- Times are stored as SQLite `datetime` text in UTC. No time zone handling.

## What I deliberately cut

- Regular enrollment. The brief forbids it.
- Authentication, email, and a real payment provider.
- The refund job. The need is recorded as `refund_required`, and nothing drains it.
- A cancel endpoint. A failed payment is already the retry path.
- CSS and JavaScript. The brief says frontend polish does not score.

## What I would monitor after release

- **Classes over capacity.** Must be zero. Anything else pages someone.
- **The `seat_taken` rate.** It says how often a parent pays and loses, which is the cost of the
  chosen tradeoff.
- **The age of the `refund_required` backlog.** A refund nobody sent is money owed.
- **The payment failure rate**, split by provider error.
- **Unique index violations.** A rise means the UI is letting parents double-submit.
- **The drop from booking created to booking confirmed**, which finds an abandoned checkout.

## What I would do next

1. A seat hold with a short TTL plus a sweeper, so a parent is never charged for a lost seat.
2. The refund job that drains `refund_required`.
3. Idempotency keys on the payment endpoint, for a real provider that retries a webhook.
4. Postgres, and `SELECT ... FOR UPDATE` on the class row, so confirms serialize per class and not
   per database.
5. Auth, and a real parent session.

## Time spent

About 2 hours of build, plus the reading and the planning before it.
