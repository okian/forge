# Changelog

What changed, for somebody deciding whether to upgrade.

Not a commit log — the history is that, and it is better at it. What belongs
here is a change somebody outside this repository would notice: a declaration
that generates differently, a diagnostic that is new or gone, a layer that
arrived, a name the `plugin` surface added. A refactor nobody can observe does
not.

Entries are written in the commit that makes the change, under `Unreleased`, and
`Unreleased` becomes a version at release time. Writing them later means writing
them from the log, which is how a changelog turns into a worse copy of one.

Generated code is where "somebody would notice" is widest. A change to what
forge writes reaches every committed `forge.gen.go` in every checkout, and the
diff arrives in a review that has nothing to do with forge — so it is called out
here whether or not the API moved.

Versions follow [semantic versioning](https://semver.org). Nothing is released
yet, so the surface is unstable and the first tag is what starts the promise.
[`docs/releasing.md`](docs/releasing.md) is how a release is cut.

## Unreleased

### Added

- `forge`, the command: `generate`, `check`, `explain`, `list`, `doctor` and
  `version`, over declarations written inline or in a spec file.
- Twelve layers: `Slice`, `Ring`, `Collection`, `Json`, `Validate`, `Clone`,
  `Hash`, `Builder`, `Patch`, `Redact`, `Enum` and `Guarded`. Markers for the
  rest of the catalog are published as *staged*, so a declaration naming one
  type-checks and is answered with what is missing.
- `plugin`, the surface a layer is written against, and `driver`, the dozen
  lines a binary linking one holds.
- `github.com/okian/forge/x/csv`, a CSV transport in a module of its own,
  written against `plugin` and the standard library and nothing else. It claims
  forge's own staged `Csv` marker, so a declaration naming it generates once the
  layer is linked and reports pending work when it is not.
- A layer may claim a marker forge published and has not implemented, taking it
  over from the placeholder. Two layers that both generate for one marker are
  still refused.

### Fixed

- The `markers` line in a generated header names the module that declares the
  markers rather than the module of the binary that ran. A binary somebody
  linked a layer into stamped its own module, and `forge check` compares that
  line — so every file such a binary wrote looked to forge's own command like
  the work of different tooling. Every committed file under `x/csv/ledger` was
  rewritten by the fix.
- A boolean option is read with `strconv.ParseBool` rather than compared against
  the word `true`, so `//forge:json omitzero=1` is on rather than silently off.
  A declaration written that way generates a different document than it did.
