{
  description = "registries-conf: read containers-registries.conf(5) the way Podman, Buildah, Skopeo and CRI-O do";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      # No x86_64-darwin: nixpkgs 26.11 (and so nixos-unstable) dropped it, and
      # its packages fail to evaluate. Intel Macs can use the overlay with
      # nixpkgs 26.05, or go install.
      forAllSystems = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
      pkgsFor = system: nixpkgs.legacyPackages.${system};
      # Reported by `registries-conf version`; dirtyRev ends in "-dirty".
      rev = self.rev or self.dirtyRev or null;
    in
    {
      overlays.default = final: prev: {
        registries-conf = final.callPackage ./package.nix { inherit rev; };
      };

      packages = forAllSystems (system: rec {
        registries-conf = (pkgsFor system).callPackage ./package.nix { inherit rev; };
        default = registries-conf;
      });

      apps = forAllSystems (system:
        let rc = self.packages.${system}.registries-conf; in {
          default = {
            type = "app";
            program = "${rc}/bin/registries-conf";
            meta = { inherit (rc.meta) description; };
          };
        });

      # `nix flake check` runs the Go tests (in the package build) and checks
      # that the bundled example configuration resolves as documented.
      checks = forAllSystems (system:
        let
          pkgs = pkgsFor system;
          rc = self.packages.${system}.registries-conf;
        in
        {
          package = rc;
          # The link-time version matches package.nix, and the containers/image
          # version is read from the build info.
          version = pkgs.runCommand "registries-conf-version" { nativeBuildInputs = [ rc pkgs.jq ]; } ''
            registries-conf version --json | tee version.json
            jq -e '.version == "${rc.version}"' version.json
            jq -e '.dependencies["go.podman.io/image/v5"] | startswith("v5.")' version.json
            [ "$(registries-conf --version | head -1)" = "registries-conf ${rc.version}" ]
            touch $out
          '';
          example = pkgs.runCommand "registries-conf-example" { nativeBuildInputs = [ rc ]; } ''
            export HOME=$TMPDIR
            conf=${./cmd/registries-conf/testdata}
            registries-conf check --registries-conf $conf/registries.conf --registries-conf-dir $conf/registries.conf.d
            got=$(registries-conf resolve --format plain \
              --registries-conf $conf/registries.conf --registries-conf-dir $conf/registries.conf.d \
              docker.io/library/nginx)
            want='mirror.internal:5000/dockerhub/library/nginx:latest
            registry-1.docker.io/library/nginx:latest'
            [ "$got" = "$(echo "$want" | sed 's/^ *//')" ] || { echo "unexpected: $got"; exit 1; }
            touch $out
          '';
        });

      devShells = forAllSystems (system:
        let pkgs = pkgsFor system; in {
          default = pkgs.mkShell {
            packages = [ pkgs.go pkgs.gopls pkgs.govulncheck pkgs.nix-update ];
            # Match package.nix: a static, cgo-free build.
            CGO_ENABLED = "0";
          };
        });
    };
}
