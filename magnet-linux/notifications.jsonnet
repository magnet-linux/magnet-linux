local n = import '../packages/notifications.jsonnet';
local mako = n.makoWithPostInstall(|||
    mkdir -p /out/bin
    # Mako only searches the home directory by default. The wrapper supplies
    # /etc defaults while retaining each user's configuration override.
    cat > /out/bin/magnet-notifications <<'MAGNET_NOTIFICATIONS'
||| + (importstr './magnet-notifications.sh') + |||
    MAGNET_NOTIFICATIONS
    chmod 0755 /out/bin/magnet-notifications
    sed -i 's@^Exec=.*@Exec=/bin/magnet-notifications@' \
      /out/share/dbus-1/services/fr.emersion.mako.service
|||);
{
  notifications: {
    name: 'magnet-notifications', build: { kind: 'none' },
    buildDeps: [], fetch: [], runDeps: [mako, n.libnotify],
  },
}
