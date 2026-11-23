## jig Results: shop

| Property | Value |
|---|---|
| Spec | e2e/jig.yaml |
| Cluster context | kind-shop |
| Cluster | shop (kind) |
| Started | 2026-11-23T10:00:00Z |
| Duration | 6s |

| Result | |
|---|---|
| Total | 5 |
| ✅ Passed | 3 |
| ❌ Failed | 1 |
| ⏭ Skipped | 1 |

> ⛔ **Stopped** after 1 failure (step) — 3 of 4 expectations run

> 🔁 **1 expectation(s) needed a retry** — 1 extra attempt(s) total

**Command:**

```bash
jig run e2e --report
```

### Failed

| Test | Attempts | Time | Error |
|---|---|---|---|
| ❌ the order is accepted |  | 1s | exit code 1, want 0 curl: (7) refused |

### Passed

| Test | Attempts | Time |
|---|---|---|
| ✅ the API is up | 2 of 4 | 1.5s |

### Skipped

| Test | Attempts | Time |
|---|---|---|
| ⏭ only on prod |  | 0s |

### Warnings

Post-block failures (onFailure, finally); they do not change the results.

- **suite**: onFailure: kubectl logs: no pods match app=shop

### Imported jig Suites

#### jig Results: upgrade

| Property | Value |
|---|---|
| Spec | ./upgrade/jig.yaml |
| Duration | 2s |

| Result | |
|---|---|
| Total | 1 |
| ✅ Passed | 1 |
| ❌ Failed | 0 |
| ⏭ Skipped | 0 |

##### Passed

| Test | Time |
|---|---|
| ✅ upgraded | 2s |

**1 of 1 passed (2s)**

#### jig Results: db

| Property | Value |
|---|---|
| Spec | ./db/jig.yaml |
| Duration | 1s |

| Result | |
|---|---|
| Total | 1 |
| ✅ Passed | 1 |
| ❌ Failed | 0 |
| ⏭ Skipped | 0 |

> ❌ **Setup failed** at `migrate`: migrate: apply ./job.yaml: exit status 1 — 1 of 2 expectations run

##### Passed

| Test | Time |
|---|---|
| ✅ postgres is up | 1s |

**1 of 1 passed, setup failed at migrate (1s)**

❌ **Import failed:** migrate: apply ./job.yaml: exit status 1

#### ./broken/jig.yaml

❌ **Import failed:** loading import ./broken/jig.yaml: no such file

**3 of 5 passed, 1 skipped, 2 import(s) failed (6s)**

