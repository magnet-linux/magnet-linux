// Hyprland-only desktop with graphical login; select browser additions here.
local desktop = (import './desktop-base.jsonnet').system {
  name: 'magnet-linux-desktop-hyprland',
  runDeps+: [(import '../packages/hyprland/hyprland.jsonnet').hyprland,
             (import '../packages/waybar/waybar.jsonnet').waybar,
             (import '../packages/fuzzel.jsonnet').fuzzel,
             (import '../packages/login/session.jsonnet').session,
             (import '../packages/network/network.jsonnet').networkmanager,
             (import '../packages/notifications.jsonnet').notifications,
             (import '../packages/base/tools.jsonnet').shadow],
};
{
  desktop: desktop,
  'desktop-browser': desktop {
    name: 'magnet-linux-desktop-browser-hyprland',
    runDeps+: [(import '../packages/firefox/firefox.jsonnet').firefox],
  },
  'desktop-chromium': desktop {
    name: 'magnet-linux-desktop-chromium-hyprland',
    runDeps+: [(import '../packages/chromium/chromium.jsonnet').chromium],
  },
  'desktop-browsers': desktop {
    name: 'magnet-linux-desktop-browsers-hyprland',
    runDeps+: [(import '../packages/firefox/firefox.jsonnet').firefox,
               (import '../packages/chromium/chromium.jsonnet').chromium],
  },
}
