# Usage guide

This guide covers configuration, data handling, lifecycle operations, and deployment
considerations. Start with the [runnable examples](../examples/README.md) for a
complete integration. The [specification](specs/0001-session-manager.md) defines the
full behavioral contract; `go doc -all .` shows the exported API from a checkout.

## Request integration

Register one Manager with `app.Use(manager.Handler())` before routes that access
sessions. Call `sessionmanager.FromContext(c, namespace)` inside a handler to load
or create that namespace's session. Repeated calls return the same object for that
request. Unaccessed namespaces perform no storage work or identifier writes.

Each accessed namespace is saved after the downstream handler returns, including
when the handler returns an error. Explicit `Save` does not disable this final save.
A second Manager in the same request chain returns `ErrManagerConflict`; re-entering
the same Manager is supported.

The [basic example](../examples/basic/README.md) demonstrates this flow with two
namespaces and a client cookie jar.

## Configuration

| Setting | Default / contract |
| --- | --- |
| Namespace | Required; ASCII letters, digits, `_` and `-`; unique per Manager |
| Storage | Independent official memory/v2 store per namespace |
| Extractor | `extractors.FromCookie("session_id_" + namespace)` |
| KeyGenerator | `utils.SecureToken`, with 32 random bytes |
| Marshal / Unmarshal | `cbor.Marshal` / `cbor.Unmarshal`; supply both or neither |
| ErrorHandler | Log an automatic save failure and respond with HTTP 500 |
| IdleTimeout | 30 minutes; refreshed by Save |
| AbsoluteTimeout | Disabled; when enabled, at least IdleTimeout |
| CookiePath / CookieDomain | `/` / empty (host-only) |
| CookieSameSite | `Lax`; also accepts `Strict`, `None`, `Disabled` |
| CookieSecure / CookieHTTPOnly | `false` / `false` |
| CookieSessionOnly | `false`; when true, client expiry attributes are omitted |

Invalid configuration panics during New, before default storage is allocated.
SameSite=None forces Secure. Cookie names, domains, paths and `__Host-` /
`__Secure-` requirements are validated. Cookie/header output locations cannot
overlap across namespaces; header names are case-insensitive.

The official `extractors` package can supply cookies, headers, chains or custom
readers. Existing IDs are written back to their resolved cookie/header source.
Read-only or unresolved sources are not promoted for an unchanged existing ID.
Fresh IDs use the resolved writable source or the configured writable targets.
If none exist, deliver `s.ID()` through your own response protocol. Avoid carrying
credentials in URLs, which can appear in history and logs.

Configuration, Extractor trees and callback configuration must remain unchanged
after New. Callbacks and supplied storage must support concurrent use.

## Typed values and custom Codecs

```go
s.Set("roles", []string{})

var roles []string
if err := s.Get("roles", &roles); err != nil {
    return err
}
```

Set retains the original value. Each Get and Save encodes pending values, even
after a previous explicit Save. Changes to a retained map, slice or pointer before
the next operation are therefore visible. Do not modify them concurrently.

Get decodes into your original target. Invalid targets and partial updates on
decode failure follow the Codec's native behavior. Missing keys and encoding
failures leave the target untouched. Modify a decoded value and call Set again
to save the change.

Use another Codec by providing both functions, for example:

```go
manager := sessionmanager.New(sessionmanager.Config{
    Namespace: "preferences",
    Marshal:   json.Marshal,
    Unmarshal: json.Unmarshal,
})
```

The Codec must handle both application values and an envelope containing an
integer version, a Unix nanosecond absolute deadline, and `map[string][]byte`.
The library protects stored byte buffers from decoder mutation and sequential
Marshal buffer reuse. A callback must not overwrite returned buffers concurrently
or later mutate objects it already returned. There is no configured record size
limit; enforce application limits where values enter the session.

Run the [custom Codec example](../examples/custom-codec/README.md) to observe a
JSON record round trip, including the distinction between nil and empty values.

## Lifecycle

