{
  description = "Google automation CLI and a scoped Go/CGO development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: import nixpkgs { inherit system; };
      packageFor =
        system:
        let
          pkgs = pkgsFor system;
        in
        pkgs.buildGoModule {
          pname = "google-automation";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-LXB9SHurRDxmfkGyh/NY6wGZeXZXWdYf1L11KoOznVY=";
          subPackages = [ "." ];
          env.CGO_ENABLED = "1";
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            go test ./...
            runHook postCheck
          '';
          meta.mainProgram = "google-automation";
        };
    in
    {
      packages = forSystems (system: {
        google-automation = packageFor system;
        default = self.packages.${system}.google-automation;
      });
      apps = forSystems (system: {
        default = {
          type = "app";
          meta.description = "Personal Google automation CLI";
          program = "${self.packages.${system}.default}/bin/google-automation";
        };
      });
      devShells = forSystems (
        system:
        let
          pkgs = pkgsFor system;
          # SQLite uses CGO. Scope its compiler to the Go child process rather
          # than exporting a Nix CC or library path into mise Python's environment.
          goWithCompiler = pkgs.symlinkJoin {
            name = "google-automation-go";
            paths = [ pkgs.go ];
            nativeBuildInputs = [ pkgs.makeWrapper ];
            postBuild = ''
              wrapProgram "$out/bin/go" \
                --set CC ${pkgs.stdenv.cc}/bin/cc \
                --set CGO_ENABLED 1
            '';
          };
        in
        {
          default = pkgs.mkShellNoCC {
            packages = [
              goWithCompiler
              pkgs.gopls
              pkgs.gotools
            ];
          };
        }
      );
      checks = forSystems (system: {
        cli = self.packages.${system}.default;
      });
      formatter = forSystems (system: (pkgsFor system).nixfmt);
    };
}
