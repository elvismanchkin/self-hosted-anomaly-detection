## Summary

<!-- What changes and why. Link the issue or plan (docs/plans/…) it implements. -->

## Changes

<!-- One line per change; for bug fixes: symptom → cause → fix. -->

-

## Test plan

<!-- Tick what you ran; CI runs the first group on every PR. -->

- [ ] `anomalyd`: `gofmt -l .` clean, `go vet ./...`, `go test -race ./...`
- [ ] New or changed behaviour has a test that fails without the change
- [ ] Prometheus / Alertmanager / OTel configs: `promtool check rules` + `promtool test rules`, `amtool check-config`, `otelcol-contrib validate`
- [ ] PoC Python: `prom_anomaly_job.py --selftest`; Go/Python detector parity if `internal/detect` changed
- [ ] `python3 docs/cost_model.py --write` if cost inputs changed
- [ ] e2e (`anomalyd/test/e2e/run.sh`, or the **e2e** workflow) for ingest, eval or alerting changes

## Deployment notes

<!-- Flag changes, state-file compatibility (stateVersion), config migrations, rollout order
     (server before agents?), anything operators must do. "None" is a fine answer. -->

## Docs

- [ ] README / `anomalyd/README.md` / `docs/` updated, or not needed
