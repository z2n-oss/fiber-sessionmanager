# Repository Guidelines

This file contains working instructions for AI coding agents. The contributor
workflow and AI contribution policy for all developers live in
[CONTRIBUTING.md](CONTRIBUTING.md).

## Project Overview

This repository provides a session management middleware for Go Fiber v3.

Its primary features include:

- Multiple independent, named sessions per HTTP request.
- Pluggable serialization codecs, with CBOR as the default.
- Deferred serialization and lazy deserialization.

The library is intended for general-purpose use and must not depend on application-specific business logic.

## Repository Structure

- `README.md`: Project overview and entry point for users.
- `docs/guide.md`: Detailed integration and operational guidance.
- `docs/specs/`: Behavioral contracts and architecture.
- `docs/adr/`: Architecture decisions and their rationale.
- `docs/plans/`: Curated engineering plans and historical implementation records.
- `examples/`: Simple integration examples with runnable programs and READMEs.
- `CONTRIBUTING.md`: Contributor workflow and policy, including AI-assisted work.
- `docs/maintainers.md`: Maintainer release procedures.
- `SECURITY.md`: Private vulnerability reporting and security boundaries.

## Coding Conventions

- Follow idiomatic Go conventions and standard library practices.
- Format all modified Go files with `gofmt`.
- Write public documentation and code comments in English.

## Working on the Repository

- Read the relevant specification and architecture decisions before changing
  observable behavior. Treat module paths, supported Go versions, defaults,
  lifecycle semantics, and stored records as public contracts.
- Preserve unrelated workspace changes. Keep edits within the requested scope;
  avoid mixing dependency updates or broad refactors into documentation changes.
- Keep the public API, implementation, tests, examples, and specifications aligned.
  Document a deliberate contract change and its rationale in the same patch.
- Preserve error chains, deferred encoding, native Codec target behavior, and
  storage ownership. Inspect the existing code before introducing abstractions.
- Keep agent execution instructions in this file. Do not place prompts, chat
  transcripts, task-specific approvals, or temporary progress logs in public
  documentation. Engineering plans should be curated records, not agent scratchpads.
- Keep runnable examples in the root Go module, small and self-contained. Include
  run commands, expected results, and any local-development assumptions in each
  example's README. Do not add application-specific infrastructure to teach an API.
- When reorganizing documentation, update inbound relative links and preserve
  historical records unless the task calls for their removal.

## Validation and Handoff

- Follow the checks in CONTRIBUTING.md that are relevant to the change and any
  required repository checks. Run modified examples; compilation alone does not
  verify their documented behavior.
- For regression tests, prefer real Fiber requests, small fault-injection stores,
  and the internal namespace clock instead of timing sleeps. Cover HTTP and detached
  access when a shared persistence contract changes.
- Format modified Go files with gofmt and check links in edited documentation.
- Report the checks actually completed, their outcomes, and any checks that could
  not run. Local validation does not establish that remote CI or publication passed.
- Summarize the resulting behavior and material limitations. Keep historical
  measurements dated rather than presenting them as current validation.
