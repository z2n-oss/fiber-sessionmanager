# Changelog

Notable changes to SessionManager are documented here, with the newest version
first. Unreleased changes are collected before the next release.

## [0.1.0] - 2026-10-28

Initial public release of SessionManager, a standalone session management
middleware for Go Fiber v3.

### Added

- Multiple independent, named sessions within a single HTTP request.
- Pluggable serialization Codecs, with CBOR as the default, deferred value
  encoding, and lazy value decoding.
- Official `fiber.Storage` support, with independent in-memory stores by default
  and explicit ownership of supplied storage providers.
- Automatic request-end saving in namespace registration order, explicit saves,
  and per-namespace error handlers for automatic-save failures.
- Idle and absolute timeouts, ID regeneration, session reset, and destruction.
- Detached background access through `GetByID` and `DeleteByID`, with explicit
  saving and context-aware lifecycle operations.
- Configuration and generated-ID validation, versioned record envelopes, and
  protection against Codec byte-buffer reuse and decoder mutation.
- Idempotent Manager shutdown that closes only manager-owned storage.
- Runnable examples for HTTP sessions, custom JSON serialization, and background
  access, alongside English usage guides, specifications, and architecture notes.
- Regression tests, fuzz tests, benchmarks, and GitHub Actions CI.
- Apache-2.0 licensing and contributor, maintainer, and security documentation.

### Requirements

- Go 1.26.0 or newer and Fiber v3.6.1 or newer.
- Module path: `go.z2n.io/fiber/sessionmanager`.

### Behavior notes

- `CookieSecure` and `CookieHTTPOnly` both default to `false`. Enable them
  explicitly for production HTTPS deployments. `SameSite=None` forces Secure.
- Background reads do not renew expiry or save automatically. Explicit saves
  refresh idle expiry while preserving the stored absolute deadline.
- Concurrent snapshots use last-write-wins semantics. A delayed save can overwrite
  another update or recreate a deleted record; atomic revocation and transactions
  across namespaces are not provided.
- The API and record format are independent of Fiber's built-in session middleware
  and the original internal implementation. Existing records require a compatible
  decoder or new sessions; automatic migration is not provided.

[0.1.0]: https://github.com/z2n-oss/fiber-sessionmanager/releases/tag/v0.1.0
