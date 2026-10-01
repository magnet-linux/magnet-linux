-- A dedicated compositor for the login screen, without desktop key bindings.
hl.monitor({ output = "", mode = "preferred", position = "auto", scale = 1 })
hl.config({
    general = { gaps_in = 0, gaps_out = 0, border_size = 0 },
    decoration = { rounding = 0, blur = { enabled = false }, shadow = { enabled = false } },
    animations = { enabled = false },
    input = { kb_layout = "us" },
    misc = {
        disable_hyprland_logo = true, disable_splash_rendering = true,
        disable_hyprland_guiutils_check = true, enable_anr_dialog = false,
    },
})
hl.window_rule({ name = "greeter", match = { class = "^gtkgreet$" }, fullscreen = true })
hl.on("hyprland.start", function ()
    -- Release the seat after authentication (or a greeter failure). greetd
    -- starts the user session or restarts the login screen as appropriate.
    hl.exec_cmd("gtkgreet --command 'magnet-session hyprland' --style /etc/greetd/style.css; hyprctl dispatch exit")
end)
