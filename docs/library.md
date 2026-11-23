# As a library

```go
d, err := jig.FromFile("jig.yaml")      // parse, expand includes; every problem at once
if err != nil { … }
result, err := jig.Run(ctx, d, jig.Options{
	UseCurrent: false,                   // or Context: "kind-dev"
	Images:     []string{"shop-api:dev"}, // narrows environment.images
	ReportFile: "report.json",            // "" writes none; the CLI's bare --report writes report.md
})
```

`jig.Plan(w, d)` prints what a run will do, as `jig validate` does. `jig.Validate(d)` returns every problem; `jig.ValidateWith(d, funcs)` checks templates against your own functions too, when you run with `Options.Functions`. `Options.Parallel` bounds concurrent builds and `Options.EcosystemChart` installs another data-services chart.

Callers import the `jig` package only: it carries the whole vocabulary (`jig.Declaration`, `jig.Step`, `jig.Expectation`, the constants and validators), and its packages stay its own.

A caller with its own format maps it onto a `jig.Declaration` and runs that. [Inrun](https://inrun.dev) does: it keeps its e2e spec, maps it with `ToJig()` (its CRDs, bundle and install become ordinary steps), and loads its imports through `Options.Load`, so an Inrun e2e file and a plain `jig.yaml` run the same way.

| Package | Holds |
| --- | --- |
| `jig` | `FromFile`, `Validate`, `ValidateWith`, `Plan`, `Run`, `Discover`, `Suite`, and the vocabulary |
| `check` | The whole offline check: shape, files, images, git, needs, expressions, references |
| `ecosystem` | The data services: the plan, Secrets, install (`ecosystem/operator`: the pinned operators) |
| `gitsource` | Checkouts for images built from repositories, and their cache |
| `spec` | The jig: the `Declaration`, parsing, validation |
| `runner` | The run: cluster, images, environment, steps, expectations by `after`, imports, teardown |
| `expect` | Expectations, side effects and the result; `expect/report` writes it |
| `kubectl` | The kubectl checks |
| `post` | Teardown |
| `cli` | The CLI: `cli.Command()` is the root command |
| `cmd/jig` | Runs it |

---

Back to the [README](../README.md).