| Operation | Behavior |
| --- | --- |
| `Get(key, target)` | Decode one key; return `ErrKeyNotFound` when absent |
| `Set(key, value)` | Retain a value until a later Get or Save encodes it |
| `Delete(key)` / `Keys()` | Remove a data key / return sorted data keys |
| `ID()` / `Fresh()` | Read the ID / whether this object created or rotated it |
| `Save()` | Write immediately, refresh idle expiry, then update HTTP identifiers |
| `Destroy()` | Delete the record and invalidate the object after success |
| `Regenerate()` | Delete the old ID, retain values and absolute deadline |
| `Reset()` | Delete the old ID, clear values, restore configured timeouts |
| `SetIdleTimeout(duration)` | Override this object's save TTL; nonpositive values panic |

Save, Destroy, Regenerate and Reset each have a `WithContext(ctx)` variant.
Normal request completion still saves after an explicit Save. New IDs from
Regenerate and Reset are persisted on the next Save; if that save fails, the old
record has already been deleted. Generation/validation and deletion failures
leave the original in-memory state intact. Generators must provide different,
unpredictable IDs; empty, transport-invalid or immediately reused IDs are rejected.

After successful Destroy, use FromContext again to create a new request session.
Do not reuse destroyed or manually constructed Session objects.

An absolute deadline never moves except on Reset. Save after that deadline returns
`ErrExpired`, and a lookup of an expired record treats it as missing. Backend TTL
resolution still applies; memory/v2 uses seconds. Changing namespace configuration
does not rewrite the absolute deadlines already stored in existing records.

## Background access

```go
s, err := manager.GetByID(ctx, "auth", id)
if errors.Is(err, sessionmanager.ErrSessionNotFound) {
    // The ID has no live record. This call does not create a session.
    return nil
}
if err != nil {
    return err
}
s.Set("flag", true)
return s.SaveWithContext(ctx)
```

To delete a record instead, use DeleteByID. Deletion does not need to decode the
record, and missing records are harmless:

```go
return manager.DeleteByID(ctx, "auth", id)
```

GetByID returns an independent, detached Session. It does not renew expiry,
automatically save, write cookies, or retain the loading Context. Explicit Save
refreshes idle expiry and preserves the absolute deadline. Detached no-argument
methods use `context.Background()`; WithContext methods pass the supplied context
to Storage. Nil contexts normalize to Background. Cancellation support depends on
the storage provider.

Detached sessions also support rotation and reset; the caller must deliver the new
ID to clients. There is no Release obligation, enumeration, user index or background
creation API. Within an HTTP request, use Session.Destroy to remove the loaded
object from automatic saving; Manager.DeleteByID cannot do that.

The [background example](../examples/background/README.md) runs a complete
load, update, reload, and deletion sequence outside HTTP handling.

## Storage, concurrency and errors

Set Config.Storage to an official provider or another `fiber.Storage`
implementation. Supplied stores remain caller-owned. Manager.Close is idempotent
and closes only stores the Manager created, after requests and background work
have drained. Close preserves errors from every owned store.

Records use `session:<namespace>:<id>`. Independent applications sharing a backend
must use distinct namespaces or storage isolation. Reusing a namespace across
Managers intentionally shares records and requires compatible configuration.
Changing a Codec requires clearing sessions or providing a compatible decoder;
neither Gob nor the original internal library has a migration guarantee.

Managers support concurrent use; individual Sessions and pending values do not.
Multiple snapshots of one ID use **last-write-wins** semantics. A delayed save can
overwrite another update or recreate a deleted/idle-expired record. DeleteByID
does not provide atomic revocation against in-flight requests. Namespace commits
are independent; a failed later save does not roll back an earlier one. Storage
errors may occur after a write took effect.

Explicit errors return to the caller and preserve `errors.Is` / `errors.As`.
Automatic save errors go to each namespace's ErrorHandler in registration order;
after a normal callback return, other namespaces continue saving. The original
business error is returned to Fiber and may override the callback's response.
Wrappers identify the namespace, operation and relevant data key without adding
session IDs or values; custom backend/Codec errors control their own contents.

Panics propagate and the request container is cleaned. Install Recover **before**
the Manager if a recovered panic should skip automatic saving. A Recover middleware
inside the Manager can turn a panic into a normal error return, which saves sessions.
Complete session operations before starting streaming/hijacking; do not access an
HTTP Session after the request or from a stream callback.
