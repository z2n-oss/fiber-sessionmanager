# SessionManager specification

This is the authoritative specification for `go.z2n.io/fiber/sessionmanager`.
For integration instructions, see the [usage guide](../guide.md) and
[examples](../../examples/README.md). Design rationale is recorded in
[architecture decision 0001](../adr/0001-session-manager-design.md).

## Goals and boundaries

- Manage multiple independent, named sessions in one HTTP request.
- Support configurable serialization, with CBOR as the default.
- Defer value encoding until Get or Save, and decode values only when requested.
- Support explicit session access outside HTTP requests.
- Use the official `fiber.Storage` interface without application-specific logic.

The package is named `sessionmanager`. The minimum Go version is 1.26.0, as
required by the selected Fiber v3.6.1 dependency and its source-aware extractors.
The license is Apache-2.0. Compatibility with the original internal API,
existing sessions, or Fiber's Gob records is not promised.

This library does not provide session enumeration, user indexes, bulk revocation,
background session creation, storage adapters, or distributed transactions.
Applications and storage providers control data size; there is no record size
limit configuration.

## Construction and configuration

```go
manager := sessionmanager.New(
    sessionmanager.Config{Namespace: "auth"},
    sessionmanager.Config{Namespace: "preferences"},
)
app.Use(manager.Handler())
```

`New(configs ...Config) *Manager` requires at least one namespace. Configuration
errors panic, following Fiber's fail-fast initialization model. All configurations
are validated before default storage is allocated.

- Namespace is required, unique within a Manager, and consists of ASCII letters,
  digits, underscores, or hyphens.
- Storage defaults to an independently owned official memory/v2 store for each
  namespace. A supplied Storage must not be a typed nil and remains caller-owned.
- Marshal and Unmarshal must be supplied together; their defaults are the
  unmodified `cbor.Marshal` and `cbor.Unmarshal` functions.
- The default Extractor reads `session_id_<namespace>` from a cookie.
- The default KeyGenerator is `utils.SecureToken` (32 random bytes encoded as an
  unpadded URL-safe token). Custom generators must produce unpredictable, unique,
  nonempty IDs suitable for the configured identifier transports.
- IdleTimeout defaults to 30 minutes. AbsoluteTimeout defaults to zero (disabled).
  Negative timeouts are invalid; a positive absolute timeout must be at least the
  configured idle timeout and representable as a Unix nanosecond deadline.
- CookieSecure and CookieHTTPOnly are ordinary bool fields, both false by default.
  Production HTTPS examples explicitly enable both.
- CookiePath defaults to `/`, CookieSameSite to `Lax`, and CookieDomain to empty.
  SameSite accepts Disabled, Lax, Strict, and None, case-insensitively. None forces
  Secure. CookieSessionOnly defaults to false.
- Cookie names, paths, domains, and the `__Host-` / `__Secure-` prefix requirements
  are validated before use. Header names must be valid HTTP field names.
- Writable cookie/header locations cannot overlap across namespaces; header names
  compare case-insensitively. Writable targets are collected during construction.

Configuration, including Extractor trees and state captured by callbacks, must
not be mutated after construction. Callbacks and supplied Storage must support
concurrent calls. Source-aware extraction must retain the official Extractor's
identity; cloning its Chain can break provenance and is not a supported shortcut.

## Request lifecycle

`FromContext(c, namespace)` loads a session only on first access and returns the
same object for subsequent accesses. `FromContextWithContext(ctx, c, namespace)`
uses the supplied Go context for that first storage read only.

One request chain may contain one Manager. Re-entering the same Manager is
allowed; only its outer invocation saves. A different Manager returns
`ErrManagerConflict` without replacing the container. A private Locals key holds
the container; there is no global Manager singleton.

Missing middleware returns `ErrMiddlewareMissing` before namespace lookup.
Unregistered namespaces return `ErrUnknownNamespace`. Unaccessed namespaces have
no storage or client-identifier side effects.

A missing identifier, storage miss, or absolutely expired record creates a new
ID. A supplied ID with no usable record is never adopted. Extraction failures
other than `extractors.ErrNotFound`, storage failures, and corrupt records are
returned; they do not install an empty session for automatic saving.

On normal downstream return, including a business error, accessed sessions are
saved in registration order. Explicit Save does not suppress the final automatic
save. A panic propagating through the Manager skips subsequent implicit saves;
the request container is always cleared. Recover middleware must wrap the
Manager if a recovered panic should not be treated as a normal downstream return.

HTTP sessions cannot be used concurrently or retained beyond their request.
Finish session operations before streaming, hijacking, or moving work to another
goroutine; stream callbacks must not access a request-bound Session.

## Data and Codec contract

```go
func (s *Session) Set(key string, value any)
func (s *Session) Get(key string, target any) error
```

Set retains the original value in a pending map, without encoding it. Get on a
pending value encodes and then decodes it, so its behavior matches a persisted
value. Every Get and Save re-encodes pending values, including after explicit
Save. Reference values may change between calls but not concurrently with calls.

Get passes the original target to Unmarshal. The Codec validates that target and
may partially modify it on failure. Missing keys return `ErrKeyNotFound`; missing
keys and Marshal failures leave the target untouched. Mutating a decoded result
does not update the Session: call Set again to persist it.

