# Using jig as a library

`runjig` is the smallest program that uses jig as a library: load, validate, plan, run, report — the five calls that are the library's surface, in about eighty lines. It is the program [`docs/library.md`](../../docs/library.md) describes.

```console
    cd examples/library
    go run ./runjig ../commands/jig.yaml
    go run ./runjig ../commands/jig.yaml report.json
```
