# Test Design

How to choose *which* tests to write and *what* each one asserts. A suite is not measured by its size — it is measured by the bugs it catches and the confidence it earns when green. **A test that passes whether or not the code is correct is worse than no test: it costs maintenance and pays false confidence.** Every case below earns its place by pinning a distinct behavior a real defect could break, and every assertion states the contract, not the implementation.

This is the case-design and quality half of the skill. For where tests sit in the review/verify loop, see `../SKILL.md`; for scaling a large suite-authoring or backfill job across agents, the barrier and verification discipline is the same one in `../../loop-engine/references/harness-policy.md` (H2 earned barrier, H4 adversarial verify).

## 1. Case design: partition the input space, then attack the edges

**Test one representative per equivalence class, then every boundary between classes — not a hundred points from the middle of one class.** Inputs the code treats identically form a class; a case from the middle of a class tells you nothing a sibling case didn't. The bugs live at the edges where one behavior hands off to another.

Work each unit through these lenses in order:

| Lens | What it generates | Example: `chargeCard(amount)` valid for `1..=10000` |
|---|---|---|
| **Equivalence partitioning** | one case per class the code handles the same way | below range, in range, above range → 3 classes, 1 case each |
| **Boundary-value analysis** | the values *at and adjacent to* each class edge (off-by-one hunt) | `0, 1, 2` and `9999, 10000, 10001` — the `<` vs `<=` bugs live here |
| **Special / degenerate inputs** | empty, null/undefined/NaN, zero, negative, single-element, max/overflow, duplicate, unicode/empty string | `amount = 0`, `null`, `NaN`, `Number.MAX_SAFE_INTEGER`, `-0.01` |
| **Error / exception paths** | every way the unit is *documented or coded* to fail | out of range → throws `RangeError`; declined card → returns failure result |
| **State transitions** | for stateful units, each legal transition **and** the illegal ones the code must reject | `open → charge` ok; `refunded → charge` must reject; double-close idempotent |

The highest-yield cases are boundaries and error paths — that is where `<`/`<=`, off-by-one, empty-collection, and unhandled-variant defects concentrate (the same correctness traps `../../loop-review/references/quality-checks.md` Tier 1 hunts). For error paths, assert the **specific** error type and message, not merely that *something* threw — a test that accepts any exception passes when the code throws the wrong one. Do not stop at the happy path; a suite that only proves the unit works on the easy input proves almost nothing.

**Property-based testing** where it fits: when a unit has an *invariant* that must hold for all inputs — round-trip (`decode(encode(x)) == x`), idempotence (`f(f(x)) == f(x)`), commutativity, ordering preserved, output always sorted/within bounds, conservation (count in == count out), or an oracle (the fast impl agrees with a slow obvious one) — assert the property over generated inputs (fast-check, Hypothesis, jqwik, proptest, PropCheck) instead of hand-picking three examples. It finds the boundary you didn't think of and shrinks it to a minimal reproducer. Reach for it on parsers, serializers, encoders, data structures, and pure transforms; skip it where a handful of explicit examples fully characterize the behavior. **When a property test fails, add the shrunk counterexample as an explicit regression case** so the exact input stays pinned even if generation changes.

## 2. Test doubles: the taxonomy, and when each is honest

**Replace a real collaborator with a double only to gain determinism or reach an otherwise-unreachable path — never merely to avoid understanding the collaborator.** The five kinds are not interchangeable; picking the wrong one is how tests come to assert implementation.

| Double | What it is | Use it when | Asserts on |
|---|---|---|---|
| **Dummy** | a value passed only to fill a signature, never used | a required arg irrelevant to this test | nothing |
| **Stub** | returns canned answers to calls | you need to *drive* the code down a path (make the API return 404, the DB return empty) | state / return value (indirect input) |
| **Spy** | a real-ish object that records how it was called | you must verify a real side effect with no return value (an email was sent) | recorded calls, checked after |
| **Mock** | pre-programmed with expectations that fail the test if unmet exactly | the *interaction itself* is the contract (the retry policy calls the gateway exactly 3 times) | calls, verified as they happen |
| **Fake** | a working lightweight implementation | you need real behavior without the real cost (in-memory repo, SQLite for Postgres, a fake clock) | state, via the fake's real logic |

