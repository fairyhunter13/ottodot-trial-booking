# Ottodot trial booking

One slice of the trial booking flow. A parent picks a child and a trial class, pays a mock payment,
and sees the booking status. A teacher reads the roster. 4 seats per class.

I wrote the plans before the code. They are in [docs/development-plan.md](docs/development-plan.md)
and [docs/test-plan.md](docs/test-plan.md).

## How to run

```
go run .                       # http://localhost:8080
go run . -addr :9000 -db /tmp/trial.db -keep
```

You need Go 1.25 or newer. There is one dependency, `modernc.org/sqlite`. That driver is pure Go, so you need
no C compiler and no database server. Every start rebuilds and reseeds `data.db`, so the demo shows
the same state each time. The `-keep` flag skips that reset.

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

The payment form has two buttons, "Pay (success)" and "Pay (failure)". So a reviewer can make a
payment fail with one click.

## Invariants

Two rules must hold whatever any handler does. Both rules live in the database, and not in Go.

1. **A child holds at most one live booking per class.** A partial unique index enforces it.
2. **A class never holds more confirmed students than its capacity.** The code counts the confirmed
   seats inside the same write transaction that confirms the new seat.

```sql
CREATE UNIQUE INDEX one_active_booking ON bookings(student_id, class_id)
  WHERE status IN ('pending_payment', 'confirmed');
```

A `payment_failed` or `cancelled` row leaves that index, so a parent can book again after a failure.
The same index catches a repeated form submit. The handler turns that error into a 409, and never
into a 500.

## Backend design

### Data model

```
parents(id, name, email)
students(id, parent_id, name, grade)
trial_classes(id, subject, starts_at, capacity)
bookings(id, student_id, class_id, status, reason, created_at, updated_at)
payment_attempts(id, booking_id, outcome, amount_cents, refund_required, created_at)
```

I added four fields beyond the suggested model. Here is why each one is there:

| Field | Why |
| --- | --- |
| `trial_classes.capacity` | the seat count reads a column, so 4 is not hardcoded, and a test runs a class of 2 |
| `trial_classes.starts_at` | the roster must be right *before class starts*, so a class that already began refuses a new booking |
| `bookings.reason` | tells a parent that they lost the last seat, apart from a payment that failed |
| `payment_attempts.refund_required` | the school owes money to a parent who pays and loses the seat. The flag records that debt, and nothing pays it yet |

### Booking statuses

`pending_payment` → `confirmed` · `payment_failed` · `cancelled`.

A `pending_payment` booking holds **no** seat. Only a `confirmed` booking counts against capacity.

### How I stop a duplicate

The partial unique index above stops it. The rule is stricter than the brief asks. The brief forbids
a duplicate *confirmed* booking. The index also blocks a second *pending* booking for the same child
and class. The index therefore stops a real double-submit. The cost: a parent cannot hold two
payment attempts for one class at the same time. I accepted that tradeoff on purpose.

### How I handle a failed payment

`Pay` writes the `payment_attempts` row and the booking status in one transaction. A failure sets
the status to `payment_failed`. The roster query reads `confirmed` rows only, so the child never
appears on the roster. The failed row leaves the unique index, so the parent books the same class
again with no cleanup step.

### How two users compete for the last seat

**The approach.** A pending booking holds no seat. The code counts the seat at *confirm* time,
inside a `BEGIN IMMEDIATE` transaction. `BEGIN IMMEDIATE` takes the SQLite write lock before the
first read. So the count and the status change are one step, and a second payer cannot read a stale
count.

**Why.** The confirm-time count is the smallest correct answer here. The design needs no background
job, no expiry job and no extra table. The guarantee comes from the database, and not from careful
handler code.

**The tradeoffs I accepted.**

- A parent can pay and still lose the seat. The loser ends `cancelled` with
  `reason = 'seat_taken'`, and `Pay` flags the attempt row `refund_required`. **I did not build the
  refund.** The database records the debt. A real system would read that flag from a background job
  and send the money.
- A seat hold with a time limit never charges twice. But it wastes a seat when a parent leaves the
  checkout, and it needs a job to expire the hold. That design is the "next step", and not the
  build.
- `BEGIN IMMEDIATE` serializes every confirm across the whole database, and not per class. At this
  size that cost is free. That lock is the first thing to change under load.

### Which check belongs where

| Check | Where it lives | Why there |
| --- | --- | --- |
| seats left, and a full class shown as disabled | UI | a hint only, and it goes stale the moment it renders |
| the child belongs to this parent | backend | an authorization rule, and not a data shape |
| one live booking per child and class | database | nothing can race a unique index |
| a class never exceeds its capacity | database transaction | the count and the write must be one step |
| the class start time is in the future | backend | a time comparison, and it is not worth a trigger |
| the refund for a `seat_taken` payment | background job | **not built**, and the flag is the work queue |

## Tests

Three layers, and every test re-checks both invariants when it ends.