Delete removes a key from both maps. Keys returns their merged keys, sorted.
ID returns the client identifier. Fresh reports creation or rotation since this
object was loaded; Save does not reset it.

Each session uses one KV, `session:<namespace>:<id>`. Namespaces shared across
Managers or applications on the same backend deliberately share that key space
and must use compatible configuration. Use different namespaces or isolated
backends for independent applications.

The version 1 envelope contains:

- `version`: integer 1;
- `expires_at`: absolute deadline in Unix nanoseconds, or zero;
- `values`: a non-nil `map[string][]byte` of individually encoded values.

The configured Codec encodes both the envelope and each value. It must round-trip
structs and byte strings as well as the application's values. Loading decodes
only the envelope. A custom JSON Codec is supported. Changing Codecs requires
clearing sessions or supplying a compatible decoder; automatic format migration
is not provided.

The library copies encoded byte results before retaining them and supplies
private buffers to Unmarshal. A Codec may reuse a Marshal buffer between
sequential calls, but cannot overwrite a returned buffer concurrently while its
caller is copying or decoding it. Unmarshal must not retain and later mutate
output objects. Marshal must not mutate application values. Callback panics
propagate. Empty encoded envelopes are invalid and must never reach Storage,
whose interface permits silently ignoring empty values.

## Persistence, expiry, and identifiers

Save encodes everything before writing storage. The TTL is the smaller of the
idle timeout and the remaining absolute lifetime. An already expired absolute
deadline produces `ErrExpired`; no nonpositive TTL is passed to Storage.
Backend TTL resolution still applies (memory/v2 uses seconds).

Only a successful storage write updates the client identifier. Failed writes
retain pending values and emit no new identifier. A backend error can occur
after a write took effect: this is not a transactional guarantee.

Destroy deletes the record and, after success, removes client identifiers and
invalidates the object. In a request, FromContext can create a replacement while
ignoring the old incoming ID. Zero-value and successfully destroyed Sessions are
invalid and must not be used.

Regenerate retains data and the absolute deadline. Reset clears data, restores
the configured idle timeout, and starts a new absolute lifetime. Both generate
and validate a different ID before deleting the old record; failed generation
or deletion leaves the in-memory state intact. The new ID is stored on Save,
not during rotation. Failure to save after successful rotation can therefore
leave the client without a usable session.

SetIdleTimeout requires a positive duration and changes this object's subsequent
saves, not the namespace configuration or future loads.

All four persistence operations have `WithContext` variants. Nil Go contexts
normalize to `context.Background()`. The selected context is passed to Storage;
the backend determines cancellation support.

Existing IDs are written back only to their resolved cookie/header source.
Read-only or unresolved sources are not promoted for an unchanged existing ID.
Fresh IDs use the resolved writable source, or the registered writable targets
when there is none. With a read-only extractor and no writable targets, callers
must deliver a new ID through their own response protocol.

## Background access

```go
func (m *Manager) GetByID(ctx context.Context, namespace, id string) (*Session, error)
func (m *Manager) DeleteByID(ctx context.Context, namespace, id string) error
```

GetByID returns a detached Session with no HTTP context, cookie writes, or
automatic save. An empty ID returns `ErrInvalidSessionID`. A missing or absolutely
expired record returns `ErrSessionNotFound`; a corrupt record remains an error,
not a new session. Reads do not renew expiry and do not delete expired records.

Explicit Save refreshes idle expiry just as it does during HTTP handling; the
absolute deadline remains unchanged. The context passed to GetByID is used only
for loading. No-argument operations on detached sessions use Background; use
WithContext methods to supply deadlines and cancellation.

Detached objects share the data and lifecycle API, including rotation and reset.
Callers are responsible for communicating any new ID to a client. Objects do
not require Release, are not pooled, and must not be used concurrently.

DeleteByID directly deletes the namespaced key without decoding it, so corrupt
records can be removed. Deleting a missing record succeeds. It cannot invalidate
already loaded objects or request containers; within a request use Session.Destroy
to prevent that object's automatic save.

## Error boundaries, concurrency, and ownership

Explicit operation errors return to the caller. Automatic save errors are sent
to the namespace's ErrorHandler; after a normal callback return, saving continues
with the next namespace. The default handler logs the error and responds with
HTTP 500. The downstream business error is then returned unchanged to Fiber and
may override the callback's response. Errors are not combined into one request
error. An ErrorHandler panic stops subsequent saves and propagates.

Error wrapping preserves `errors.Is` and `errors.As`, and identifies the namespace,
operation, and relevant key without adding IDs or values. Custom Codec or Storage
errors may themselves contain sensitive data; callback implementations control
their own error messages and logging.

Managers handle concurrent requests and background operations. A Session and its
pending values are not safe for concurrent use. Concurrent snapshots of one ID
use last-write-wins semantics, including stale snapshots overwriting updates or
recreating deleted/idle-expired records. Namespaces are saved independently;
multi-namespace commits, compare-and-swap, and atomic revocation are not promised.

Close is idempotent and closes only manager-created stores. It attempts every
owned close and preserves their errors. Drain all requests and background work
before calling it. Manager or Session use after Close is unsupported.
