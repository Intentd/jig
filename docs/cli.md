# The command line

```console
jig validate      # check the jig offline and print what a run will do
jig run           # run it and its imports, and print the report (or: jig it)
jig clean         # delete the clusters a run kept
jig version

jig run ./...     # every jig.yaml below, as one suite, like go test ./...
```

Each reads `jig.yaml` here, or the file or directory given as an argument or with `-f`.

| Command | Does |
| --- | --- |
| `jig validate` | Reads the jig, expands its includes and checks it, reporting every problem at once; then prints the cluster, the steps in order and the expectations after each. Touches no cluster. |
| `jig run` | Runs the jig: by default in a kind cluster it creates and deletes afterwards. |
| `jig clean` | Deletes the kind clusters a run left behind on purpose (`--keep`, `--keep=cluster`): the jig's, its `-imports` cluster, and those of its imports. |
| `jig version` | Prints the version. |

`jig run` flags:

| Flag | Does |
| --- | --- |
| `--use-current`, `-c/--context` | Run in the current context, or the one named; no cluster is created or deleted. |
| `--keep` | Leave everything as the run left it: no teardown, the cluster kept. `--keep=cluster` keeps only the cluster the run creates; what the run did is still torn down. Either way kubectl's context is left on the kept cluster, to look at it; without `--keep` it is switched back. |
| `--images` | Narrow `environment.images` to these; with none declared, these are loaded. |
| `--parallel` | How many images build at once (default: min(4, CPUs/2)); `--parallel=1` builds them one after another. |
| `--ecosystem-chart` | The `data-services` chart for `environment.ecosystem`: an `oci://` reference or a local chart directory (default: the chart jig pins). |
| `--workers`, `--kind-version` | Shape a cluster the run creates. |
| `--max-failures` | Stop after N failed expectations. |
| `--dry-run` | `client` (the default): print what would run. `server`: send the writes to an existing cluster as server dry runs, skipping commands and assertions. |
| `-r/--report` | Also write the report to a file: JSON for `.json`, JUnit XML for `.xml`, else Markdown (`report.md` when no name is given). |
| `--skip`, `--wait`, `--list`, `--limit` | With `./...`: leave out paths, pause between tests, list what a run would find (`--limit` of them). |

`--keep`, `--report` and `--dry-run` take their value with `=` (`--keep=cluster`); written bare, they mean `all`, `report.md` and `client`.

## Every jig in a tree: `./...`

As with `go test ./...`, a path ending in `/...` means every `jig.yaml` below it:

```console
jig validate ./...                  # check each, one line per jig
jig run ./...                       # run them all as one suite, on one cluster
jig run ./e2e/... --skip vendor,testdata --wait 2s
jig run ./... --list                # what a run would find, without running it
```

The jigs run in path order, sharing one cluster the way a suite's imports do. A jig that only imports (a suite of its own) is left out, so its imports don't run twice; `--skip` leaves out paths that match, and `--wait` pauses between jigs.

## The report

The report is written also when the environment or a step fails: Markdown for people and CI summaries, JSON for machines, JUnit XML for CI systems that show test results natively (GitLab, Jenkins, Azure DevOps, CircleCI, the GitHub test-report actions), by the report file's extension. It lists each expectation with its result, duration and last output, and each import as its own section.

---

Back to the [README](../README.md).
