# CLAUDE.md

Research and a tested proof of concept for anomaly detection on a free, self-hosted stack
(Prometheus, Alertmanager, Grafana, Filebeat → Elasticsearch, Jaeger, OpenTelemetry). The code is
`anomalyd/`, a Go engine; everything else is research, plans and PoC configs. Start with
[README.md](README.md) and [anomalyd/README.md](anomalyd/README.md).

## Layout

- `anomalyd/`: Go module (Go 1.26). `cmd/anomalyd` (subcommands `server`, `agent`, `gen`,
  `bench`, `version`), `internal/` (`server`, `agent`, `detect`, `drain`, `series`, `logparse`,
  `promrw`, `wire`, `gen`), `deploy/` (agent DaemonSet, Grafana dashboards), `test/e2e/` and
  `test/bench/`.
- `poc/`: Prometheus rules and promtool tests, ElastAlert2 rules, OTel collector configs, Python
  Drain3 exporter and scorer.
- `docs/`: research, reference architecture, roadmap, cost model, glossary, and `docs/plans/`.

## Plans and where work stands

- [docs/plans/feature-gaps.md](docs/plans/feature-gaps.md): gaps G1–G37 beyond the older plans,
  in batches. **Batch 1 (G1, G2, G5, G6, G11, G12, G13) is built.** Batch 2 is next: G7, G8, G9,
  G10, G28, G29, G30, G31, G32, in the order the plan gives. Then "Next" (medium), then "Later".
- [docs/plans/future-improvements.md](docs/plans/future-improvements.md): items N1–N11, X1–X14,
  L1–L7. [model-trainer.md](docs/plans/model-trainer.md) (MT) and
  [deployment.md](docs/plans/deployment.md) (DP) are the other plans.
- Before building an item, read its section: it says what the existing plan items already cover,
  what to build, dependencies and "done when".

## Checks

From `anomalyd/` (CI runs the same on every pull request):

```sh
gofmt -l .            # must print nothing
go vet ./...
go test -race -count=1 ./...
CGO_ENABLED=0 go build -trimpath -o /dev/null ./cmd/anomalyd
```

- Fuzz targets: `go test -run='^$' -fuzz=FuzzDecode -fuzztime=30s ./internal/promrw` (also
  `FuzzHandlePush` in `internal/server`, `FuzzTokenize` in `internal/drain`). One target per run.
- Configs: `promtool check rules` / `promtool test rules` in `poc/prometheus/tests`,
  `amtool check-config`; see `.github/workflows/ci.yml` for exact versions.
- PoC Python: `python poc/ml/prom_anomaly_job.py --selftest`.
- End to end: `anomalyd/test/e2e/run.sh` needs Docker, Prometheus and Alertmanager. On GitHub it is
  the `e2e` workflow: weekly, or run it by hand (`workflow_dispatch`) on a branch. Run it for any
  change to ingest, evaluation or alerting. It expects three injected incidents to become exactly
  three alerts. In a cloud container without Docker, dispatch it on the branch (about 14 min) and
  link the run in the PR.
- The first `go` command may download the go1.26 toolchain (GOTOOLCHAIN); that is expected.
- Short fuzz runs need `-fuzzminimizetime=2s` next to `-fuzztime`; otherwise minimising new
  inputs uses up the budget.
- Mutation-check each new test: revert the fix briefly, see the test fail, restore.
- Version: build with `-ldflags "-X main.version=..."`, otherwise it reports `dev`.

## Conventions

- **No new Go dependencies.** Standard library plus `github.com/klauspost/compress` only.
- Match the surrounding code: terse comments, short names, the existing idioms. gofmt.
- Every behaviour change gets a test that fails without it. Concurrency changes are tested under
  `-race`.
- `internal/detect` must stay in parity with `poc/ml/prom_anomaly_job.py`
  (`internal/detect/testdata/parity.py`, tolerance 1e-9). Change both or neither.
- **Snapshots** (`internal/server/state.go`) are gob. Only add fields; keep `stateVersion` unless
  the format breaks. Old snapshots must load in new binaries and the other way round.
- **Finding labels are Alertmanager identity.** Never change a firing finding's labels; put
  changing detail in annotations.
- Lock order in the server: `findMu` → a miner lock (`drain.Locked`) → `tmplMu`, and `findMu` →
  a series shard lock. So never take `findMu` while holding a miner lock, `tmplMu` or a shard
  lock, and never take a miner lock while holding `tmplMu`. Copy under locks, then encode or do
  I/O with no lock held.
- A new finding kind needs a case in `restoreFindings` (state.go) and an entry in the
  `anomalyd_findings_active` kinds (metrics.go).
- Finding copies used outside `findMu` (API, snapshots) clone `Labels` and `Annotations`:
  evaluation rewrites annotations in place.
- Names that become labels or pool keys go through `cleanService` (at most 128 bytes, valid
  UTF-8). One invalid UTF-8 label value breaks every scrape.
- The agent's own `anomalyd_agent_*` metrics have no labels; the server's per-agent series carry
  `agent`. Check both `/metrics` endpoints for name collisions.
- Remote write drops NaN and ±Inf samples but counts them as arriving (`undefData`); only the
  staleness marker means a series is gone.
- New flags keep today's behaviour by default unless the plan item says otherwise. Bound every
  new map or label set (cardinality).
- **Docs are part of the change.** Update `anomalyd/README.md` for flags, metrics, endpoints and
  alerts; `docs/plans/deployment.md` for what operators must do; `docs/glossary.md` for new
  terms; the plan item's status in `docs/plans/feature-gaps.md`. Style: short plain sentences,
  terms explained or linked to the glossary, code referenced by function name. Do not edit the
  dated "Verification" sections.

## Git and pull requests

- Use the PR template (`.github/pull_request_template.md`): summary, one line per change
  (symptom → cause → fix for bugs), test plan, deployment notes, docs.
- Merge with a **merge commit**, not squash or rebase, so branches built on a merged commit stay
  valid.
- Run e2e before merging any change to ingest, evaluation or alerting.
- After a PR merges, restart the branch from `origin/main` for follow-up work.
- Parallel agents in git worktrees (`.claude/worktrees/`, ignored): a new worktree starts at the
  default HEAD, so `git checkout --detach <sha>` first. Split work by file: `New` (server.go),
  `load` and `copyState` (state.go), metrics.go and anomalyd/README.md conflict most.
