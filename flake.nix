{
  description = "registries-conf: read containers-registries.conf(5) the way Podman, Buildah, Skopeo and CRI-O do";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      pkgsFor = system: nixpkgs.legacyPackages.${system};
    in
    {
      overlays.default = final: prev: {
        registries-conf = final.callPackage ./package.nix { };
      };

      packages = forAllSystems (system: rec {
        registries-conf = (pkgsFor system).callPackage ./package.nix { };
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
