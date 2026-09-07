# Video script — 5 to 8 minutes

Three scenes, because the brief asks for three things: the solution running, the last-seat race, and
the tradeoffs. Times are a target, not a rule.

Before recording: `go run .` in one terminal, two browser windows side by side, and one terminal for
`curl` and `go test`.

## Scene 1 — the solution running (about 2 minutes)

1. Open `http://localhost:8080`. Say: 4 seats per class, a parent picks a child and a class.
2. Point at the class list. `c1` has seats, `c2` has one seat left, `c3` shows as full and its radio
   is disabled.
3. Book a child into `c1`. The status page says `pending_payment`. Say: **a pending booking holds no
   seat**, and that is the whole design.
4. Click "Pay (failure)". The status is `payment_failed`, and the page says the child is not on the
   roster.
5. Open `/classes/c1/roster` and show the child is absent.
6. Book the same child into the same class again. Say: the failed row leaves the unique index, so a
   retry needs no cleanup.
7. Click "Pay (success)". The status is `confirmed`, and now the child is on the roster.
8. Show `curl localhost:8080/api/classes/c1/roster`.

## Scene 2 — the last-seat race (about 2 minutes 30)

1. Open two browser windows. Left is parent A, right is parent B. Use `c2`, which has one seat left.
2. Both book. Both see `pending_payment`. Say: no seat is held yet, so nobody has won.
3. B pays first, and B is `confirmed`.
4. A pays second. A is `cancelled` with `seat_taken`, and the page says the payment is flagged for a
   refund.
5. Show the roster: 4 confirmed, and A is not on it.
6. Switch to the code. Show `Pay` in `internal/booking/booking.go`: `BEGIN IMMEDIATE`, then the
   count, then the update, all in one transaction. Say: the write lock is taken before the first
   read, so the count cannot be stale.
7. Run `go test ./internal/booking/ -run TestConcurrentLastSeat -race -count=3`. Say: 16 goroutines
   pay at once, and exactly one wins.
8. Run `./scripts/mutate.sh`. Say: this breaks each guard on purpose and checks a test notices. Point
   at `SURVIVED`, and explain that guard sits behind a stronger one, so it is reported and not
   hidden.

## Scene 3 — tradeoffs and what I cut (about 2 minutes)

1. **The tradeoff I took.** A parent can pay and still lose the seat. The loser is `cancelled` with
   `seat_taken`, and the payment row carries `refund_required`. The debt is recorded, and the refund
   job is not built.
2. **The alternative.** A seat hold with a TTL never double-charges, but it leaks a seat on an
   abandoned checkout and needs an expiry job. It is on the "next" list, not in the build.
3. **Where each check lives.** Seats-left is a UI hint. The child-belongs-to-parent check is backend.
   The one-booking rule is a database index. The capacity rule is a database transaction. Show the
   table in the README.
4. **What I cut.** Regular enrollment, auth, email, a real payment provider, the refund job, CSS.
5. **What is next.** The seat hold, the refund job, idempotency keys, and Postgres with
   `SELECT ... FOR UPDATE` on the class row, so confirms serialize per class and not per database.
6. Close on `docs/development-plan.md` and `docs/test-plan.md`. Say: these were written before the
   code.