| Layer | Package | What it catches |
| --- | --- | --- |
| package tests | `internal/booking` | the rules, and the concurrency |
| endpoint tests | `internal/web` | status codes, templates, the JSON shape |
| one journey test | `internal/web` | the whole demo path over HTTP |

`fixture.AssertInvariants` runs through `t.Cleanup` after every test. The helper fails a test when
any class holds more confirmed students than its capacity, or when any child holds two live
bookings. The test fails even when it examined something else.

Every edge case carries an id, and the id is the subtest name. So the test output is the coverage
list, and not a claim in this file:

```
$ go test ./internal/booking/ -v -run 'TestCreate|TestPayStates'
--- PASS: TestCreate/EC-02_duplicate_against_a_pending_row
--- PASS: TestCreate/EC-05_retry_after_a_lost_seat
--- PASS: TestCreate/EC-09_child_of_another_parent
--- PASS: TestCreate/EC-20_the_class_already_started
--- PASS: TestPayStates/EC-14_success_after_the_seat_is_gone
--- PASS: TestPayStates/EC-16_pay_on_a_cancelled_booking
```

[docs/test-plan.md](docs/test-plan.md) holds the full list of ids.

`scripts/brief-check.sh` is a second checker. It grades this repo against the brief, and not against
the code. Each line of the brief is one row. `--honest` breaks each row inside a copy of the tree. A
row that still passes there is decoration.

### Proof that the guards work

A green suite is not proof, because a test that passes against broken code proves nothing.
`scripts/mutate.sh` breaks one guard at a time inside a copy of the tree. It then checks that a
named test notices. Real output:

```
CAUGHT   begin immediate -> begin           by TestConcurrentLastSeat
CAUGHT   drop the unique index              by TestCreate
SURVIVED drop the pending-payment guard     by design, see docs/test-plan.md
CAUGHT   seat count off by one              by TestCapacityNeverExceeded
CAUGHT   confirm always loses the seat      by TestConcurrentDistinctSeats
```

The confirm-always-loses mutation catches an over-strict guard. A lock that refuses *every* booking
passes every capacity test. So `TestConcurrentDistinctSeats` asserts that two payers for two free
seats both win.

The script reports the survivor, and hides nothing. The surviving guard sits behind the transaction,
which is the stronger guard. No test can see behind the transaction to reach it.
[docs/test-plan.md](docs/test-plan.md) holds the reasoning.

## Seed data

`go run .` writes the seed. `c1` has free seats. `c2` holds exactly 3 confirmed students, and it is
the race demo. `c3` is full. One child already holds a seat in `c1`, so the duplicate attempt is one
click. One booking sits at `payment_failed`. One parent has 3 children, so the parent must really
choose between children.

`TestSeedMeetsBrief` asserts that those cases are really in the seed, so a broken demo fails a test.

Reproduce the race by hand:

```
go run .
# open http://localhost:8080, book a child into the Science class (c2, 1 seat left)
# open the same class for a second parent in another tab, book, and pay first
# go back to the first tab and pay -> cancelled, seat_taken
curl localhost:8080/api/classes/c2/roster
```

## Assumptions

- There is no authentication. A link names the parent, because this task does not grade auth.
- The payment provider is a button. A real provider works asynchronously, and its webhook would
  arrive where `Pay` runs now.
- One process, one SQLite file. Both invariants move to Postgres unchanged. The partial index is the
  same, and `BEGIN IMMEDIATE` becomes `SELECT ... FOR UPDATE` on the class row.
- The code stores every time as SQLite `datetime` text in UTC. The code does not handle time zones.

## What I deliberately cut

- Regular enrollment. The brief forbids it.
- Authentication, email, and a real payment provider.
- The refund job. The data records the need as `refund_required`, and nothing reads it yet.
- A cancel endpoint. A failed payment is already the retry path.
- CSS and JavaScript. The brief says frontend polish does not score.

## What I would monitor after release

- **Classes over capacity.** The count must be zero. Any other number must raise an alert.
- **The `seat_taken` rate.** The rate shows how often a parent pays and loses, which is the cost of
  the tradeoff I chose.
- **The age of the `refund_required` backlog.** A refund nobody sent is money the school owes.
- **The payment failure rate**, split by provider error.
- **Unique index violations.** A rise means the UI lets a parent submit twice.
- **The drop from booking created to booking confirmed.** A rise there finds an abandoned checkout.

## What I would do next

1. A seat hold with a short time limit, plus a job that expires it. Then a parent never pays for a
   lost seat.
2. The refund job that reads `refund_required` and sends the money.
3. Idempotency keys on the payment endpoint, for a real provider that retries a webhook.
4. Postgres, and `SELECT ... FOR UPDATE` on the class row. Then confirms serialize per class, and
   not per database.
5. Auth, and a real parent session.

## Time spent

About 2 hours of build, plus the time I spent to read the brief and write the plans.
