# Documentation

Start with the [project README](../README.md) for installation and a short
integration, or run one of the [examples](../examples/README.md).

| Audience | Document | Purpose |
| --- | --- | --- |
| Library users | [Usage guide](guide.md) | Configuration, typed values, lifecycle, storage, and background access |
| Library users and contributors | [SessionManager specification](specs/0001-session-manager.md) | Exact observable behavior and API contracts |
| Contributors | [Contributing](../CONTRIBUTING.md) | Development, validation, pull requests, and AI contribution policy |
| Contributors | [Architecture decision 0001](adr/0001-session-manager-design.md) | Rationale and consequences of the initial design |
| Maintainers | [Maintainer guide](maintainers.md) | Release checks and module publication |
| Security reporters | [Security policy](../SECURITY.md) | Private reporting and operational boundaries |
| Coding agents | [Repository instructions](../AGENTS.md) | Agent workflow and handoff requirements |

From a checkout, use `go doc -all .` to browse the exported API. The examples in
[`example_test.go`](../example_test.go) also appear in GoDoc and run during tests.

## Engineering history

The [initial implementation record](plans/0001-session-manager.md) preserves the
extraction plan and its dated validation results. Those results describe the
initial implementation, not the current checkout or the status of remote CI.

Specifications describe what the library guarantees. Architecture decisions
explain why a design was selected. Engineering plans record scoped work and its
outcome; they do not replace either the specification or the contributor guide.
