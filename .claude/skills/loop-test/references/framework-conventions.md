# Framework Conventions — Detect and Match the Existing Stack

The governing rule: **match the repo, do not reinvent it.** A test that uses a different framework, assertion style, or file layout than the rest of the suite is a maintenance liability even if it passes. Detect first, mirror an existing test file second, and scaffold a framework only when there truly is none.

## 1. Detection procedure

Run these in order; stop once the stack is unambiguous. **The manifest's declared test dependency and `test` script are ground truth — trust them over file extensions or a hunch.**

1. **Read the manifest.** `package.json` (`devDependencies`, `scripts.test`), `pyproject.toml` / `setup.cfg` / `pytest.ini` / `requirements-dev.txt`, `Gemfile`, `go.mod`, `pom.xml` / `build.gradle(.kts)`, `Cargo.toml`. The test dependency and the `test` script name the runner and its invocation.
2. **Find existing tests.** Glob for the ecosystem's test globs (§3). Open two or three and copy their structure — imports, describe/it vs function-per-test, assertion calls, fixture usage. Existing tests outrank any doc as the source of truth.
3. **Read test config.** `jest.config.*`, `vitest.config.*` (or the `test` block in `vite.config.*`), `pytest.ini` / `[tool.pytest.ini_options]`, `.rspec`, `.mocharc.*`. Config reveals roots, setup files, coverage settings, and custom matchers.
4. **Find the run command.** Prefer the manifest's `test` script or the documented command over invoking the runner binary directly, so you inherit the project's flags.
5. **Check CI.** `.github/workflows/*`, `.gitlab-ci.yml`, etc. show the canonical command the project actually runs — the ground truth for "how tests are run here".

In a monorepo with more than one runner, scope detection to the package that owns the file under test: walk up to its **nearest** manifest, not the repo root.

## 2. What to mirror once detected

- **Framework & runner** — the exact library and the exact invocation (script, not raw binary).
- **File naming & location** — `*.test.ts` beside source vs `__tests__/` vs `tests/` vs `*_test.py` vs `spec/`. Match the dominant pattern.
- **Test structure** — `describe/it` blocks vs flat functions vs class-based (`unittest.TestCase`, JUnit classes).
- **Assertion style & argument order** — `expect(actual).toBe(expected)` vs `assertEquals(expected, actual)` vs bare `assert x == y`. Copy the local order; it differs across frameworks and getting it backwards inverts failure messages.
- **Fixtures & factories** — pytest fixtures, Jest `beforeEach`, factory-bot, testing-library render helpers, builder functions. Reuse the project's, don't hand-roll new setup.
- **Mocks** — the repo's mocking lib. Do not introduce a new one; Go and Rust in particular favor hand-written fakes over a mocking dependency.

## 3. Per-ecosystem conventions

Match these exactly. **The single most reliable move is to open a neighboring test file and copy its shape** — this table is the fallback for when no sibling test exists.

