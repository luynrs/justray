# Usage:
#   nix run .#justray               # try it without installing (jray - short alias)
#   nix build .#justray              # build ./result/bin/{justray,jray,justrayd}
#
# NixOS:        imports = [ justray.nixosModules.default ]; programs.justray.enable = true;
# home-manager: imports = [ justray.homeManagerModules.default ]; services.justray.enable = true;
{
  description = "A modern VPN client that lives in your terminal";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{ flake-parts, ... }:
    let
      version = builtins.head (
        builtins.match ".*Version = \"([^\"]*)\".*" (builtins.readFile ./internal/version/version.go)
      );
      tags = [
        "with_quic"
        "with_utls"
        "with_gvisor"
        "with_grpc"
        "with_xhttp"
        "badlinkname"
      ];
    in
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];

      perSystem =
        {
          self',
          pkgs,
          lib,
          ...
        }:
        let
          pkg = self'.packages.default;
          app = program: {
            type = "app";
            inherit program;
            inherit (pkg) meta;
          };
        in
        {
          packages.default = pkgs.buildGo127Module {
            pname = "justray";
            inherit version;

            src = lib.fileset.toSource {
              root = ./.;
              fileset = lib.fileset.unions [
                ./go.mod
                ./go.sum
                ./cmd
                ./internal
                ./LICENSE
              ];
            };

            vendorHash = "sha256-GrCNCexU8PTj1LBdbc3SyOeja+OopmEPiuJAPQ/9zWQ=";
            proxyVendor = true;

            subPackages = [
              "cmd/justray"
              "cmd/justrayd"
            ];
            inherit tags;
            ldflags = [
              "-s"
              "-w"
              "-X"
              "github.com/luynrs/justray/internal/version.Version=${version}"
            ];

            nativeBuildInputs = [ pkgs.installShellFiles ];

            postInstall = ''
              ln -s justray $out/bin/jray
            ''
            + lib.optionalString (pkgs.stdenv.buildPlatform.canExecute pkgs.stdenv.hostPlatform) ''
              for cmd in justray jray; do
                installShellCompletion --cmd "$cmd" \
                  --bash <($out/bin/$cmd completion bash) \
                  --zsh <($out/bin/$cmd completion zsh) \
                  --fish <($out/bin/$cmd completion fish)
              done
            '';

            meta = {
              description = "A modern VPN client that lives in your terminal";
              homepage = "https://github.com/luynrs/justray";
              license = lib.licenses.gpl3Plus;
              mainProgram = "justray";
              platforms = lib.platforms.unix;
            };
          };
          packages.justray = self'.packages.default;

          apps = {
            default = app (lib.getExe pkg);
            justray = app (lib.getExe pkg);
            jray = app (lib.getExe' pkg "jray");
            justrayd = app (lib.getExe' pkg "justrayd");
          };

          formatter = pkgs.nixfmt-tree;

          devShells.default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_27
              gopls
              golangci-lint
              goreleaser
            ];
            GOFLAGS = "-tags=${builtins.concatStringsSep "," tags}";
          };
        };

      flake = {
        overlays.default = final: prev: {
          justray = inputs.self.packages.${final.stdenv.hostPlatform.system}.justray;
        };

        nixosModules = rec {
          default =
            {
              config,
              lib,
              pkgs,
              ...
            }:
            let
              cfg = config.programs.justray;
            in
            {
              options.programs.justray = {
                enable = lib.mkEnableOption "justray";
                package = lib.mkOption {
                  type = lib.types.package;
                  default = pkgs.justray or inputs.self.packages.${pkgs.stdenv.hostPlatform.system}.justray;
                };
              };
              config = lib.mkIf cfg.enable {
                environment.systemPackages = [ cfg.package ];
                security.wrappers.justrayd = lib.mkIf pkgs.stdenv.hostPlatform.isLinux {
                  source = lib.getExe' cfg.package "justrayd";
                  capabilities = "cap_net_admin+ep";
                  owner = "root";
                  group = "root";
                };
              };
            };
          justray = default;
        };

        homeManagerModules = rec {
          default =
            {
              config,
              lib,
              pkgs,
              osConfig ? null,
              ...
            }:
            let
              cfg = config.services.justray;
            in
            {
              options.services.justray = {
                enable = lib.mkEnableOption "justray";
                package = lib.mkOption {
                  type = lib.types.package;
                  default = pkgs.justray or inputs.self.packages.${pkgs.stdenv.hostPlatform.system}.justray;
                };
                execPath = lib.mkOption {
                  type = lib.types.str;
                  default =
                    if osConfig != null && (osConfig.programs.justray.enable or false) then
                      "/run/wrappers/bin/justrayd"
                    else
                      lib.getExe' cfg.package "justrayd";
                };
              };
              config = lib.mkIf cfg.enable {
                home.packages = [ cfg.package ];
                systemd.user.services.justrayd = lib.mkIf pkgs.stdenv.hostPlatform.isLinux {
                  Unit = {
                    Description = "justray VPN daemon";
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
          justray = default;
        };
      };
    };
}
