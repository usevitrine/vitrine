self:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.vitrine;
  up = cfg.updates;
  system = pkgs.stdenv.hostPlatform.system;
  settingsFile = (pkgs.formats.toml { }).generate "vitrine.toml" cfg.settings;

  requestDir = "/run/vitrine-update-request";
  request = "${requestDir}/request";
  statusDir = "/run/vitrine-update";
  status = "${statusDir}/status";
  jobWanted = up.button || up.automatic.enable;

  env = {
    VITRINE_CONFIG = toString settingsFile;
    VITRINE_DATA_DIR = cfg.dataDir;
    VITRINE_LISTEN = cfg.listen;
    VITRINE_HWACCEL = cfg.hwaccel;
    VITRINE_MANAGED_BY = if up.automatic.enable then "nixos-auto" else "nixos";
    HOME = cfg.dataDir;
  }
  // lib.optionalAttrs (cfg.filesDir != null) { VITRINE_FILES_DIR = cfg.filesDir; }
  // lib.optionalAttrs up.button {
    VITRINE_UPDATE_TRIGGER = request;
    VITRINE_UPDATE_STATUS = status;
  };

  # The vitrine command, finding this library; as the service's user, so that nothing it writes ends up root's.
  cli = pkgs.writeShellScriptBin "vitrine" ''
    ${lib.concatStringsSep "\n" (lib.mapAttrsToList (k: v: "export ${k}=${lib.escapeShellArg v}") env)}
    if [ "$(${pkgs.coreutils}/bin/id -u)" = 0 ] && [ ${cfg.user} != root ]; then
      exec ${pkgs.util-linux}/bin/runuser -u ${cfg.user} -- ${lib.getExe cfg.package} "$@"
    fi
    exec ${lib.getExe cfg.package} "$@"
  '';

  # A new version may change the database for good, and an older one then refuses it: a backup first, whatever brought
  # the new version (the Update button has made one: its mark says so). No backup, no start: as with the button.
  backupFirst = pkgs.writeShellScript "vitrine-backup-first" ''
    set -eu
    stamp=${lib.escapeShellArg cfg.dataDir}/.nixos-version
    new=${lib.escapeShellArg cfg.package.version}
    old=$(${pkgs.coreutils}/bin/cat "$stamp" 2>/dev/null || true)
    if [ "$old" != "$new" ] && [ -e ${lib.escapeShellArg cfg.dataDir}/meta.db ]; then
      if [ -n "$(${pkgs.findutils}/bin/find ${requestDir}/backed-up -mmin -240 2>/dev/null)" ]; then
        echo "backed up before the update already"
      else
        echo "Vitrine $old -> $new: backing up the library first"
        ${lib.getExe cfg.package} backup -before "$new" -unless-newer
      fi
    fi
    ${pkgs.coreutils}/bin/rm -f ${requestDir}/backed-up
    printf %s "$new" > "$stamp"
  '';

  # One job for the three ways an update starts (by hand, the timer, the Update button): only Vitrine's input moves.
  job = pkgs.writeShellScript "vitrine-update" ''
    set -u
    say() {
      printf '%s\n' "$1" > ${status}.new
      ${pkgs.coreutils}/bin/mv ${status}.new ${status}
    }
    ${pkgs.coreutils}/bin/rm -f ${request}
    say running
    # (the system's flake is often a Git repository of a user's, which Nix won't open as root unless told it's fine)
    export HOME=${statusDir}/home XDG_CACHE_HOME=/root/.cache
    ${pkgs.coreutils}/bin/mkdir -p "$HOME"
    printf '[safe]\n\tdirectory = %s\n' ${lib.escapeShellArg up.flake} > "$HOME/.gitconfig"
    lock=${lib.escapeShellArg up.flake}/flake.lock
    if ! ${pkgs.coreutils}/bin/cp "$lock" ${statusDir}/flake.lock.before; then
      say failed
      exit 1
    fi
    if ! nix --extra-experimental-features 'nix-command flakes' flake update ${lib.escapeShellArg up.input} \
      --flake ${lib.escapeShellArg up.flake}; then
      say failed
      exit 1
    fi
    # (still its owner's to commit or change)
    ${pkgs.coreutils}/bin/chown --reference=${lib.escapeShellArg up.flake}/flake.nix "$lock"
    if ${pkgs.diffutils}/bin/cmp -s "$lock" ${statusDir}/flake.lock.before; then
      echo "already on the newest version"
      say unchanged
      exit 0
    fi
    if ! nixos-rebuild switch --flake ${lib.escapeShellArg "${up.flake}#${up.host}"}; then
      ${pkgs.coreutils}/bin/cp ${statusDir}/flake.lock.before "$lock"
      say failed
      exit 1
    fi
    say done
  '';
