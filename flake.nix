
# Usage:
#   nix run .#justray               # try it without installing (jray is a short alias)
#   nix build .#justray              # build ./result/bin/{justray,jray,justrayd}
#
# home-manager (flake-based):
#   {
#     imports = [ justray.homeManagerModules.default ];
#     services.justray.enable = true;    # runs justrayd as a systemd --user service
#   }
{
  description = "A modern VPN client that lives in your terminal";

  inputs = {
    nixpkgs.url = "git+https://github.com/NixOS/nixpkgs.git?ref=nixos-unstable&shallow=1";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;

      version = "1.0.0";

      justrayFor = system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in pkgs.buildGoModule {
          pname = "justray";
          inherit version;
          src = ./.;

          vendorHash = "sha256-2b7BJ9r5iTu4wuoEzJkdmbmLvNVdgdkBcI+PvqAxpvQ=";

          subPackages = [ "cmd/justray" "cmd/justrayd" ];
          tags = [ "with_quic" "with_utls" "with_gvisor" ];
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
        default = justrayFor system;
      });

      apps = forAllSystems (system:
        let pkg = justrayFor system; in {
          justray = { type = "app"; program = "${pkg}/bin/justray"; };
          jray = { type = "app"; program = "${pkg}/bin/jray"; };
          justrayd = { type = "app"; program = "${pkg}/bin/justrayd"; };
          default = { type = "app"; program = "${pkg}/bin/justray"; };
        });

      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.mkShell {
            packages = [ pkgs.go pkgs.gopls pkgs.golangci-lint pkgs.goreleaser ];
            GOFLAGS = "-tags=with_quic,with_utls,with_gvisor";
          };
        }
      );

      homeManagerModules.default = { config, lib, pkgs, ... }:
        let
          cfg = config.services.justray;
          system = pkgs.stdenv.hostPlatform.system;
        in
        {
          options.services.justray = {
            enable = lib.mkEnableOption "the justray background daemon (justrayd), as a systemd --user service";

            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${system}.justray;
              defaultText = lib.literalExpression "justray.packages.<system>.default";
              description = "The justray package providing the justray/jray TUI and the justrayd daemon.";
            };

            execPath = lib.mkOption {
              type = lib.types.str;
              default = "${cfg.package}/bin/justrayd";
              defaultText = lib.literalExpression ''"''${cfg.package}/bin/justrayd"'';
            };
          };

          config = lib.mkIf cfg.enable {
            home.packages = [ cfg.package ];

            systemd.user.services.justrayd = {
              Unit = {
                Description = "justray background daemon";
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
    };
}
