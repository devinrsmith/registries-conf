# Developing registries-conf

This covers building, testing and maintaining the tool. For what it does and
how to use it, see the [README](README.md).

## Layout

| Path | Contents |
| --- | --- |
| `cmd/registries-conf/main.go` | Subcommand dispatch, shared flags, `--format` handling |
| `cmd/registries-conf/resolve.go` | `resolve`: candidates and their ordered pull sources |
| `cmd/registries-conf/shortname.go` | Short-name expansion, ported from `pkg/shortnames` (see [below](#why-short-name-expansion-is-ported)) |
| `cmd/registries-conf/config.go` | `config` and `check` |
| `cmd/registries-conf/version.go` | `version` / `--version`, from link-time values and Go build info |
| `cmd/registries-conf/testdata/` | Example `registries.conf` and drop-in used by the tests and `nix flake check` |
| `package.nix` | The `buildGoModule` derivation, including `vendorHash` |
| `flake.nix`, `flake.lock` | Package, overlay, app, dev shell and checks; pins nixpkgs, and therefore the Go toolchain |

## Development environment

```sh
nix develop
```

This gives you Go, `gopls`, `govulncheck` and `nix-update`. It also sets
`CGO_ENABLED=0`, so local builds match the Nix package. Without Nix, use Go
1.25.7 or newer and export `CGO_ENABLED=0` yourself; otherwise Go looks for a C
compiler.

## Building

```sh
nix build                          # runs the Go tests, then ./result/bin/registries-conf
nix run . -- resolve nginx         # build and run in one step
go build -trimpath -ldflags="-s -w" ./cmd/registries-conf   # plain Go, same flags as the package
```

The binary is static, so cross-compiling with plain Go works directly:

```sh
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" ./cmd/registries-conf
```

The flake defines packages for x86_64/aarch64 Linux and Darwin. Building a
platform other than your own with `nix build .#packages.<system>.default` needs
a builder for that platform; the flake does not set up Nix cross-compilation.

## Testing

```sh
go test ./...       # unit tests, including the parity test against upstream
nix flake check     # the above in the Nix sandbox, plus the example-config check
```

Tests use their own `short-name-aliases.conf` in a temporary directory (see
`TestMain`). That keeps a developer's real recorded aliases out of the results,
and keeps the tests working in the Nix sandbox, where `$HOME` is not writable.

## Updating Go dependencies

Our one direct dependency is `go.podman.io/image/v5`; everything else comes in
through it.

```sh
nix develop

# 1. Bump. Usually just containers/image:
go get go.podman.io/image/v5@latest
#    or every module we compile against, to its latest minor/patch:
#    go get -u ./...
go mod tidy

# 2. Test.
go test ./...

# 3. Refresh vendorHash in package.nix.
nix-update --flake --version=skip default

# 4. Check everything in the sandbox.
nix flake check
```

Commit `go.mod`, `go.sum` and `package.nix` together.

`nix-update` replaces a stale `vendorHash` string but not the `lib.fakeHash`
placeholder. Without `nix-update`, set `vendorHash = lib.fakeHash;`, run
`nix build`, and copy the hash from the `got:` line of the error. Either way, a
wrong hash can't slip through: `nix build` fails with a hash mismatch.

To see what is out of date, limited to the modules that are actually compiled
in (a newer version is shown in brackets, e.g. `v1.9.4 [v1.10.2]`):

```sh
go list -deps -f '{{with .Module}}{{.Path}}{{end}}' ./cmd/registries-conf | sort -u \
  | xargs go list -m -u
```

`go list -m -u all` lists far more, because it includes modules containers/image
declares for features we never compile, such as signing, OpenTelemetry and
sqlite.

### The Go toolchain comes from nixpkgs

`go get` updates libraries. The compiler and standard library come from
nixpkgs, pinned in `flake.lock`:

```sh
nix flake update
nix flake check
```

- Run this periodically. Security fixes in the standard library (for example
  `crypto/tls`) arrive this way, not through `go.mod`.
- A containers/image release can require a newer Go, and `go get` raises the
  `go` line in `go.mod` to match. If that goes above the Go in the locked
  nixpkgs, `nix build` fails. Update `flake.lock` first.

### Vulnerability scanning

```sh
govulncheck ./...
```

It reports separately whether our code can reach a vulnerability, or only
imports or requires an affected package. Only the first category needs urgent
action. Standard-library findings are fixed by updating `flake.lock` once
nixpkgs ships the patched Go.

## What to check when bumping containers/image

Most of the tool uses containers/image's stable API. In these three places we
rely on more than that; each is guarded so that an upstream change fails a test
or the build instead of changing behavior silently.

| What | Where | Guarded by |
| --- | --- | --- |
| Short-name expansion ported from `pkg/shortnames` | `shortname.go` | `TestExpandShortNameMatchesUpstream` compares candidates, descriptions and error text with upstream |
| Parsing the `%q`-formatted string from `ConfigurationSourceDescription` | `configSources` in `config.go` | `TestConfigJSON` and `TestParseQuotedList`; an unrecognized format is an error, not a guess |
| `GetRegistries`, deprecated upstream | `loadConfig` in `config.go` | Fails to compile if removed. It is still the only way to list every `[[registry]]` table. |

If the parity test fails, upstream changed how short names resolve. Port the
change into `shortname.go`; don't loosen the test.

Also skim the release notes for `pkg/sysregistriesv2`, for example new
`registries.conf` keys that `config` should report.

### Binary size

The binary is about 3.7 MiB. A dependency that starts using `text/template` (or
`reflect`'s `Method`/`MethodByName`) forces the linker to keep every exported
method and adds about 1.6 MiB. After a bump:

```sh
nix build && ls -l result/bin/registries-conf
go list -deps ./cmd/registries-conf | grep -E 'text/template|promptui'   # expect no output
```

## Versions and releasing

`registries-conf version` (or `--version`) reports the tool's version and git
revision, the Go version, and the versions of `go.podman.io/image/v5` and
`go.podman.io/storage` that were compiled in. `--json` gives the same as JSON.
The dependency versions always come from Go's build info. The tool's own
version comes from different places depending on how it was built:

| Build | Version | Revision |
| --- | --- | --- |
| `nix build` / `nix run` | `version` in `package.nix`, passed with `-X main.version` | the flake's `self.rev`, or `self.dirtyRev` with uncommitted changes (shown as "modified") |
| `go install …@vX.Y.Z` | the git tag | none: Go records no VCS data for `go install` from the module proxy |
| `go build` in a checkout | a pseudo-version, e.g. `v0.0.0-20261019165301-5899c6fd28ee+dirty` | `vcs.revision` and `vcs.modified` |

Nix builds need the `-X` flags because the sandbox has no `.git` and Go sees the
module as `(devel)`. The `version` flake check makes sure the reported version
matches `package.nix`.

To release, keep the two sources in step:

1. Set `version` in `package.nix` (e.g. `0.2.0`) and commit.
2. Tag that commit `v0.2.0` and push the tag.

## Design notes

### Why short-name expansion is ported

`pkg/shortnames` imports `promptui` for its interactive registry picker, which
pulls in `text/template` and grows the binary from about 3.7 to 5.3 MiB. We
never prompt, so `shortname.go` reimplements only the non-interactive path,
using public `sysregistriesv2` functions (`ResolveShortNameAlias`,
`UnqualifiedSearchRegistriesWithOrigin`). Upstream is imported only by the
parity test, so it stays out of the binary.

### Why `sources` is parsed from a string

`sysregistriesv2` exposes the configuration search paths only through
`ConfigurationSourceDescription`, as `fmt.Sprintf("%q", paths)`. Calling
`go.podman.io/storage/pkg/configfile` directly would mean copying private
options such as the rootless UID lookup, which could drift from what Podman
reads. Parsing the string back is exact, and fails loudly if the format
changes.

## Automation

Renovate and Dependabot can open PRs that bump `go.mod`, but neither updates
`vendorHash`, so their PRs fail `nix build` until the hash is fixed. A scheduled
CI job that runs the four update steps above (with `nix-update` for the hash)
and opens a PR avoids that.
