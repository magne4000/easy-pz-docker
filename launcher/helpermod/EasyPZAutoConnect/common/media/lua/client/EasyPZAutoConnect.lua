-- Joins the server in ~/Zomboid/Lua/easypz-autoconnect.ini (written by the launcher), once.

local FILE = "easypz-autoconnect.ini"
local TAG = "[EasyPZAutoConnect] "

local function readConfig()
    local reader = getFileReader(FILE, false)
    if not reader then return nil end
    local cfg = {}
    while true do
        local line = reader:readLine()
        if not line then break end
        line = line:gsub("\r$", "")
        local eq = line:find("=", 1, true)
        if eq then
            cfg[line:sub(1, eq - 1)] = line:sub(eq + 1)
        end
    end
    reader:close()
    local writer = getFileWriter(FILE, true, false)
    writer:close()
    return cfg
end

local function connect(cfg)
    local main = MainScreen.instance
    if main.animPopup ~= nil then
        main.animPopup:removeFromUIManager()
    end
    main.bottomPanel:setVisible(false)
    local joypadData = JoypadState.getMainMenuJoypad()
    if joypadData then
        joypadData.focus = main
        updateJoypadFocus(joypadData)
    end
    print(TAG .. "connecting to " .. cfg.host .. ":" .. cfg.port .. " as " .. cfg.user)
    ConnectToServer.instance:connect(main.bottomPanel, cfg.serverName or "", cfg.user, cfg.password or "",
        cfg.host, "", cfg.port, cfg.serverPassword or "", false, cfg.doHash == "true",
        tonumber(cfg.authType) or 1)
end

-- Wait a tick: vanilla's OnMainMenuEnter handler builds MainScreen.
local function onFETick()
    Events.OnFETick.Remove(onFETick)
    if isIngameState() or not MainScreen.instance or not ConnectToServer.instance then return end
    local cfg = readConfig()
    if not cfg or not cfg.host or cfg.host == "" then return end
    if not cfg.port or not cfg.user then
        print(TAG .. "ignoring " .. FILE .. ": port and user are required")
        return
    end
    connect(cfg)
end

Events.OnMainMenuEnter.Add(function()
    Events.OnFETick.Add(onFETick)
end)
