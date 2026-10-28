# Contributing

Contributions can improve the library, tests, integration examples, or documentation.
This guide applies to everyone contributing to SessionManager, including people
using AI tools.

## Discussing a change

Search existing issues and pull requests before opening a new one. For a bug,
include a minimal reproduction, the Go and Fiber versions, the storage provider,
and relevant configuration. Use synthetic data instead of real session IDs or
credentials. Report vulnerabilities through [SECURITY.md](SECURITY.md).

For changes to the public API, persistence format, or lifecycle behavior, open an
issue describing the use case and tradeoffs before investing in a large patch.
The library is general-purpose; application-specific authentication, user indexes,
and business logic belong in consuming applications.

## Local development

Install Go 1.26 or newer, then work from a repository checkout:

```sh
git clone https://github.com/z2n-oss/fiber-sessionmanager.git
cd fiber-sessionmanager
go mod download
go test ./...
```

The [documentation index](docs/README.md) links to the usage guide, specification,
and architecture decisions. Runnable [examples](examples/README.md) use the root
module and can be launched with `go run ./examples/<name>`.

Follow idiomatic Go, format Go files with `gofmt`, and write public documentation
and code comments in English. Include regression coverage for behavior changes
and update the relevant specification, guide, and examples when contracts change.
Keep dependency changes focused and compatible with the supported Go versions.

## Validation

For Go changes, run the usual checks from the repository root:

```sh
gofmt -l .
go test ./...
go vet ./...
```

The formatting command should print nothing. Tests also compile the runnable
examples and execute the GoDoc examples in `example_test.go`. When changing a
runnable example, run it and check the behavior described in its README. For
documentation changes, check relative links and any commands or snippets you edit.

The full CI checks also include:

```sh
go mod tidy -diff
go mod verify
go test -race -cover ./...
go test -run '^$' -fuzz '^FuzzDecodeRecord$' -fuzztime=10s -parallel=2
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

See the [CI workflow](.github/workflows/ci.yml) for the supported Go matrix and
check configuration. For performance changes, compare benchmarks on the same
machine using `go test -run '^$' -bench . -benchmem`. They include Fiber test
transport overhead and do not set hard performance thresholds.

## Using AI tools

AI tools are welcome for implementation, tests, documentation, and review. The
person submitting a contribution remains responsible for its correctness,
security, licensing, and maintenance, just as for code written without AI.

- Review and understand every submitted change. Verify APIs and behavioral claims
  against the code and specification, and run the relevant checks yourself.
- Describe material AI assistance in the pull request: which parts it helped with
  and how you reviewed and validated the result. A short note is enough; sharing
  private prompts or full chat transcripts is not required. Routine completion or
  spelling suggestions do not need individual attribution.
- Do not send credentials, live session data, private reports, or other information
  you are not authorized to share to an AI service. Use synthetic reproductions.
- Check the provenance and license compatibility of suggested code and preserve
  required attribution. AI output does not remove those responsibilities.
- Support generated claims with actual evidence. Distinguish completed checks from
  checks that failed, were skipped, or could not run; include remaining limitations.
- Configure coding agents to follow [AGENTS.md](AGENTS.md). Keep agent instructions
  there; user guides and examples should explain the library to its users.

Maintainer review and acceptance criteria are the same for AI-assisted and other
contributions. Generated tests and explanations are subject to review too.

## Submitting a pull request

Keep the change focused. Explain the problem, the resulting behavior, and any
compatibility implications. Include relevant test results and reproduction steps,
link related issues, and note material AI assistance as described above.

For changes to concurrency, expiry, or storage, explain the failure cases the
change handles. Screenshots and raw tool transcripts do not replace a reproducible
example or a clear description of what was checked.

Release procedures for maintainers are documented in
[the maintainer guide](docs/maintainers.md).
