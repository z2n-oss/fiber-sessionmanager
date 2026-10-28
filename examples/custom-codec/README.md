# Custom Codec

[Source](main.go) · [All examples](../README.md)

This example replaces the default CBOR Codec with `encoding/json` and performs
two simulated HTTPS requests through Fiber. It prints the same result before
and after a storage round trip, preserving the distinction between an empty slice
and a nil slice.

## Run

From the repository root:

```sh
go run ./examples/custom-codec
```

Expected output:

```text
request 1: {"colors":[],"missing":null}
request 2: {"colors":[],"missing":null}
```

The program uses `app.Test` in process; it does not start a server or contact
`example.test`. The first request creates a session and sets the two values.
Its cookie is passed to the second request, which loads the saved record without
calling Set again.

## Codec configuration

Configure both functions together:

```go
sessionmanager.Config{
    Namespace: "preferences",
    Marshal:   json.Marshal,
    Unmarshal: json.Unmarshal,
}
```

These functions serialize both individual values and the versioned record
envelope containing `map[string][]byte`. They do not choose Fiber's HTTP response
encoding; `c.JSON` is a separate operation in this example.

Get receives the original target, so type errors and partial updates on decoding
failure follow the selected Codec. Mutating a decoded value does not update the
session until it is passed to Set. Pending reference values are re-encoded on
each Get and Save and must not be changed concurrently with those calls.

Changing the Codec does not migrate existing records. Use a compatible decoder
or start new sessions when changing formats. See the
[Codec contract](../../docs/specs/0001-session-manager.md#data-and-codec-contract)
for buffer ownership and callback requirements.