**Prefer a fake or a stub over a mock.** A mock encodes *how* the code calls its collaborator; the moment you refactor the call pattern without changing behavior, the mock fails — a false alarm that trains people to ignore tests. Reserve mock-with-expectations for the rare case where the interaction *is* the behavior (call counts in a retry/rate-limit policy, ordering in a two-phase protocol). Assert on outcomes, not on call-counts, unless the outbound effect is itself the contract.

**The boundary rule: mock only at trust boundaries, never internal collaborators.** Legitimate seams to double are the ones you don't own and that make tests slow or nondeterministic:

- **Network** — HTTP APIs, third-party services, message brokers.
- **Clock / time** — `now()`, timers, timezones. Inject a fake clock; never sleep in a test.
- **Filesystem** — real files where a temp dir or in-memory FS suffices.
- **Randomness** — seed it or inject the RNG so runs are reproducible.
- **Other processes** — subprocesses, external DBs (a fake or disposable container often beats a mock).

Inject these seams (pass the clock, the RNG, the HTTP client as arguments) rather than patching internals. Doubling an *internal* collaborator — one class in your own module standing in for another — welds the test to your current decomposition and tests the mock, not the code. Test the two together through the public surface instead; if that is too painful, the design (not the test) is the problem, and the pain is the signal (`../../loop-review/references/quality-checks.md` → *Untestable shape*). Over-mocking is the most common way a test couples to implementation and stops catching real regressions.

## 3. What makes a test good

Every test should hold all six. When one fails, fix the test before trusting the suite.

1. **Tests behavior / contract, not implementation.** Assert on the observable result and public API — the return value, the emitted event, the persisted state — not on private methods, internal call sequences, or intermediate data shapes. The refactor test: *could a correct rewrite of the internals break this test?* If yes, it is coupled to implementation and will cry wolf.
2. **Deterministic.** Same input, same result, every run, on every machine. No dependence on wall-clock time, hash-map ordering, network, locale, or `Math.random()`. A flaky test is discarded by the team and takes the suite's credibility with it.
3. **Fast.** Milliseconds for a unit test. Slow suites get run less, so bugs are found later. Push slow dependencies behind fakes; keep the genuinely-slow integration tests in a separate, deliberately-run tier.
4. **Isolated.** No shared mutable state, no order dependency, no leftover rows or files. Each test sets up and tears down its own world and passes when run alone or in any order. Order-dependence is a latent flake.
5. **One reason to fail.** A test targets a single behavior, so a failure names the broken thing. Not one assertion literally, but one *concept* — five assertions all pinning "the parsed user object" is one reason; asserting parsing *and* persistence *and* notification in one test is three, and a failure won't say which. If the name needs "and", split it. Parameterize (table-driven / `pytest.mark.parametrize` / `it.each`) when the *same* behavior is checked across many inputs — one behavior, many data points, still one logical test.
6. **Readable — Arrange, Act, Assert.** Structure every test in three visible beats: set up inputs and doubles (**Arrange**), invoke the unit once (**Act**), check the outcome (**Assert**). The test is executable documentation of the contract; a reader should learn what the code promises without reading the code. Name it for the behavior (`rejects_charge_when_card_expired`), not the method (`test_charge_2`).

## 4. What NOT to test

Coverage of these adds maintenance and subtracts nothing real. Skipping them is discipline, not laziness.

