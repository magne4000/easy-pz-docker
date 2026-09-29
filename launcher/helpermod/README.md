# EasyPZAutoConnect helper mod

A client-side mod the launcher installs into `~/Zomboid/mods` and enables for the main menu. When the game reaches the
main menu, it reads `~/Zomboid/Lua/easypz-autoconnect.ini`, erases the file (so it only runs once), and connects the same
way the game's multiplayer screen does for a saved account. It does nothing when the file is missing or empty.

The file uses one `key=value` per line:

| key | required | meaning |
|---|---|---|
| `host`, `port` | yes | server address |
| `user` | yes | account username |
| `password` | | stored hash when `doHash=false`, plain text when `doHash=true` |
| `doHash` | | `false` (default) or `true` |
| `serverPassword` | | the server's `Password=` |
| `serverName` | | shown on the connecting screen |
| `authType` | | `1` = password (default) |

## Manual test (plan milestone 1)

`~/Zomboid` is `%UserProfile%\Zomboid` on Windows.

### 0. Install and enable the mod

```bash
cp -R launcher/helpermod/EasyPZAutoConnect ~/Zomboid/mods/
```

Start the game with `-nosteam` (and no `+connect`). Go to **Mods**, enable **EasyPZ Auto-Connect** and go back to the
main menu. Then send the contents of `~/Zomboid/mods/default.txt`: the launcher will write this file itself, so the
format needs to be confirmed. The server's own mods must already be installed, as they would be after a launcher sync.

### A. Plain password

Close the game, then:

```bash
cat > ~/Zomboid/Lua/easypz-autoconnect.ini <<'EOF'
host=YOUR_HOST
port=16261
user=YOUR_USER
password=YOUR_PLAIN_PASSWORD
doHash=true
serverPassword=YOUR_SERVER_PASSWORD
serverName=Test
EOF
```

Start with `-nosteam`. Expected: the main menu shows briefly, then "Connecting…" and you join the server.
`~/Zomboid/console.txt` should contain `[EasyPZAutoConnect] connecting to …`.

### B. Stored hash (what the launcher will actually use)

Save the account in-game first (Join → server → account with "remember password" ticked), then read the hash:

```bash
sqlite3 ~/Zomboid/db/ServerList.db \
  "SELECT s.ip, s.port, a.username, a.password, a.isSavePassword, a.authType
   FROM server s JOIN account a ON a.serverId = s.id;"
```

Write the `.ini` as in A, but with `password=<the $2a$12$… value>` and `doHash=false`. Expected: you join the same way.

Optional: check the hash formula the launcher will use. It should print the same value as the database:

```bash
python3 -c 'import bcrypt,hashlib,sys; print(bcrypt.hashpw(hashlib.md5(sys.argv[1].encode()).hexdigest().encode(), b"$2a$12$O/BFHoDFPrfFaNPAACmWpu").decode())' 'YOUR_PLAIN_PASSWORD'
```

(`pip install bcrypt` if needed.)

### C. Failure and one-shot behaviour

1. Use a wrong password. Expected: the connect screen shows the error, and **Back** returns to a working main menu.
2. Restart the game normally. Expected: no auto-join, and `easypz-autoconnect.ini` is empty.
3. Leave the server and return to the main menu. Expected: no auto-join.

Report which steps passed, plus `default.txt` and any `EasyPZAutoConnect` or Lua error lines from `console.txt`.
