# Development plan

I wrote this file before the code. The README describes what shipped. This file describes the plan
and the reasons behind it. The test plan is in [test-plan.md](test-plan.md).

## Stack

Go 1.25, `net/http`, `html/template`, and one dependency: `modernc.org/sqlite`. That driver is pure
Go, so you need no C compiler. The whole setup is therefore `go run .`.

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

Both rules live in the database, so a bug in a handler cannot break them.

```sql
CREATE UNIQUE INDEX one_active_booking ON bookings(student_id, class_id)
  WHERE status IN ('pending_payment','confirmed');   -- no duplicate

BEGIN IMMEDIATE;                                      -- no overbooking
  SELECT COUNT(*) FROM bookings WHERE class_id=? AND status='confirmed';
  UPDATE bookings SET status='confirmed'
   WHERE id=? AND status='pending_payment';
COMMIT;
```

A pending booking holds no seat. The count runs at confirm time, inside the write transaction.
`BEGIN IMMEDIATE` takes the write lock before the read, so the check and the write are one step.

A `payment_failed` or `cancelled` row leaves the partial index, so the parent can try again.

## The last-seat race

B confirms first and takes seat 4. A pays after B. The count then reads 4, and the transaction
refuses. A ends `cancelled` with `reason='seat_taken'`, and `Pay` marks the payment row
`refund_required`.

The tradeoff: A can pay and still lose the seat. The alternative is a seat hold with a time limit.
That hold never charges twice, but it wastes a seat when a parent leaves the checkout. The hold is a
"next step", and not the build.

## Routes and their failure codes

| Route | Success | Refusals |
| --- | --- | --- |
| `GET /?parent=` | page: children, classes, seats left | 404 unknown parent |
| `POST /bookings` | 303 to the booking page | 404 unknown child or class · 403 child is not this parent's · 409 active booking exists · 409 class already full |
| `GET /bookings/{id}` | status page and mock pay form | 404 |
| `POST /bookings/{id}/pay` | 303 back to the booking page, even on a confirmed booking | 404 · 409 the booking is `payment_failed` or `cancelled` |
| `GET /classes/{id}/roster` | roster page | 404 |
| `GET /api/classes/{id}/roster` | JSON, confirmed only | 404 |

A second pay submit on a confirmed booking changes nothing, and it returns the same 303. So a
double-clicked form is not an error the parent must read. A pay submit on a `payment_failed` or
`cancelled` booking returns a 409, because that booking is finished. The parent must book again.

The pay form carries `outcome=success|fail`, so a payment failure takes one click. There is no
cancel endpoint. A failed payment is the retry path, and one route fewer is one thing fewer to test.

## Seed data

- `c1` has free seats.
- `c2` holds 3 confirmed students, and it is the race demo.
- `c3` is full.
- One booking is already confirmed, so the duplicate case takes one click.
- One booking sits at `payment_failed`.
- One parent has 3 children, so "choose a child" is a real choice.
- More than one parent exists, so a test can reach the wrong-parent case.

Every `go run .` rebuilds and reseeds the demo database, so the reviewer sees the same state each
time. The `-keep` flag skips that reset.

## What this plan deliberately cuts

- Regular enrollment. The brief forbids it.
- A refund job, email, and authentication. The data records the refund need as `refund_required`,
  and nothing acts on it.
- A seat hold with a time limit. The section above names it as the alternative to the confirm-time
  count.
- CSS and JavaScript. The brief says frontend polish does not score.

## Timebox

2 hours of build. If I run out of time, the CI file drops first. The mutation script then becomes a
manual note. The tests and the README never drop, because those two carry the grade.