- **Framework / language / library internals.** You are not testing that `Array.map`, the ORM, or the router works — assume your dependencies are tested. Test *your* use of them only where you add logic on top.
- **Trivial getters/setters and pass-through code** with no branching or transformation. A test that asserts `getName()` returns the name it was given tests nothing.
- **Constants and configuration** with no behavior — a test that hard-codes the same value the code does just breaks in lockstep and catches no bug.
- **Generated code and pure declarations** (DTOs, plain type definitions) — no logic, no test.
- **Private methods directly.** Test them through the public method that calls them; a private method with no public path is dead code (`../../loop-review/references/quality-checks.md` → *Dead code*).

The line: if you cannot state a wrong input that a real bug would make the code produce, there is no behavior to pin. This mirrors the reporting bar everywhere in this repo — no concrete failure, no finding, no test.

## 5. Coverage: a floor, never a target

**Coverage tells you what code *ran*, never what behavior was *verified* — 100% coverage with weak assertions catches nothing.** Use it to find untested code, not to certify tested code.

| Metric | Measures | Blind to |
|---|---|---|
| **Line** | each line executed at least once | whether the *outcome* was asserted; which branch of a line ran |
| **Branch** | each `if`/`else`, `case`, `&&`/`\|\|` side taken | *combinations* of branches; the boundary values within a branch |
| **Path** | each route through the control-flow graph | still nothing about assertion quality; explodes combinatorially, rarely 100% achievable |

Branch coverage is the useful working metric — line coverage happily reports 100% while the `else` you never wrote a test for is the one that ships the bug. But all three share one blindness: **they measure execution, not assertion.** A test that calls the function and asserts nothing (or `expect(fn).not.toThrow()` as its only check) moves the number without adding safety (`../../loop-review/references/quality-checks.md` → *Assertion gaps*). Treat a coverage *drop* on changed lines as a signal to write a test; never treat a coverage *number* as proof the code is tested, and never write an assertion-free test to hit a threshold.

## 6. Good vs bad, concretely

**Coupled to implementation → tests behavior.**

```js
// BAD — asserts HOW the total is computed; a correct refactor breaks it.
const spy = jest.spyOn(cart, "_applyDiscount");
cart.checkout();
expect(spy).toHaveBeenCalledTimes(2);   // fails the day you fold two discounts into one pass

// GOOD — asserts WHAT checkout produces; survives any correct internals.
const result = cart.checkout();
expect(result.total).toBe(1710);        // 1900 with two 10%-off items applied
```

**Nondeterministic → seeded and injected.**

```py
# BAD — depends on the real clock; passes today, flakes at year-end, unreproducible.
token = make_token()
assert token.expires_at > datetime.now()

# GOOD — inject the clock; the expiry contract is now pinned exactly.
clock = FakeClock(at="2026-01-01T00:00:00Z")
token = make_token(clock=clock, ttl=timedelta(hours=1))
assert token.expires_at == datetime(2026, 1, 1, 1, 0, tzinfo=UTC)
```

**Happy-path only → boundaries and errors covered.**

```js
// BAD — one middle-of-the-class case; proves nothing about the edges where bugs live.
test("charge works", () => expect(charge(50).ok).toBe(true));

// GOOD — partition + boundaries + specific error, each a separate reason to fail (§1).
test("rejects amount below minimum", () => expect(() => charge(0)).toThrow(RangeError));
test("accepts the minimum boundary", () => expect(charge(1).ok).toBe(true));
test("accepts the maximum boundary", () => expect(charge(10000).ok).toBe(true));
test("rejects one over the maximum", () => expect(() => charge(10001)).toThrow(RangeError));
test("surfaces a declined card",     () => expect(charge(50, declinedCard).ok).toBe(false));
```

**Invariant → property test instead of three examples.**

```js
// GOOD — one property replaces (and out-covers) a pile of hand-picked round-trip cases.
fc.assert(fc.property(fc.string(), (s) => {
  expect(decode(encode(s))).toBe(s);    // holds for every input, shrinks failures to a minimal case
}));
```

The through-line: each good test states a contract, runs the same way every time, and fails for exactly one nameable reason. That is what makes a green suite mean something.
