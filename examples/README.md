# Examples

These small programs demonstrate the public API without external services. They
use the repository's root module, so no additional `go.mod` or replace directive
is needed. Use Go 1.26 or newer and run commands from the repository root.

| Example | Demonstrates | Run |
| --- | --- | --- |
| [Basic HTTP sessions](basic/README.md) | Cookies, independent namespaces, typed values, automatic saving, shutdown | `go run ./examples/basic` |
| [Custom Codec](custom-codec/README.md) | JSON serialization and nil/empty values across requests | `go run ./examples/custom-codec` |
| [Background access](background/README.md) | Loading, saving, and deleting an existing session outside a request | `go run ./examples/background` |

Each example includes complete source, commands, and expected results. The basic
example listens on `127.0.0.1:3000`. The others use Fiber's in-process request
helper and exit after printing their results; they do not open network listeners.

## Shared assumptions

All examples use default memory storage, so data is local to the process and is
lost when it exits. Separate application instances need a shared storage provider
to access the same sessions. Supplied providers remain caller-owned.

CookieSecure and CookieHTTPOnly both default to false in the library. The local
HTTP example explicitly enables HttpOnly and leaves Secure disabled; the examples
using simulated HTTPS requests enable both. Set the cookie policy for your actual
deployment, including application-level CSRF protection where needed.

Request-bound Sessions stay inside their handlers. Background work loads a
detached Session by ID and saves explicitly. A Session is not safe for concurrent
use, and stale snapshots can recreate deleted records. These examples demonstrate
session APIs; they do not implement authentication or atomic revocation.

`go test ./...` compiles all example programs and runs the separate GoDoc examples
in [`example_test.go`](../example_test.go). Run the commands above to exercise each
program. See the [usage guide](../docs/guide.md) for configuration and lifecycle
details and the [specification](../docs/specs/0001-session-manager.md) for exact
contracts.
