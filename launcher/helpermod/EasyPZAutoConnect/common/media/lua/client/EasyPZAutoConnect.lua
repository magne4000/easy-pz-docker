-- EasyPZ Auto-Connect
--
-- The launcher writes ~/Zomboid/Lua/easypz-autoconnect.ini right before
-- starting the game. On the main menu this mod reads it, erases it (one-shot),
-- and connects the same way MultiplayerUI does for a saved account.
--
-- Keys (one key=value per line):
--   host, port            server address (required)
--   user                  account username (required)
--   password              account password: the stored hash when doHash=false,
--                         plain text when doHash=true
--   doHash                "false" (default) or "true"
--   serverPassword        server Password= (optional)
--   serverName            shown on the connecting screen (optional)
--   authType              1 = password (default)

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
    -- One-shot: truncate so a later normal launch does not auto-join.
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

-- Wait one front-end tick so vanilla's OnMainMenuEnter handler has built MainScreen.
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
