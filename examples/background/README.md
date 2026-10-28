# Background access

[Source](main.go) · [All examples](../README.md)

This example creates a session through a simulated HTTP request, then uses its ID
to read, update, reload, and delete it outside the request. Only the ID crosses
the boundary; the Fiber context and request-bound Session stay inside the handler.

## Run

From the repository root:

```sh
go run ./examples/background
```

Expected output:

```text
before: light
after: dark
deleted: true
```

The program uses `app.Test` in process and exits when the operations finish. It
does not listen on a port or contact `example.test`. The initial response cookie
supplies the ID solely for this demonstration; a real application obtains it
through its own authorized workflow.

## Lifecycle

1. The HTTP handler sets `theme` to `light`. The Manager automatically saves the
   session before the response becomes available.
2. `GetByID` loads an independent, detached Session. It does not create a missing
   record, renew expiry, or schedule a save. Missing or absolutely expired records
   return `ErrSessionNotFound`.
3. Set changes the detached object. `SaveWithContext` explicitly persists `dark`,
   refreshing idle expiry while preserving the stored absolute deadline.
4. A fresh `GetByID` verifies the saved value. `DeleteByID` removes the record
   without decoding it, and the final load checks `errors.Is` for the missing case.

The context used to load a Session is not retained. The example passes a context
to each storage operation. No-argument detached lifecycle methods use
`context.Background()`; cancellation support depends on the provider.

Deletion does not invalidate objects already loaded by another request or worker.
A later Save from either object can recreate the record, so the example does not
save its snapshots after deletion. Within a request, use Session.Destroy to also
remove that object from automatic saving. See the
[background contract](../../docs/specs/0001-session-manager.md#background-access)
and [concurrency guidance](../../docs/guide.md#storage-concurrency-and-errors).

Close the Manager after all requests and background work have drained. Its default
memory stores are owned by the Manager; a supplied storage provider is closed by
the application that owns it.
