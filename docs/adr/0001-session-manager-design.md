# 0001: A shared session model with optional HTTP binding

Status: Accepted. This record describes the initial independent implementation.

The [specification](../specs/0001-session-manager.md) is the behavioral contract.
The [implementation record](../plans/0001-session-manager.md) contains the initial
work sequence and dated validation results.

## Context

The library manages several independent namespaces in a Fiber request and also
needs explicit access to existing sessions outside HTTP handling. Both entry
points must use the same record format, Codec behavior, expiry rules, and errors.
The independent package must work with official `fiber.Storage` implementations
without depending on an application's authentication or user model.

## Decisions and rationale

### Keep one Session type

Manager owns normalized namespace configuration and any default storage. Session
owns the ID, pending values, encoded values, and expiry state. An optional private
request binding carries Fiber context, identifier provenance, and request ownership.

HTTP requests load sessions lazily and save them at the end of the middleware
chain. `GetByID` returns the same public Session type without an HTTP binding or
automatic saving. Shared persistence code keeps the two APIs consistent. Sessions
are not pooled and have no Release obligation, which keeps detached ownership clear.

### Preserve Codec behavior and deferred encoding

Each value is encoded separately inside a versioned envelope. Loading decodes the
envelope; Get decodes only the requested value. Set retains the original value and
every Get or Save re-encodes pending values, including after an earlier Save.

CBOR is the default. Configured Marshal and Unmarshal functions handle both values
and the envelope. Get passes the original target to the Codec, preserving its
validation and partial-mutation behavior. Copies at byte-buffer boundaries protect
stored data from sequential buffer reuse and decoder mutation.

### Use the official storage interface

Each namespace and ID maps to one storage key. Supplied storage remains owned by
the caller; Manager closes only the default stores it created. Generic
`fiber.Storage` enables provider reuse but has no compare-and-swap or transaction
primitive. Concurrent snapshots therefore use last-write-wins semantics.

Reads outside requests do not renew expiry; explicit saves do. The stored absolute
deadline survives reloads and regeneration, while Reset starts a new lifetime.
ID rotation validates a replacement before deleting the old key and persists the
replacement on the next Save.

### Make configuration and runtime errors distinct

New panics for invalid configuration before default storage allocation. Explicit
operation errors return to the caller. Automatic-save errors go to the namespace
ErrorHandler, and the original downstream error is returned unchanged to Fiber.
This keeps initialization failures separate from request and backend failures.

CookieSecure and CookieHTTPOnly are ordinary bools, both false by default, so
consumers configure HTTPS and script-access restrictions explicitly. HTTPS examples
enable both. SameSite=None forces Secure to produce usable cookies.

## Consequences

- A Manager can be shared, but each Session is used sequentially. HTTP Sessions
  cannot outlive their requests; detached Sessions have no retained request context.
- Stale saves can overwrite updates or recreate deleted records. Atomic revocation,
  multi-namespace transactions, enumeration, and user indexes require application
  coordination or a separate abstraction.
- Failed persistence does not emit a new identifier, but a storage error may occur
  after a backend write took effect. Rotation is not a transaction across two keys.
- Applications must configure their cookie policy and enforce value-size limits.
- The module has an independent API and record format. Existing Gob or internal
  records require a compatible decoder or a new session population.
