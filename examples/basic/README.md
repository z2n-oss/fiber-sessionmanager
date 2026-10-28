# Basic HTTP sessions

[Source](main.go) · [All examples](../README.md)

This server uses two independent namespaces: `visits` stores a counter and
`preferences` stores a theme. Handlers load typed values with Get and change them
with Set. The Manager saves each accessed namespace when the handler returns.

## Run

From the repository root:

```sh
go run ./examples/basic
```

The server listens on `127.0.0.1:3000`. In a second terminal, use one temporary
cookie jar to carry the session IDs between requests:

```sh
sessionmanager_cookie_jar="$(mktemp)"
curl -sS -w '\n' -c "$sessionmanager_cookie_jar" http://127.0.0.1:3000/visits
curl -sS -w '\n' -b "$sessionmanager_cookie_jar" -c "$sessionmanager_cookie_jar" http://127.0.0.1:3000/visits
curl -sS -w '\n' -b "$sessionmanager_cookie_jar" -c "$sessionmanager_cookie_jar" http://127.0.0.1:3000/preferences
curl -sS -w '\n' -X PUT -b "$sessionmanager_cookie_jar" -c "$sessionmanager_cookie_jar" http://127.0.0.1:3000/preferences/dark
curl -sS -w '\n' -b "$sessionmanager_cookie_jar" -c "$sessionmanager_cookie_jar" http://127.0.0.1:3000/preferences
curl -sS -w '\n' -b "$sessionmanager_cookie_jar" -c "$sessionmanager_cookie_jar" http://127.0.0.1:3000/visits
rm "$sessionmanager_cookie_jar"
```

Starting with a new cookie jar, the responses are:

```text
{"visits":1}
{"visits":2}
{"theme":"light"}
{"theme":"dark"}
{"theme":"dark"}
{"visits":3}
```

Press Ctrl+C in the server terminal to stop accepting requests, wait for active
requests to finish, and close the Manager's memory stores. Restarting the process
clears the stored sessions.

## What to observe

Requests to `/visits` access only the `visits` namespace. They do not load or create
the `preferences` session, and updating a preference does not increment the visit
counter. Each namespace has its own cookie: `session_id_visits` or
`session_id_preferences`.

Omitting the cookie jar starts a new session. Supplying an unknown ID also creates
a new ID; the Manager does not adopt a client-selected ID without a live record.
Unsupported theme values return HTTP 400 before loading a session.

The example enables CookieHTTPOnly explicitly and leaves CookieSecure false for
local HTTP. Both flags default to false in the library. Enable Secure for HTTPS
deployments and apply the application's authorization and CSRF controls to writes.
The server is a session integration example, not an authentication implementation.

See the [usage guide](../../docs/guide.md) for expiry, storage, and lifecycle options.
