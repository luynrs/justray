# Usage:
#   nix run .#justray               # try it without installing (jray - short alias)
#   nix build .#justray              # build ./result/bin/{justray,jray,justrayd}
#
# NixOS (flake-based):
#   {
#     imports = [ justray.nixosModules.default ];
#     programs.justray.enable = true;    # sets up security.wrappers for TUN mode
#   }
#
# home-manager (flake-based):
#   {
#     imports = [ justray.homeManagerModules.default ];
#     services.justray.enable = true;    # runs justrayd as a systemd --user service
#     services.justray.execPath = "/run/wrappers/bin/justrayd"; # if TUN mode is needed
#   }
{
  description = "A modern VPN client that lives in your terminal";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;

      version = builtins.head (builtins.match ".*Version = \"([^\"]*)\".*" (builtins.readFile ./internal/shared/version/version.go));

      justrayFor = system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in pkgs.buildGoModule {
          pname = "justray";
          inherit version;

          src = pkgs.lib.fileset.toSource {
            root = ./.;
            fileset = pkgs.lib.fileset.unions [ ./go.mod ./go.sum ./cmd ./internal ./LICENSE ];
          };

          vendorHash = "sha256-K3IquEOFkc9E3E6xZTeZhMvz/2oWDz8oa6R4YHs/wsk=";
          proxyVendor = true;

          subPackages = [ "cmd/justray" "cmd/justrayd" ];
          tags = [ "with_quic" "with_utls" "with_gvisor" "with_grpc" ];
          ldflags = [ "-s" "-w" "-X" "github.com/luynrs/justray/internal/shared/version.Version=${version}" ];

          nativeBuildInputs = [ pkgs.installShellFiles ];

          postInstall = ''
            ln -s justray $out/bin/jray

            for cmd in justray jray; do
              installShellCompletion --cmd "$cmd" \
                --bash <($out/bin/$cmd completion bash) \
                --zsh <($out/bin/$cmd completion zsh) \
                --fish <($out/bin/$cmd completion fish)
            done
          '';

          meta = with pkgs.lib; {
            description = "A modern VPN client that lives in your terminal";
            homepage = "https://github.com/luynrs/justray";
            license = licenses.gpl3Plus;
            mainProgram = "justray";
            platforms = platforms.unix;
          };
        };
    in
    {
      packages = forAllSystems (system: {
        justray = justrayFor system;
        default = self.packages.${system}.justray;
      });

      apps = forAllSystems (system:
        let pkg = self.packages.${system}.justray; in {
          default = { type = "app"; program = nixpkgs.lib.getExe pkg; };
          justray = { type = "app"; program = nixpkgs.lib.getExe pkg; };
          jray = { type = "app"; program = nixpkgs.lib.getExe' pkg "jray"; };
          justrayd = { type = "app"; program = nixpkgs.lib.getExe' pkg "justrayd"; };
        });

      overlays.default = final: prev: {
        justray = self.packages.${final.stdenv.hostPlatform.system}.justray;
      };

      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in {
          default = pkgs.mkShell {
            packages = with pkgs; [ go gopls golangci-lint goreleaser ];
            GOFLAGS = "-tags=with_quic,with_utls,with_gvisor,with_grpc";
          };
        });

      nixosModules = rec {
        justray =
          { config, lib, pkgs, ... }:
          let
            cfg = config.programs.justray;
          in
          {
            options.programs.justray = {
              enable = lib.mkEnableOption "justray";

              package = lib.mkOption {
                type = lib.types.package;
                default = self.packages.${pkgs.stdenv.hostPlatform.system}.justray;
              };
            };

            config = lib.mkIf cfg.enable {
              environment.systemPackages = [ cfg.package ];

              security.wrappers.justrayd = lib.mkIf pkgs.stdenv.isLinux {
                source = "${cfg.package}/bin/justrayd";
                capabilities = "cap_net_admin+ep";
                owner = "root";
                group = "root";
              };
            };
          };
        default = justray;
      };

      homeManagerModules = rec {
        justray =
          { config, lib, pkgs, ... }:
          let
            cfg = config.services.justray;
          in
          {
            options.services.justray = {
              enable = lib.mkEnableOption "justray";

              package = lib.mkOption {
                type = lib.types.package;
                default = self.packages.${pkgs.stdenv.hostPlatform.system}.justray;
              };

              execPath = lib.mkOption {
                type = lib.types.str;
                default = "${cfg.package}/bin/justrayd";
              };
            };

            config = lib.mkIf cfg.enable {
              home.packages = [ cfg.package ];

              systemd.user.services.justrayd = lib.mkIf pkgs.stdenv.isLinux {
                Unit = {
                  After = [ "network-online.target" ];
                  Wants = [ "network-online.target" ];
                };
                Service = {
                  ExecStart = cfg.execPath;
                  Restart = "on-failure";
                  RestartSec = 3;
                };
                Install.WantedBy = [ "default.target" ];
              };
            };
          };
        default = justray;
      };
    };
}
