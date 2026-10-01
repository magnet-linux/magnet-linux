local bootstrap = (import '../bootstrap.jsonnet').bootstrap;
local login = import './login.jsonnet';
{
  session: {
    name: 'magnet-graphical-session',
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      mkdir -p /out/bin /out/share/wayland-sessions
      cat > /out/bin/magnet-session <<'MAGNET_SESSION'
    ||| + (importstr '../../magnet-linux/magnet-session.sh') + |||
      MAGNET_SESSION
      cat > /out/bin/magnet-greetd <<'MAGNET_GREETD'
    ||| + (importstr '../../magnet-linux/magnet-greetd.sh') + |||
      MAGNET_GREETD
      chmod 0755 /out/bin/magnet-session /out/bin/magnet-greetd
      cat > /out/bin/magnet-audio-session <<'MAGNET_AUDIO_SESSION'
    ||| + (importstr '../../magnet-linux/magnet-audio-session.sh') + |||
      MAGNET_AUDIO_SESSION
      chmod 0755 /out/bin/magnet-audio-session
      cat > /out/share/wayland-sessions/magnet-hyprland.desktop <<'EOF'
      [Desktop Entry]
      Name=Hyprland (Magnet)
      Exec=magnet-session hyprland
      Type=Application
      EOF
      # Applications without an explicit theme use the same desktop default.
      mkdir -p /out/share/icons/default
      cat > /out/share/icons/default/index.theme <<'EOF'
      [Icon Theme]
      Inherits=Adwaita
      EOF
    ||| },
    buildDeps: [bootstrap],
    runDeps: [login.greetd, login.gtkgreet,
      (import '../adwaita-cursors.jsonnet').cursors,
      (import '../audio/pipewire.jsonnet').wireplumber,
      (import '../audio/libraries.jsonnet').pulse,
      (import '../audio/libraries.jsonnet').utils,
      (import '../base/init.jsonnet').perp,
      (import '../base/tools.jsonnet').shell_tools,
      (import '../base/tools.jsonnet').util_linux,
      (import '../hyprland/hyprland.jsonnet').hyprland,
      (import '../browser-common/libraries.jsonnet').dbus],
    fetch: [],
  },
}
