## What this makes possible

<!-- The change in one or two sentences, said in terms of what a user or a
     caller can now do — not which files moved. -->

## Why

<!-- The decision behind it, not the mechanics of it. If this changes
     behaviour, link the issue where that was agreed. -->

## Checklist

- [ ] `make check` is green (formatting, `go vet`, golangci-lint, tests, coverage floor).
- [ ] Tests cover the new behaviour, and the golden suite is updated where output changed.
- [ ] Godoc on every exported identifier the change adds.
- [ ] No complexity budget in `.golangci.yml` was raised — or it was, and this PR says why.
