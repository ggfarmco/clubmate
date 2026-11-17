# Testing

CI runs on a 3-core macOS runner under `-race`, about 3× slower than a dev Mac. The rules come from the CI and local flakes in clubmate-notes `research/2026-11-14-ci-flakes.md`; a *Flake:* note names the CI reds a rule would have prevented. `tools/testlint` checks parts of rules 1, 2, 3 and 5 in `make test` and CI; see Checks for exactly what it flags and what it misses.

1. **Wait for a condition, never sleep.** Use `testutil.Eventually(t, cond, d)`. To prove that something does *not* happen, use `testutil.Never(t, cond, d)`. A `time.Sleep` is allowed only inside a closure such as a fake handler or a goroutine. A sleep is a guess about the machine's speed; it is either too long locally or too short on CI.
2. **A deadline detects hangs; it does not assert speed.** Every deadline goes through `testutil.Timeout`, `Eventually` or `Poll`, which scale it by `TEST_TIMEOUT_SCALE` (default 3). Don't call testify's `Eventually`/`Never` directly. A helper that prints its own failure message uses `testutil.Poll`. A test that asserts *which* of two deadlines fires leaves seconds between them, not hundreds of milliseconds: under load a 600 ms plugin start took over 1 s (TestWaitForStartAndCallShareOneTimeout). The lint can't see this; reviewers check it. *Flake:* the 5 s harness `WaitSlot` and the 10 s agents waits (6 red runs).
3. **Never read state once right after a wait.** After `waitStatus`, a restart or an action, the plugin may still be settling. Wait for the exact value you assert (`testutil.Eventually` on it, `testutil.WaitEvents` for the bus), not for a status the session may already have. *Flake:* TestAgentExitKeepsIdleSessionState, TestOpenItems…SurviveARestart, TestArchiveFromAStalePlanDoesNothing (6 red runs).
4. **"Ready" means ready.** A test waits on the signal a client would use, such as the plugin's `running` state or a rendered slot. If the product sends a ready signal before it can serve, fix the product, not the test. *Flake:* `<id>.ready` arrived before `running`, giving `plugin unavailable` (4 red runs, fixed in 4b2a2c8f).
5. **Nothing shared between tests or runs.** Use `t.TempDir()`, `testutil.ShortDirs` for socket paths, a port of `:0` and a per-test workspace. Never use a fixed name under `os.TempDir()`: worktrees run the same tests at the same time.
6. **Git in tests is isolated and quiet.** Use one `GIT_CONFIG_GLOBAL` per package from `TestMain`, with `maintenance.auto = false` and `GIT_CONFIG_NOSYSTEM=1`, as in `plugins/agents`. Without it, background `git maintenance` competes for the runner (#529).
7. **A test owns its processes.** Start them through the harness or `exec.CommandContext(t.Context(), …)` and kill them in `t.Cleanup`. `testutil.NewService` kills session leftovers.
8. **No network.** Use a local fake server; check that the app is present before any DNS lookup. *Flake:* TestBrowserWithoutAppFailsFast resolved example.com.
9. **`t.Parallel()` shares only read-only fixtures.** Each parallel test gets its own harness, bus, repo and temp dir. A test that calls `t.Setenv` cannot be parallel.
10. **A failure explains itself.** A failing harness test prints the supervisor log, each plugin's state, last error and log tail. Wait helpers print the last state they saw. Keep it that way when you add helpers.

## Checks

- `make lint-tests` runs `tools/testlint` on `_test.go` files (not `internal/testutil`). It flags:
  - `time.Sleep` in a test, helper or method body, outside func literals; the only allowed methods are the slow fakes `cli.syncBuffer.Write` and `sessionhost.throttled.Read`, and macui's `instance.eventually` poll;
  - `time.After(d)` outside func literals unless `d` is `testutil.Timeout(…)`;
  - testify `Eventually`/`EventuallyWithT`/`Never` and their `f` forms anywhere;
  - `(*bus.Bus).Read` outside func literals (internal/bus's own tests excepted);
  - `filepath.Join(os.TempDir(), "lit")` and `os.TempDir() + "lit"`.

  It does not see sleeps or deadlines inside func literals, calls through function values, `time.NewTimer`, an unscaled `context.WithTimeout` deadline, `time.Since` bounds, interface-typed buses, or `"/tmp/…"` literals (#586).
- `make test-stress BASE=<sha>` reruns the new or edited `Test` functions up to 10× under `-race`, with CPU hogs and `TEST_TIMEOUT_SCALE=1`. Packages run fewest selected tests first, each in an equal share of what is left of the 900 s budget: one run to time it, then as many more (up to 10 in all) as fit the share, so time one package leaves goes to the rest. CI's `stress` job does the same for every push and PR (advisory until #590 calibrates it). A test that fails there would flake on CI.
- CI reruns a failed test up to twice (`scripts/ci/go-test.sh` through gotestsum `--rerun-fails`, `scripts/ci/swift-test.sh` for XCTest; more than 10 failures in one run are not rerun). A test that passes on a rerun does not fail the job: it becomes a "Flaky test" warning annotation and a line in the job summary, and `scripts/release/flaky-backlog.sh` files the released run's flaky tests as one backlog task (tags `flaky`, `ci`). A rerun hides nothing: fix those tests like any other flake. CI runs one push to `main` at a time; a newer push replaces the one still waiting.
- To reproduce a timing flake locally, run `TEST_TIMEOUT_SCALE=1 GOMAXPROCS=3 go test -race -p 3 -count=N` with a dozen `yes >/dev/null` running, and kill them afterwards. Run the test in the foreground: a job a script starts with `&` has SIGINT ignored, so a test that sends ^C to a pty fails.
