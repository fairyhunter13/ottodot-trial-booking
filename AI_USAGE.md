# AI usage

## Which tools I used

Claude Code (Claude Opus) in a terminal, for the whole task. I used it to read the brief, to plan,
and to write the Go code, the tests and these documents.

## What I used it for

- I read the brief with it. I checked every section for a line I missed.
- I wrote the plan first, and only then the code. The plan is in `docs/`.
- The schema, the booking rules, the handlers, the templates, the test fixture, and the tests.
- The mutation script, which checks that each test really catches a broken guard.

## One place where AI moved me faster

The test fixture and the table-driven tests. There are 23 numbered edge cases in
`docs/test-plan.md`. By hand, each case needs its own setup block. The model built
`internal/fixture` first, so each case became one table row and one line of setup. I could then read
the whole list on one screen, and I could see what was missing.

The same is true of `fixture.AssertInvariants`. It runs after every test through `t.Cleanup`, so a
test that breaks a database rule fails even when that test examined something else. One helper
therefore covers cases nobody wrote a test for.

## One place where I disagreed with, corrected, or rejected the output

There were several, and they are the reason the tests look the way they do.

1. **The first plan held unit tests only.** I asked where the integration tests and the endpoint
   tests were. The three layers exist because of that question. A package test never sees a broken
   template or a wrong status code, and a reviewer meets both.
2. **A list of test names is not proof.** I asked how a case gets configured, tested, fixed, and
   *proved*. A green suite proves nothing on its own, because a test that passes against broken code
   is worthless. That question produced `scripts/mutate.sh`. The script breaks one guard at a time,
   and it checks that a named test notices.
3. **The capacity tests were all one-sided.** I asked about the edge cases again. The audit then
   found that a guard which refuses *every* booking still passes every capacity test. `EC-23` and a
   fifth mutation answer that: two payers, two free seats, and both must win.
4. **The plan was not in the repo.** The model kept the development plan and the test plan in a
   private scratch file. I asked why, because the brief grades the explanation. They are now
   `docs/development-plan.md` and `docs/test-plan.md`.
5. **I almost hid a mutation that survived.** One mutation survives, because the guard it removes
   sits behind a stronger one. The honest answer was to report it and say why. The dishonest answer
   was a test that only pretends to cover it. The README and `docs/test-plan.md` both report it.

## What I would change about my AI workflow

I would ask "how will you *prove* this works" in the first prompt, and not in the fifth. Every real
improvement in this repo came from that question. The early question saves two rounds of planning.

I would also ask for the plan as a repo file from the start. A scratch file made the plan invisible
to me and to the reviewer.

## How I verified the final implementation

- `go vet ./...`
- `go test ./... -race -count=3`, so the suite still catches a race that appears one run in three.
- `./scripts/mutate.sh`, which breaks five guards one at a time and reports which test caught each.
- I ran `go run .`. I then walked the demo by hand: book, pay with a failure, book again, pay with
  a success, and read the roster JSON.
