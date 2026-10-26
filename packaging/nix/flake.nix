# jack-ketch Nix flake: packages + Home Manager module.
# The primary dev box is NixOS; `services.jack-ketch.enable` installs
# the user units, matching `jack-ketch daemon install`.
{
  description = "jack-ketch: local async process supervisor for AI coding agents";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "0.2.1";
      in {
        packages = {
          jack-ketch = pkgs.buildGoModule {
            pname = "jack-ketch";
            inherit version;
            # This flake lives in packaging/nix; the Go module is the repo root.
            src = ../..;
            # Real hash (no vendor/ dir): refresh after go.mod/go.sum changes with
            #   nix build ./packaging/nix#jack-ketch  # read expected hash from error
            vendorHash = "sha256-KHIzOJhiJUSIiY1dKftznueIruWmReW2FrFxY3mizs8=";
            subPackages = [
              "cmd/jack-ketch"
              "cmd/jackketchd"
              "cmd/jack-ketch-shim"
            ];
            ldflags = [
              "-s"
              "-w"
              "-X jack-ketch/internal/mcp.Version=${version}"
              "-X main.version=${version}"
            ];
          };
          default = self.packages.${system}.jack-ketch;
        };
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            nodejs
          ];
        };
      }) // {
        homeManagerModules.default = { config, lib, pkgs, ... }:
          let cfg = config.services.jack-ketch; in {
            options.services.jack-ketch = {
              enable = lib.mkEnableOption "jack-ketch per-user daemon";
              package = lib.mkPackageOption pkgs "jack-ketch" { };
            };
            config = lib.mkIf cfg.enable {
              home.packages = [ cfg.package ];
              systemd.user.sockets.jackketchd = {
                Unit.Description = "jack-ketch daemon socket";
                Socket = {
                  ListenStream = "%t/jack-ketch/jackketchd.sock";
                  SocketMode = "0600";
                  DirectoryMode = "0700";
                };
                Install.WantedBy = [ "sockets.target" ];
              };
              systemd.user.services.jackketchd = {
                Unit = {
                  Description = "jack-ketch daemon";
                  Requires = [ "jackketchd.socket" ];
                  After = [ "jackketchd.socket" ];
                };
                Service = {
                  Type = "notify";
                  KillMode = "process";
                  Delegate = true;
                  Restart = "on-failure";
                  ExecStart = "${cfg.package}/bin/jackketchd --foreground";
                };
                Install.WantedBy = [ "default.target" ];
              };
            };
          };
      };
}
