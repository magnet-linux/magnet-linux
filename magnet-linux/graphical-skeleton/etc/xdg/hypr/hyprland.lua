-- Magnet's initial desktop. Copy to ~/.config/hypr/hyprland.lua to customize.
hl.monitor({ output = "", mode = "preferred", position = "auto", scale = 1 })
hl.config({
    general = { gaps_in = 5, gaps_out = 10, border_size = 2 },
    decoration = {
        rounding = 5,
        blur = { enabled = true, size = 3, passes = 1 },
        shadow = { enabled = true },
    },
    animations = { enabled = true },
    input = { kb_layout = "us", follow_mouse = 1 },
    misc = {
        disable_hyprland_logo = true, disable_splash_rendering = true,
        -- This minimal profile does not install the optional GUI dialog tools.
        disable_hyprland_guiutils_check = true, enable_anr_dialog = false,
    },
})
hl.on("hyprland.start", function ()
    -- Allow D-Bus to restart Mako with this compositor's Wayland socket.
    hl.exec_cmd("dbus-update-activation-environment WAYLAND_DISPLAY XDG_CURRENT_DESKTOP && magnet-notifications")
    hl.exec_cmd("waybar > \"${XDG_STATE_HOME:-$HOME/.local/state}/magnet-linux/waybar.log\" 2>&1")
    hl.exec_cmd("foot")
end)
hl.bind("ALT + Return", hl.dsp.exec_cmd("foot"))
hl.bind("ALT + D", hl.dsp.exec_cmd("fuzzel"))
hl.bind("XF86AudioRaiseVolume", hl.dsp.exec_cmd("wpctl set-volume -l 1 @DEFAULT_AUDIO_SINK@ 5%+"))
hl.bind("XF86AudioLowerVolume", hl.dsp.exec_cmd("wpctl set-volume @DEFAULT_AUDIO_SINK@ 5%-"))
hl.bind("XF86AudioMute", hl.dsp.exec_cmd("wpctl set-mute @DEFAULT_AUDIO_SINK@ toggle"))
hl.bind("XF86AudioMicMute", hl.dsp.exec_cmd("wpctl set-mute @DEFAULT_AUDIO_SOURCE@ toggle"))
-- Ordinary keys are useful when the host handles the media keys itself.
hl.bind("ALT + F10", hl.dsp.exec_cmd("wpctl set-mute @DEFAULT_AUDIO_SINK@ toggle"))
hl.bind("ALT + F11", hl.dsp.exec_cmd("wpctl set-volume @DEFAULT_AUDIO_SINK@ 5%-"))
hl.bind("ALT + F12", hl.dsp.exec_cmd("wpctl set-volume -l 1 @DEFAULT_AUDIO_SINK@ 5%+"))
hl.bind("ALT + SHIFT + Q", hl.dsp.window.close())
hl.bind("ALT + SHIFT + E", hl.dsp.exit())
hl.bind("ALT + F", hl.dsp.window.fullscreen())
hl.bind("ALT + left", hl.dsp.focus({ direction = "left" }))
hl.bind("ALT + right", hl.dsp.focus({ direction = "right" }))
hl.bind("ALT + up", hl.dsp.focus({ direction = "up" }))
hl.bind("ALT + down", hl.dsp.focus({ direction = "down" }))
hl.bind("ALT + SHIFT + left", hl.dsp.window.swap({ direction = "left" }))
hl.bind("ALT + SHIFT + right", hl.dsp.window.swap({ direction = "right" }))
hl.bind("ALT + SHIFT + up", hl.dsp.window.swap({ direction = "up" }))
hl.bind("ALT + SHIFT + down", hl.dsp.window.swap({ direction = "down" }))
for i = 1, 9 do
    hl.bind("ALT + " .. i, hl.dsp.focus({ workspace = i }))
end
