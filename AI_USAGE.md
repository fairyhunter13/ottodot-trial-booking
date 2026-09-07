# AI usage

## Which tools I used

Claude Code (Claude Opus) in a terminal, for the whole task: reading the brief, the planning, the Go
code, the tests, and these documents.

## What I used it for

- Reading the brief and checking that nothing in it was missed, section by section.
- Writing the plan first, and only then the code. The plan is in `docs/`.
- The schema, the booking rules, the handlers, the templates, the test fixture, and the tests.
- The mutation script that checks the tests actually bite.

## One place where AI moved me faster

The test fixture and the table-driven tests. There are 23 numbered edge cases in
`docs/test-plan.md`. Written by hand each one would have been its own setup block. The model built
`internal/fixture` first, so each case became one table row and one line of setup, and I could read
the whole list on one screen and see what was missing.

The same is true of `fixture.AssertInvariants`. It runs after every test through `t.Cleanup`, so a
test that breaks a database rule fails even where that test was looking at something else. That is
one helper covering cases nobody wrote a test for.

## One place where I disagreed with, corrected, or rejected the output

Several, and they are the reason the tests look the way they do.

1. **The first plan was unit tests only.** I asked where the integration tests and the endpoint tests
   were. That is how the three layers exist: a package test never sees a broken template or a wrong
   status code, and a reviewer would hit both.
2. **A list of test names is not proof.** I asked how a case gets configured, tested, fixed, and
   *proved*. A green suite proves nothing on its own, because a test that passes against broken code
   is worthless. That question produced `scripts/mutate.sh`, which breaks one guard at a time and
   checks a named test notices.
3. **The capacity tests were all one-sided.** When I pushed on the edge cases again, the audit found
   that a guard which refuses *every* booking would pass every capacity test written at that point.
   `EC-23` and a fifth mutation were added for that: two payers, two free seats, and both must win.
4. **The plan was not in the repo.** The model kept the development plan and the test plan in a
   private scratch file. I asked why, since the brief grades the explanation. They are now
   `docs/development-plan.md` and `docs/test-plan.md`.
5. **A survived mutation was nearly papered over.** One mutation survives, because the guard it
   removes sits behind a stronger one. The honest answer was to report it and say why, not to write a
   test that pretends to cover it. It is reported in the README and in `docs/test-plan.md`.

## What I would change about my AI workflow

I would ask "how will you *prove* this works" in the first prompt rather than the fifth. Every real
improvement in this repo came from that question, and asking it early would have saved two rounds of
planning.

I would also ask for the plan as a repo file from the start. Keeping it in a scratch file made it
invisible to me and to the reviewer.

## How I verified the final implementation

- `go vet ./...`
- `go test ./... -race -count=3`, so a race that appears one run in three is still caught.
- `./scripts/mutate.sh`, which breaks five guards one at a time and reports which test caught each.
- Running `go run .` and walking the demo by hand: book, pay with failure, book again, pay with
  success, then read the roster JSON.
