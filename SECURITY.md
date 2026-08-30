# Security policy

## Supported versions

`forge` is in early development and the public surface is unstable. Fixes go to
`main`; there are no maintained release branches yet.

## Reporting a vulnerability

Report privately through GitHub, using **Security → Report a vulnerability** on
this repository — the [advisory form][advisory] opens a channel only the
maintainers can read. Please do not open a public issue for a vulnerability.

[advisory]: https://github.com/okian/forge/security/advisories/new

A useful report says what an attacker gets, and how you got there. For this
project that usually means one of:

- A **declaration** — the type whose generation produced the problem — plus the
  file `forge` wrote for it.
- A path `forge` read or wrote that it should not have. Generation walks a
  package and writes files beside its source, so anything that escapes the
  package directory is a bug worth reporting.
- Generated code that compiles but is unsafe: a codec that reads past a bound,
  a decoder that trusts a length from the wire, an emitted helper that races.

Expect an acknowledgement within a week. If a fix is warranted, the advisory
stays private until it ships, and you will be credited in it unless you would
rather not be.

## Scope

Generated output is in scope: `forge`'s whole job is producing code somebody
else compiles, so a flaw in what it emits is a flaw in the tool.

The marker types in the root package are compile-time vocabulary and carry no
runtime behaviour, so they are not an attack surface on their own.
