# registries-conf

A small CLI that reads `containers-registries.conf(5)` **exactly** the way
Podman, Buildah, Skopeo and CRI-O do. It does that by calling the same Go code
they use, [`go.podman.io/image/v5/pkg/sysregistriesv2`][image] (formerly
`github.com/containers/image/v5`). That package handles config loading and
merging, `[[registry]]` prefix matching, mirrors, `blocked`, `insecure`,
`mirror-by-digest-only` / `pull-from-mirror`, prefix → location rewriting, and
short-name aliases.

The tool's answers therefore match what those tools would do with the same
config. This includes:

- `registries.conf.d/*.conf` drop-ins
- `$HOME/.config/containers/registries.conf`
- `$CONTAINERS_REGISTRIES_CONF`
- the recorded `short-name-aliases.conf`

The one piece not called directly is the short-name expansion step from
`pkg/shortnames`. It's ported to keep the binary small, and a test checks it
against upstream. See [DEVELOPMENT.md](DEVELOPMENT.md#why-short-name-expansion-is-ported).

[image]: https://pkg.go.dev/go.podman.io/image/v5/pkg/sysregistriesv2

## Usage

```console
$ registries-conf resolve nginx
nginx
  # Resolving "nginx" using unqualified-search registries (/etc/containers/registries.conf)
  quay.io/nginx:latest
    primary quay.io/nginx:latest
  docker.io/library/nginx:latest
    mirror  mirror.internal:5000/dockerhub/library/nginx:latest (insecure)
    primary registry-1.docker.io/library/nginx:latest

$ registries-conf resolve --format plain docker.io/library/nginx   # try-order, one per line
mirror.internal:5000/dockerhub/library/nginx:latest
registry-1.docker.io/library/nginx:latest

$ registries-conf config --format json   # effective merged config
$ registries-conf check --json           # {"ok": true, "sources": [...]}
$ registries-conf check              # exit 1 if the config is invalid
```

Every command accepts:

| flag | containers/image equivalent |
| --- | --- |
| `--registries-conf PATH` | `SystemContext.SystemRegistriesConfPath` (`--registries-conf` in skopeo/podman) |
| `--registries-conf-dir PATH` | `SystemContext.SystemRegistriesConfDirPath` |
| `--root PATH` | `SystemContext.RootForImplicitAbsolutePaths`, to inspect another rootfs |
| `--short-name-aliases PATH` | `SystemContext.UserShortNameAliasConfPath` |
| `--format text\|json\|plain` | output format (default `text`; `plain` is `resolve` only). `--json` is shorthand for `--format json` |

In `resolve --format json`, each input has a `resolvedBy` field. It is
`"qualified"` when the input already named a registry, `"alias"` when a short
name matched an alias, or `"unqualified-search"` when it was combined with each
unqualified-search registry. `origin` names the file the alias or search list
came from. For an alias, that path tells a configured alias in
`registries.conf(.d)` apart from one Podman recorded in `short-name-aliases.conf`.

`sources` (in `config` and `check` JSON) lists the paths searched for
configuration: the main `registries.conf` candidates and then the drop-in
directories, each highest priority first. Paths that do not exist are still
listed.

Short names are always resolved without prompting. Every candidate is listed,
the same as `short-name-mode = "disabled"`. The configured mode is shown by
`config`.

## Nix

```sh
nix run github:devinsmith/registries-conf -- resolve nginx
```

The flake exports `packages.<system>.default`, `overlays.default` (adds
`pkgs.registries-conf`) and `apps.<system>.default`. The binary is static, with no
runtime dependencies. To build it yourself, see
[DEVELOPMENT.md](DEVELOPMENT.md#building).

A typical use is validating a generated config at build time. In a NixOS
configuration:

```nix
system.checks = [
  (pkgs.runCommand "check-registries-conf" { nativeBuildInputs = [ pkgs.registries-conf ]; } ''
    export HOME=$TMPDIR
    registries-conf check \
      --registries-conf ${config.environment.etc."containers/registries.conf".source} \
      --registries-conf-dir /var/empty
    touch $out
  '')
];
```

When `--short-name-aliases` is not given, short-name resolution reads (and may
create the directory for) `$XDG_CACHE_HOME/containers/short-name-aliases.conf`.
That is why `HOME` must be writable inside a Nix sandbox.

## Why a binary rather than a Go library

Go programs gain nothing from a wrapper: they can import `sysregistriesv2`
directly, and this repo is a worked example of how. Everything else (shell
scripts, Nix derivations, Python, CI) cannot link against Go. For those
consumers a small, static binary with stable JSON output is the useful shape.

## Development

Building from source, testing, updating dependencies and design notes are in
[DEVELOPMENT.md](DEVELOPMENT.md).

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
