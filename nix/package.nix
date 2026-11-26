{
  lib,
  buildGo127Module,
  buildNpmPackage,
  ffmpeg-headless,
  makeBinaryWrapper,
  nodejs_24,
  vips,
}:

let
  # The whole app, without what a build doesn't read (a new doc needn't rebuild anything).
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../CHANGELOG.md
      ../cmd
      ../go.mod
      ../go.sum
      ../internal
      ../locales
      ../web
    ];
  };

  # The version is the changelog's newest heading: the commit "Vitrine X.Y.Z" sets it. Anything since then (its heading
  # is "Unreleased") is a development build, which the update check never calls out of date.
  headings = lib.filter (l: lib.hasPrefix "## " l) (lib.splitString "\n" (builtins.readFile ../CHANGELOG.md));
  newest = builtins.match "## ([0-9]+\\.[0-9]+\\.[0-9]+(-hotfix)?) .*" (lib.head headings);
  version = if newest == null then "dev" else "v${lib.head newest}";

  # The web app (it reads ../locales and ../CHANGELOG.md, and builds into ../internal/server/web/app).
  web = buildNpmPackage {
    pname = "vitrine-web";
    inherit version src;
    sourceRoot = "${src.name}/web";
    nodejs = nodejs_24;
    npmDepsHash = "sha256-TQ/b8cMDT6wNbLdIu6vllqRNQwVp46ccl5EQCfvbBC4=";
    env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";
    preBuild = "chmod -R u+w ..";
    installPhase = ''
      runHook preInstall
      cp -r ../internal/server/web/app $out
      runHook postInstall
    '';
  };
in
buildGo127Module {
  pname = "vitrine";
  inherit version src;
  vendorHash = "sha256-uNfSO1vmZrIbIm4ccNk1WMfb9kC00Yik1TYiivguzdA=";
  subPackages = [ "cmd/vitrine" ];
  env.CGO_ENABLED = "0";
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];
  nativeBuildInputs = [ makeBinaryWrapper ];
  preBuild = ''
    rm -rf internal/server/web/app
    cp -r ${web} internal/server/web/app
    chmod -R u+w internal/server/web/app
  '';
  doCheck = false; # (they need the network's tools and a while: the repository's CI runs them)
  postFixup = ''
    wrapProgram $out/bin/vitrine \
      --set-default VITRINE_FFMPEG ${lib.getExe' ffmpeg-headless "ffmpeg"} \
      --set-default VITRINE_FFPROBE ${lib.getExe' ffmpeg-headless "ffprobe"} \
      --set-default VITRINE_VIPSTHUMBNAIL ${lib.getExe' vips "vipsthumbnail"}
  '';

  passthru = { inherit web; };

  meta = {
    description = "A fast, self-hosted media library with a built-in downloader and its own web app";
    homepage = "https://vitrine.software";
    license = lib.licenses.agpl3Plus;
    mainProgram = "vitrine";
    platforms = lib.platforms.linux;
  };
}
