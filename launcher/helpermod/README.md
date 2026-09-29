# EasyPZAutoConnect

Client mod installed by the launcher and enabled on the main menu. On the main
menu it reads `~/Zomboid/Lua/easypz-autoconnect.ini`, empties it, and joins the
server described there:

```
host=pz.example.com
port=16261
user=alice
password=<hash from ServerList.db, or plain text with doHash=true>
doHash=false
serverPassword=
serverName=My Server
```