in
{
  options.services.vitrine = {
    enable = lib.mkEnableOption "Vitrine, a self-hosted media library";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${system}.default;
      defaultText = lib.literalExpression "vitrine.packages.\${system}.default";
      description = "The Vitrine package.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "vitrine";
      description = "The user Vitrine runs as (created unless it's another one you made).";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "vitrine";
      description = "The group Vitrine runs as.";
    };

    dataDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/vitrine";
      description = "The databases, thumbnails, plugins and site logins: fast storage (an SSD).";
    };

    filesDir = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/mnt/bulk/vitrine";
      description = "The original files: can be a big, slow disk. Default: `files` in dataDir.";
    };

    listen = lib.mkOption {
      type = lib.types.str;
      default = ":8080";
      example = "127.0.0.1:8080";
      description = "The address to serve on.";
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Open the port in `listen` in the firewall.";
    };

    hwaccel = lib.mkOption {
      type = lib.types.str;
      default = "auto";
      example = "off";
      description = ''
        The graphics hardware videos are decoded with: `auto`, `off`, `vaapi`, `vaapi:/dev/dri/renderD129` or `cuda`.
        Intel and AMD also need their driver in `hardware.graphics.extraPackages`, e.g. `intel-media-driver`.
      '';
    };

    settings = lib.mkOption {
      type = (pkgs.formats.toml { }).type;
      default = { };
      example = {
        renditions_max_gb = 20;
        language = "en-GB";
      };
      description = "Other settings, as in config.example.toml.";
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "A file of VITRINE_* variables that shouldn't be in the Nix store.";
    };

    updates = {
      flake = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        example = "/etc/nixos";
        description = "The folder of the system's flake: needed by `button` and `automatic`.";
      };

      input = lib.mkOption {
        type = lib.types.str;
        default = "vitrine";
        description = "Vitrine's name among that flake's inputs: the only one an update moves.";
      };

      host = lib.mkOption {
        type = lib.types.str;
        default = config.networking.hostName;
        defaultText = lib.literalExpression "config.networking.hostName";
        description = "This system's name in that flake's nixosConfigurations.";
      };

      button = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = ''
          An Update button in Settings > Updates. It starts one fixed job as root (vitrine-update.service), which moves
          the input and rebuilds the system from the flake as it stands - so with whatever else you've left uncommitted
          in it. Vitrine can't change what the job does.
        '';
      };

      automatic = {
        enable = lib.mkEnableOption "installing new versions by themselves, on a timer (the same job as the button)";

        dates = lib.mkOption {
          type = lib.types.str;
          default = "04:00";
          description = "When to look for a new version (systemd's OnCalendar).";
        };
      };
    };
  };

  config = lib.mkIf cfg.enable (
    lib.mkMerge [
      {
        assertions = [
          {
            assertion = !jobWanted || up.flake != null;
            message = "services.vitrine.updates.button and .automatic need services.vitrine.updates.flake.";
          }
        ];

        users.users = lib.mkIf (cfg.user == "vitrine") {
          vitrine = {
            isSystemUser = true;
            group = cfg.group;
            home = cfg.dataDir;
          };
        };
        users.groups = lib.mkIf (cfg.group == "vitrine") { vitrine = { }; };

        environment.systemPackages = [ cli ];

        # (the drivers' folder that ffmpeg's VA-API looks in)
        hardware.graphics.enable = lib.mkDefault true;

        networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [
          (lib.toInt (lib.last (lib.splitString ":" cfg.listen)))
        ];

        systemd.tmpfiles.rules = [
          "d ${cfg.dataDir} 0750 ${cfg.user} ${cfg.group} -"
        ]
        ++ lib.optional (cfg.filesDir != null) "d ${cfg.filesDir} 0750 ${cfg.user} ${cfg.group} -"
        ++ [ "d ${requestDir} 0750 ${cfg.user} ${cfg.group} -" ];

        systemd.services.vitrine = {
          description = "Vitrine";
          wantedBy = [ "multi-user.target" ];
          wants = [ "network-online.target" ];
          after = [ "network-online.target" ];
          environment = env;
          serviceConfig = {
            ExecStartPre = backupFirst;
            ExecStart = "${lib.getExe cfg.package} serve";
            User = cfg.user;
            Group = cfg.group;
            SupplementaryGroups = [
              "render"
              "video"
            ];
            WorkingDirectory = cfg.dataDir;
            EnvironmentFile = lib.mkIf (cfg.environmentFile != null) cfg.environmentFile;
            # (always: Settings > Backups' Restart now ends the program and counts on this)
            Restart = "always";
            RestartSec = 5;
            # (a big library's backup before an update, and opening it)
            TimeoutStartSec = "1h";
            # (finishes the current request and stops downloads between files)
            TimeoutStopSec = "2min";
            UMask = "0027";

            NoNewPrivileges = true;
            ProtectSystem = "strict";
            ReadWritePaths = [
              cfg.dataDir
              requestDir
            ]
            ++ lib.optional (cfg.filesDir != null) cfg.filesDir;
            ProtectHome = true;
            PrivateTmp = true;
            ProtectKernelTunables = true;
            ProtectKernelModules = true;
            ProtectKernelLogs = true;
            ProtectControlGroups = true;
            ProtectClock = true;
            ProtectHostname = true;
            RestrictSUIDSGID = true;
            RestrictNamespaces = true;
            RestrictRealtime = true;
            LockPersonality = true;
            CapabilityBoundingSet = "";
            AmbientCapabilities = "";
            RestrictAddressFamilies = [
              "AF_INET"
              "AF_INET6"
              "AF_UNIX"
            ];
          };
        };
      }

      (lib.mkIf jobWanted {
        systemd.tmpfiles.rules = [ "d ${statusDir} 0755 root root -" ];

        systemd.services.vitrine-update = {
          description = "Update Vitrine";
          after = [ "network-online.target" ];
          wants = [ "network-online.target" ];
          # (it mustn't stop itself when it switches the system: as system.autoUpgrade)
          restartIfChanged = false;
          stopIfChanged = false;
          unitConfig.X-StopOnRemoval = false;
          path = [
            pkgs.coreutils
            pkgs.gitMinimal
            pkgs.gnutar
            pkgs.gzip
            pkgs.openssh
            pkgs.xz
            config.nix.package.out
            config.system.build.nixos-rebuild
          ];
          environment = config.nix.envVars // {
            HOME = "/root";
            inherit (config.environment.sessionVariables) NIX_PATH;
          };
          serviceConfig = {
            Type = "oneshot";
            ExecStart = job;
          };
        };
      })

      (lib.mkIf up.button {
        systemd.paths.vitrine-update = {
          description = "The Update button in Vitrine";
          wantedBy = [ "multi-user.target" ];
          pathConfig.PathExists = request;
        };
      })

      (lib.mkIf up.automatic.enable {
        systemd.timers.vitrine-update = {
          description = "Look for a new version of Vitrine";
          wantedBy = [ "timers.target" ];
          timerConfig = {
            OnCalendar = up.automatic.dates;
            Persistent = true;
            RandomizedDelaySec = "30min";
          };
        };
      })
    ]
  );
}
