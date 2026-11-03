{
  description = "Vitrine: a fast, self-hosted media library with a built-in downloader and its own web app";

  # (unstable: Vitrine needs a newer Go than the stable releases have. Its tools come from here too, so a system's own
  # nixpkgs doesn't have to follow this one.)
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAll (pkgs: rec {
        vitrine = pkgs.callPackage ./nix/package.nix { };
        default = vitrine;
      });

      nixosModules.default = import ./nix/module.nix self;

      checks = forAll (pkgs: {
        module = pkgs.testers.runNixOSTest (import ./nix/test.nix self);
      });

      formatter = forAll (pkgs: pkgs.nixfmt-rfc-style);
    };
}