| Ecosystem (runner) | File location & naming | Run a single test | Assertion style | Fixtures / setup | Mocking |
|---|---|---|---|---|---|
| **Jest** (JS/TS) | `__tests__/` or beside source; `*.test.ts` / `*.spec.ts` | `jest path -t "name"` | `expect(x).toBe(y)` | `beforeEach`/`afterEach`/`beforeAll` | `jest.mock()`, `jest.fn()`, `jest.spyOn()` |
| **Vitest** (JS/TS) | beside source; `*.test.ts` / `*.spec.ts` | `vitest run path -t "name"` | `expect(x).toBe(y)` (Jest-compatible) | `beforeEach`/`afterEach` from `vitest` | `vi.mock()`, `vi.fn()`, `vi.spyOn()` |
| **Mocha** (JS/TS) | `test/`; `*.test.js` / `*.spec.js` | `mocha path --grep "name"` | usually `chai`: `expect(x).to.equal(y)` | `beforeEach`/`before` hooks | `sinon` (stubs/spies/mocks) |
| **node:test** (JS/TS) | `test/` or beside source; `*.test.js` | `node --test --test-name-pattern "name"` | `node:assert`: `assert.strictEqual(x, y)` | `beforeEach`/`before` from `node:test` | `mock` from `node:test`, or `sinon` |
| **pytest** (Python) | `tests/`; `test_*.py` / `*_test.py`; funcs `test_*` | `pytest path::test_name` or `-k "name"` | bare `assert x == y` | `@pytest.fixture`, `conftest.py` | `pytest-mock` (`mocker`), `unittest.mock`, `monkeypatch` |
| **unittest** (Python) | `tests/`; `test_*.py`; `TestCase` subclasses | `python -m unittest module.Class.test_name` | `self.assertEqual(x, y)` | `setUp`/`tearDown` | `unittest.mock` (`patch`, `MagicMock`) |
| **go test** (Go) | beside source, same pkg; `xxx_test.go`; `TestXxx(t *testing.T)` | `go test -run TestName ./pkg` | `if got != want { t.Errorf(...) }` | table-driven cases; `t.Cleanup()`; `TestMain` | interfaces + hand-written fakes; `httptest`; `testify/mock` if present |
| **JUnit 5** (Java/Kotlin) | `src/test/java/...` mirroring pkg; `XxxTest.java` | `mvn test -Dtest=Class#method` / `gradle test --tests "...method"` | `assertEquals(expected, actual)` or AssertJ `assertThat(x).isEqualTo(y)` | `@BeforeEach`/`@BeforeAll` | Mockito: `mock()`, `@Mock`, `when(...).thenReturn(...)` |
| **RSpec** (Ruby) | `spec/` mirroring `lib`/`app`; `*_spec.rb` | `rspec path:LINE` or `-e "description"` | `expect(x).to eq(y)` | `let`, `before`, `subject` | `double`, `allow(...).to receive(...)`, `instance_double` |
| **cargo test** (Rust) | unit: `#[cfg(test)] mod tests` in-file; integ: `tests/*.rs` | `cargo test test_name` | `assert_eq!(x, y)`, `assert!(cond)` | plain fns; helper builders; `once_cell` for shared state | trait objects + fakes; `mockall` if present |
| **PHPUnit** (PHP) | `tests/`; `*Test.php`; `TestCase` subclasses | `phpunit --filter test_name` | `$this->assertSame(y, x)` | `setUp`/`tearDown`, data providers | `createMock()`, Mockery if present |

Notes that bite if ignored:
- **Assertion argument order** differs — Jest/Vitest put actual first, JUnit/PHPUnit put expected first. Mirror the local order.
- **pytest** discovers `test_*` functions with no class needed; don't wrap them in `TestCase` unless the file already does.
- **Async** — check how the repo awaits: `async` test fns (Vitest/Jest/`pytest-asyncio`), `#[tokio::test]` (Rust), a latch or `@Test` in JUnit. Mirror the existing async test rather than inventing a pattern.
- Detect the **specific** runner in use — never assume from the language alone (a Python repo may be pytest or unittest; a JS repo may be Jest, Vitest, Mocha, or node:test).

## 4. The rule: match, mirror, then (only if empty) scaffold

1. **Match** the framework the repo already declares (§1). Never add a second runner alongside an existing one.
2. **Mirror** the closest existing test file (§2–§3) as a structural template — same directory convention, import block, assertion dialect, and setup mechanism. Faster and more correct than reconstructing convention from memory.
3. **Scaffold only when the repo has zero test setup** — no runner in the manifest, no test dir, no test files anywhere, and the user is asking you to establish testing. Then:
   - Pick the **ecosystem default**: JS → Vitest for a Vite/ESM project else Jest; Python → pytest; Ruby → RSpec; Go → built-in `testing`; Java → JUnit 5; Rust → built-in; PHP → PHPUnit. Prefer the lightest option the build already supports.
   - Add the runner and a `test` script/target so the project's normal test command works, wire minimal config, and add one real passing test to prove the harness runs before generating more.
   - **Flag it in your report** — introducing a framework is a project decision, especially for a library others depend on. Don't smuggle it in.

Before reporting done, run the single-test command from §3 so the test both runs and fails for the right reason (see the parent `SKILL.md` verify step). When this skill runs under the `loop-engine` harness, treat framework detection as a cheap upstream phase whose result feeds every downstream author agent — detect once and carry it forward through the pipeline rather than re-detecting per file (harness policy H1: pipeline-by-default, pass shared context forward instead of recomputing it).
