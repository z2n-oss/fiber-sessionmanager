# Security policy

Please report vulnerabilities privately through the repository's
[GitHub vulnerability reporting form](https://github.com/z2n-oss/fiber-sessionmanager/security/advisories/new).
If that form is unavailable, contact the repository owner through their GitHub
profile to request a private channel before sharing sensitive details. Do not
post session identifiers, credentials, or exploit details in public issues.

Include the affected version, Go/Fiber versions, storage provider, relevant
configuration, and a minimal reproduction with synthetic data. Maintainers will
assess reports against the documented contract and publish fixes and advisories
as appropriate. Use the latest published release for security fixes.

## Operational boundaries

- Enable CookieSecure and CookieHTTPOnly explicitly in production HTTPS deployments;
  both default to false. Apply application CSRF protection where required.
- Custom ID generators must produce unpredictable, unique identifiers. ID shape
  validation cannot establish entropy or prevent collisions across processes.
- Storage is trusted server-side data. Protect it with appropriate access controls
  and transport security. Codecs serialize data; this package does not encrypt it.
- Managers support concurrent requests, but Sessions and their pending reference
  values cannot be shared concurrently or retained beyond their documented lifetime.
- Generic fiber.Storage provides no compare-and-swap or transaction primitives.
  Concurrent snapshots can overwrite changes or recreate deleted records. Applications
  requiring immediate revocation against in-flight work need additional coordination.
- Applications and providers enforce size limits and backend TTL constraints.
- Codec, Storage, Extractor and ErrorHandler implementations are trusted callbacks.
  Ensure they are safe for concurrent calls and do not leak secrets through errors.

Integration guidance is in the [usage guide](docs/guide.md). The
[specification](docs/specs/0001-session-manager.md) defines the complete contract.
