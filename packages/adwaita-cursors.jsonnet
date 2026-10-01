local bootstrap = import './bootstrap.jsonnet';
{
  cursors: {
    name: 'adwaita-cursors-48.1',
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      tar -xf /fetch/adwaita-icon-theme-48.1.tar.xz
      cd adwaita-icon-theme-48.1
      mkdir -p /out/share/icons/Adwaita /out/share/licenses/adwaita-cursors
      # The release ships compiled Xcursor assets; no icon renderer is needed.
      cp -a Adwaita/cursors /out/share/icons/Adwaita/
      rm -f /out/share/icons/Adwaita/cursors/*.cur /out/share/icons/Adwaita/cursors/*.ani
      cp COPYING COPYING_CCBYSA3 COPYING_LGPL /out/share/licenses/adwaita-cursors/
      cat > /out/share/icons/Adwaita/index.theme <<'EOF'
      [Icon Theme]
      Name=Adwaita
      Comment=Adwaita cursors by the GNOME Project
      EOF
      # X11 aliases from upstream meson.build, also used by Wayland clients.
      cd /out/share/icons/Adwaita/cursors
      while read -r shape aliases; do
        for alias in $aliases; do ln -s "$shape" "$alias"; done
      done <<'EOF'
      all-resize fleur
      crosshair cross cross_reverse diamond_cross tcross
      default arrow dnd-move left_ptr top_left_arrow move
      e-resize right_side
      ew-resize sb_h_double_arrow
      grab hand1
      help question_arrow
      n-resize top_side
      ne-resize top_right_corner
      nesw-resize fd_double_arrow
      ns-resize sb_v_double_arrow
      nw-resize top_left_corner
      nwse-resize bd_double_arrow
      pointer hand2
      s-resize bottom_side
      se-resize bottom_right_corner
      sw-resize bottom_left_corner
      text xterm
      w-resize left_side
      wait watch
      EOF
    ||| },
    buildDeps: [bootstrap.bootstrap],
    runDeps: [bootstrap.root_layout],
    fetch: [{
      filename: 'adwaita-icon-theme-48.1.tar.xz',
      sha256: 'cbfe9b86ebcd14b03ba838c49829f7e86a7b132873803b90ac10be7d318a6e12',
      urls: ['https://download.gnome.org/sources/adwaita-icon-theme/48/adwaita-icon-theme-48.1.tar.xz'],
    }],
  },
}
