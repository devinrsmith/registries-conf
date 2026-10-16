{ lib, buildGoModule }:

buildGoModule {
  pname = "registries-conf";
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [ ./go.mod ./go.sum ./cmd ];
  };

  vendorHash = "sha256-7chMaGkkSYh3JmTB0CualrMXAdG14w9jrTY+OlWWiFs=";

  subPackages = [ "cmd/registries-conf" ];

  # containers/image pulls in cgo-optional storage code we never touch;
  # a static, cgo-free binary is simpler to drop into images and scripts.
  env.CGO_ENABLED = 0;
  ldflags = [ "-s" "-w" ];

  meta = {
    description = "Read containers-registries.conf(5) the way Podman, Buildah, Skopeo and CRI-O do";
    license = lib.licenses.asl20;
    mainProgram = "registries-conf";
  };
}
