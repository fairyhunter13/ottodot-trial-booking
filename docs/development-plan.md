# Development plan

Written before the code. The README describes what shipped. This file describes what was planned and
why. The test plan is in [test-plan.md](test-plan.md).

## Stack

Go 1.26, `net/http`, `html/template`, one dependency: `modernc.org/sqlite` (pure Go, no cgo). So the
whole setup is `go run .`.

```
main.go                        routes and flags
internal/store/store.go        open + schema.sql + seed.sql
internal/booking/booking.go    Create, Pay, Roster
internal/fixture/fixture.go    per-test database builder
internal/web/handlers.go       handlers + embedded templates
internal/web/templates/*.html  4 pages, no CSS, no JS
```

Tables: `parents`, `students`, `trial_classes`, `bookings`, `payment_attempts`.
Statuses: `pending_payment`, `confirmed`, `payment_failed`, `cancelled`.

## The two invariants

Both live in the database, so a bug in a handler cannot break them.

```sql
CREATE UNIQUE INDEX one_active_booking ON bookings(student_id, class_id)
  WHERE status IN ('pending_payment','confirmed');   -- no duplicate

BEGIN IMMEDIATE;                                      -- no overbooking
  SELECT COUNT(*) FROM bookings WHERE class_id=? AND status='confirmed';
  UPDATE bookings SET status='confirmed'
   WHERE id=? AND status='pending_payment';
COMMIT;
```

A pending booking holds no seat. The count runs at confirm time inside the write transaction, and
`BEGIN IMMEDIATE` takes the write lock before the read, so the check and the write are one step.

A `payment_failed` or `cancelled` row falls out of the partial index, so the parent can try again.

## The last-seat race

B confirms first and takes seat 4. A pays after, the count reads 4, and the transaction refuses. A
ends `cancelled` with `reason='seat_taken'`, and the payment row is marked `refund_required`.

The tradeoff: A can be charged and still lose the seat. The alternative is a seat hold with a TTL,
which never double-charges but leaks a seat on an abandoned checkout. That alternative is a "next
step" and not the build.

## Routes and their failure codes

| Route | Success | Refusals |
| --- | --- | --- |
| `GET /?parent=` | page: children, classes, seats left | 404 unknown parent |
| `POST /bookings` | 303 to the booking page | 404 unknown child or class · 403 child is not this parent's · 409 active booking exists · 409 class already full |
| `GET /bookings/{id}` | status page and mock pay form | 404 |
| `POST /bookings/{id}/pay` | 303 back to the booking page, also when the booking is already confirmed | 404 · 409 the booking is `payment_failed` or `cancelled` |
| `GET /classes/{id}/roster` | roster page | 404 |
| `GET /api/classes/{id}/roster` | JSON, confirmed only | 404 |

A second pay submit on a booking that is already confirmed is a no-op and returns the same 303, so a
double-clicked form is not an error the parent has to read. A pay submit on a `payment_failed` or
`cancelled` booking is a 409, because that booking is finished and the parent must book again.

The pay form carries `outcome=success|fail`, so a payment failure is one click. There is no cancel
endpoint: a failed payment is the retry path, and one fewer route is one fewer thing to test.

## Seed data

`c1` free · `c2` 3 confirmed, the race demo · `c3` full · one confirmed booking so the duplicate case
is one click · one `payment_failed` row · one parent with 3 children, so "choose a child" is a real
choice · more than one parent, so the wrong-parent case is reachable.

Every `go run .` rebuilds and reseeds the demo database, so the reviewer sees the same state each
time. `-keep` skips the reset.

## What this plan deliberately cuts

- Regular enrollment. The brief forbids it.
- A refund job, email, and authentication. The refund need is recorded in the data as
  `refund_required`, and nothing acts on it.
- A seat hold with a TTL. Named above as the alternative to the confirm-time count.
- CSS and JavaScript. The brief says frontend polish does not score.

## Timebox

2 hours of build. If the clock runs out, the CI file drops first, then the mutation script becomes a
manual note. The tests and the README never drop, because those two carry the grade.
