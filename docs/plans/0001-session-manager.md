# Initial SessionManager implementation record

The authoritative contract is [the specification](../specs/0001-session-manager.md).
This historical record preserves the initial extraction plan and its validation
results. Current development guidance is in [CONTRIBUTING.md](../../CONTRIBUTING.md),
and design rationale is in [architecture decision 0001](../adr/0001-session-manager-design.md).
Internal application handlers were outside the scope of the extraction.

## Confirmed decisions

- Module `go.z2n.io/fiber/sessionmanager`, root package `sessionmanager`.
- Go 1.26.0 minimum; Fiber v3.6.1, CBOR v2.9.6, memory/v2 v2.1.2.
- Apache-2.0; no legacy API, Gob, or stored-session compatibility promise.
- Invalid configuration panics; runtime errors preserve the original dispatch model.
- Ordinary bool CookieSecure/CookieHTTPOnly fields, both false by default.
- Shared Session type for request and background access, with Save/WithContext pairs.
- Background reads do not renew expiry; explicit background saves refresh idle expiry.
- Official fiber.Storage with last-write-wins semantics and no record size limit option.

## Architecture

Keep one public Go package. Manager owns normalized namespace configurations and
default storage. Session owns data and lifecycle state. An optional request
binding contains Fiber context, extractor provenance, and request-container
ownership; detached background sessions have no binding. Shared load/save logic
implements one format and one set of expiry and error rules.

Retain lazy envelope loading, value-level decoding, pending reference values,
explicit save followed by final save, request-local Manager conflict detection,
and caller-owned storage. Avoid pools or a Release obligation for public objects.

## Execution sequence

1. Establish the English specification and this implementation plan.
2. Migrate the internal component and tests to the independent root package.
3. Separate transport bindings; add GetByID/DeleteByID and detached lifecycles.
4. Validate configuration and generated identifiers before side effects; fix
   empty envelope writes and Codec byte-buffer ownership.
5. Add deterministic expiry, background, failure, and concurrency regressions;
   retain the original typed decoding and lazy serialization contracts.
6. Complete README, GoDoc, executable examples, contributing/security guidance,
   license, CI, fuzz tests, and benchmarks.
7. Run the full validation suite and record actual results below.

## Validation strategy

Use real Fiber requests, official memory storage, fault-injection stores,
controllable clocks, and counting/buffer-reusing Codecs. Cover constructor
validation before allocation, storage ownership, multiple namespaces, all data
and lifecycle methods, expired/missing/corrupt records, explicit Context forwarding,
identifier provenance, failed persistence, business/error-handler panics, and
different Manager conflicts.

Background tests must prove that reads do not save or write HTTP state, missing
records are not created, deleting corrupt/missing records works, and explicit
save renews only idle expiry. Ordered overlapping HTTP/background operations
document overwrite and deletion-resurrection behavior rather than claiming
atomicity that fiber.Storage cannot provide.

Exercise the default Codec's nil/empty distinctions and direct target behavior,
JSON as a custom Codec, reused Marshal buffers, mutating/aliasing decoders, and
empty encoded envelopes. Fuzz default envelope decoding. Benchmark unused,
read-only, and modified sessions without imposing hardware-dependent timing gates.

Required checks:

```sh
gofmt -l .
go mod tidy -diff
go mod verify
go test ./...
go test -race -cover ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go test -run '^$' -fuzz '^FuzzDecodeRecord$' -fuzztime=10s -parallel=2
go test -run '^$' -bench . -benchmem
```

CI runs tests and vet on Go 1.26 and 1.27; race, formatting, dependency, fuzz,
and vulnerability checks run on Go 1.27. Examples compile with the normal tests.

## Release preparation

The extraction prepared repository content for release. Domain deployment, tag
publication, and installation verification are covered by the
[maintainer guide](../maintainers.md). The implementation provides neither legacy
data migration nor atomic revocation across concurrent requests.

## Implementation status

Implementation and local validation completed on 2026-10-28 (Asia/Shanghai).

- Migrated the middleware into the independent module, preserving deferred
  serialization, lazy decoding, namespace isolation, and error dispatch.
- Added detached background access, context-aware lifecycle methods, explicit
  storage ownership, and idempotent shutdown.
- Hardened configuration and identifier validation, rotation failure handling,
  record validation, Codec buffer ownership, and automatic-save error responses.
- Added English specifications, package documentation, README, executable
  examples, contribution and security guidance, and the Apache-2.0 license.
- Added regression tests, fuzzing, benchmarks, and GitHub Actions configuration.

Local validation results on Linux/amd64:

| Check | Result |
| --- | --- |
| `gofmt -l .` | No formatting differences |
| `go mod tidy -diff` / `go mod verify` | Passed; all modules verified |
| Tests and executable examples on Go 1.26.0 | Passed |
| Tests and executable examples on Go 1.27.3 | Passed |
| Race detector and coverage on Go 1.27.3 | Passed; 97.7% statement coverage |
| `go vet ./...` | Passed |
| Default record decoder fuzzing, 10 seconds, 2 workers | Passed; 209,953 executions |
| HTTP unused/read/write benchmarks with allocation reporting | Completed; no timing thresholds imposed |
| govulncheck v1.9.0 | No vulnerabilities in called symbols or imported packages; one advisory in a required module, detailed below |

The module graph includes `golang.org/x/crypto v0.58.0`, which is associated with
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) for the unmaintained
`golang.org/x/crypto/openpgp` package. This package is not imported by the library,
and the advisory lists no fixed version. The scan passed, but this does not mean
every package in every required module is free of advisories. Re-run the scan
before each release because the advisory database changes independently of code.

At the time of the initial validation, the CI workflow had been prepared but had
not run on the remote repository. Publishing, enabling and verifying private
vulnerability reporting, configuring the vanity import mapping, and testing
installation from the published module were left as release operations described
in the [maintainer guide](../maintainers.md).
