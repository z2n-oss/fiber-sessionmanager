// Package sessionmanager provides named, typed sessions for Fiber v3 and explicit
// background access through the official fiber.Storage interface.
//
// Register namespaces with New and install Manager.Handler on the desired routes.
// FromContext loads each namespace on first access. Normal downstream return,
// including a business error, automatically saves accessed sessions in registration
// order. A propagating panic skips subsequent implicit saves. Install Recover
// before the Manager when recovered panics should not trigger automatic saving.
//
// # Data and Codecs
//
// Set retains a value and encodes it on each Get or Save. Get decodes directly into
// the supplied target, preserving the Codec's error and partial-mutation behavior.
// Changes to decoded results require Set. Pending reference values may be modified
// between calls, including after an explicit Save, but not concurrently.
//
// The default Codec is the unmodified cbor.Marshal/cbor.Unmarshal pair. Custom
// functions must encode both application values and a versioned envelope containing
// an absolute deadline and map[string][]byte. Loading decodes only that envelope;
// Get decodes only the requested value. The library copies byte results before
// retaining them and supplies private decoder input. Callbacks must not overwrite
// returned buffers concurrently or mutate previously returned output objects.
// Codec changes require clearing existing sessions or a compatible decoder.
//
// # Persistence and expiry
//
// Keys are session:<namespace>:<id>. Namespaces shared across Managers on one backend
// share data and require compatible configuration. Save renews idle expiry and is
// bounded by the stored absolute deadline. Reset starts a new absolute lifetime;
// Regenerate preserves it. Both validate a different ID before deleting the old
// record, then defer the replacement write until Save. Destroy invalidates the
// object; in a request, reacquire through FromContext to start a new session.
//
// # Background access and ownership
//
// Manager.GetByID returns a detached Session without HTTP side effects or automatic
// saving. Reads do not renew expiry. Explicit Save does refresh idle expiry; use
// SaveWithContext to supply cancellation or a deadline. The loading context is not
// retained; detached no-argument operations use context.Background(). DeleteByID
// removes a record without decoding it and cannot invalidate loaded snapshots.
// No Session needs Release. Close only releases manager-created storage, after
// all HTTP and background work has drained; supplied stores remain caller-owned.
//
// # Error and concurrency boundaries
//
// Invalid configuration panics. Explicit errors return to callers; automatic save
// errors go to the namespace ErrorHandler, then other namespaces continue saving.
// The downstream business error is returned unchanged to Fiber and can override
// an ErrorHandler's response. Codec and callback panics propagate.
//
// Managers support concurrent calls, but Session objects and pending values do not.
// HTTP sessions must not outlive the request or be used from stream callbacks.
// Concurrent snapshots of one ID use last-write-wins semantics, including possible
// recreation of deleted records by stale saves. Multi-namespace writes are not
// transactional. Applications and providers control record size and TTL precision.
//
// CookieSecure and CookieHTTPOnly default to false. Enable both for production
// HTTPS deployments. SameSite defaults to Lax and Path to /; None forces Secure.
package sessionmanager
