# Maintainer guide

This guide covers release preparation for `go.z2n.io/fiber/sessionmanager`.
Development and pull request guidance is in [CONTRIBUTING.md](../CONTRIBUTING.md).

## Prepare a release

1. Review the changes to public APIs, defaults, dependencies, and stored records.
   Keep the [specification](specs/0001-session-manager.md), usage guide, and examples
   consistent with the release. Record significant architecture decisions in `adr/`.
2. Run the full [validation suite](../CONTRIBUTING.md#validation) on the release
   commit and confirm the remote CI result. Run the examples using their READMEs;
   tests compile them but do not start every example program.
3. Review the current vulnerability scan, including whether an advisory affects
   imported packages or reachable symbols. A historical scan is not evidence about
   a new release or an updated advisory database.
4. Verify the [private reporting link](../SECURITY.md) and enable GitHub private
   vulnerability reporting for the repository if it is not already enabled.
5. Preserve the [Apache-2.0 license](../LICENSE) and applicable copyright notices.
   Prepare release notes covering behavior changes, compatibility, and migration
   steps. There is no automatic migration from Fiber Gob or legacy internal records.

## Publish the module

The module lives at the repository root. Before the first release, configure
`https://go.z2n.io/fiber/sessionmanager?go-get=1` to return this discovery metadata:

```html
<meta name="go-import" content="go.z2n.io/fiber/sessionmanager git https://github.com/z2n-oss/fiber-sessionmanager">
```

Verify that the endpoint is reachable and resolves to the intended repository.
Publish the reviewed commit and an appropriate semantic version tag. Root-module
tags have no subdirectory prefix; a future v2 requires the `/v2` module suffix.

From a fresh temporary module, install the specific published version with
`go get go.z2n.io/fiber/sessionmanager@<version>`. Use the vanity path and no local
replace directive. Check that package documentation and examples are available
for that version. Local builds alone do not verify module discovery or publication.

## Documentation maintenance

Keep the root README focused on installation and entry points. Put detailed usage
in the guide, runnable integrations in `examples/`, contracts in `specs/`, and
decision rationale in `adr/`. Link historical implementation records from the
documentation index, keeping their measurements dated.

Contributor expectations, including AI use, belong in CONTRIBUTING.md. Instructions
for coding agents belong in AGENTS.md. Update relative links when moving documents.
