# SessionManager

Named, typed sessions for Go Fiber v3, with configurable serialization and explicit
background access.

- Multiple independent sessions in one request.
- Deferred value encoding and lazy value decoding.
- Pluggable Codecs and official `fiber.Storage` support.
- Automatic request-end saves and explicit background operations.

Requires Go **1.26+** and Fiber **v3.6.1+**. The module is
`go.z2n.io/fiber/sessionmanager`; the package name is `sessionmanager`.

## Why you may need this middleware

Consider an OpenID Connect (OIDC) backend-for-frontend (BFF). During sign-in, the
application needs two sessions with different purposes and lifetimes:

- A **login-flow session** that temporarily stores metadata during authentication.
- An **application session** that keeps the user signed in across subsequent requests.

The `/auth/callback` handler connects these two lifecycles. After successfully validating
the callback, it needs to remove the temporary login-flow session and establish
the application session in the same request. Each session needs its own identifier
and expiration policy.

SessionManager supports this workflow through independent, named sessions. A handler
can access `login` and `app` separately, destroy one, and save the other, with each
namespace configured independently.

Use it when a single request needs to manage sessions with distinct lifecycles.

## Install

```sh
go get go.z2n.io/fiber/sessionmanager
```

## Quick start

Register a Manager before the routes that use sessions:

```go
import (
    "errors"

    "github.com/gofiber/fiber/v3"
    "go.z2n.io/fiber/sessionmanager"
)

app := fiber.New()
manager := sessionmanager.New(sessionmanager.Config{
    Namespace:      "visits",
    CookieSecure:   true,
    CookieHTTPOnly: true,
})
// Call manager.Close() after requests and background work have drained.
app.Use(manager.Handler())

app.Get("/visits", func(c fiber.Ctx) error {
    s, err := sessionmanager.FromContext(c, "visits")
    if err != nil {
        return err
    }
    var count int
    if err := s.Get("count", &count); err != nil &&
        !errors.Is(err, sessionmanager.ErrKeyNotFound) {
        return err
    }
    count++
    s.Set("count", count)
    return c.JSON(fiber.Map{"visits": count})
})
```

The Manager saves accessed sessions after the handler returns. `Set` retains a
value until `Get` or `Save` encodes it; changes to decoded values require another
`Set` to persist them.

The snippet enables cookies for HTTPS. **CookieSecure and CookieHTTPOnly both
default to false**; enable them for production HTTPS applications. The
[basic example](examples/basic/README.md) runs on local HTTP and includes a complete
program, two namespaces, curl commands, and graceful shutdown:

```sh
# From a repository checkout:
go run ./examples/basic
```

## Documentation

| Task                                                               | Resource                                                               |
| ------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| Run an integration example                                         | [Examples](examples/README.md)                                         |
| Configure sessions, Codecs, expiry, storage, and background access | [Usage guide](docs/guide.md)                                           |
| Look up exact API and lifecycle behavior                           | [Specification](docs/specs/0001-session-manager.md) or `go doc -all .` |
| Understand design decisions                                        | [Architecture decision](docs/adr/0001-session-manager-design.md)       |
| Contribute code, tests, or documentation                           | [Contributing](CONTRIBUTING.md)                                        |
| Report a vulnerability                                             | [Security policy](SECURITY.md)                                         |

The [documentation index](docs/README.md) also links to maintainer guidance and
engineering history.

## Operational model

Storage defaults to memory local to the process. Supply a shared `fiber.Storage`
provider when several application instances need the same sessions. Supplied
stores remain caller-owned.

A Manager supports concurrent requests; an individual Session does not.
Concurrent snapshots use **last-write-wins** semantics: a delayed save can
overwrite updates or recreate a deleted record. See the
[concurrency and error contract](docs/guide.md#storage-concurrency-and-errors)
before using sessions for authentication or revocation.

The API and record format are independent of Fiber's built-in session middleware;
existing Gob records require an application-managed migration or new sessions.

Licensed under [Apache-2.0](LICENSE).
