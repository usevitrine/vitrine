self:
{ pkgs, ... }:
{
  name = "vitrine";

  nodes.machine = {
    imports = [ self.nixosModules.default ];
    services.vitrine = {
      enable = true;
      filesDir = "/srv/vitrine-files";
      settings.renditions_max_gb = 1;
    };
    virtualisation.memorySize = 2048;
  };

  testScript = ''
    machine.wait_for_unit("vitrine.service")
    machine.wait_until_succeeds("curl -sf http://localhost:8080/ >/dev/null")

    # The command finds the library, as root too, and leaves nothing of root's in it.
    machine.succeed("vitrine config | grep /var/lib/vitrine")
    machine.fail("find /var/lib/vitrine /srv/vitrine-files -user root | grep .")

    # Thumbnails need ffmpeg's WebP encoder, images vipsthumbnail.
    machine.succeed("${pkgs.ffmpeg-headless}/bin/ffmpeg -hide_banner -encoders | grep libwebp")
    machine.succeed("${pkgs.vips}/bin/vipsthumbnail --vips-version")

    # A new version: a backup before it starts, once.
    machine.succeed("printf old > /var/lib/vitrine/.nixos-version")
    machine.succeed("systemctl restart vitrine")
    machine.wait_for_unit("vitrine.service")
    machine.succeed("ls /var/lib/vitrine/backups | grep .")
    count = machine.succeed("ls /var/lib/vitrine/backups | wc -l").strip()
    machine.succeed("systemctl restart vitrine")
    machine.wait_for_unit("vitrine.service")
    assert machine.succeed("ls /var/lib/vitrine/backups | wc -l").strip() == count

    # Restart now in Settings ends the program, and counts on being started again.
    machine.succeed("systemctl kill -s SIGTERM vitrine")
    machine.wait_until_succeeds("curl -sf http://localhost:8080/ >/dev/null")
  '';
}
