module github.com/magne4000/easy-pz-docker/launcher

go 1.27.0

require (
	github.com/andygrunwald/vdf v1.1.0
	github.com/magne4000/easy-pz-docker v0.0.0
	github.com/stretchr/testify v1.12.1
	github.com/wailsapp/wails/v3 v3.0.0-beta.26
	golang.org/x/crypto v0.57.0
	golang.org/x/mod v0.38.0
	modernc.org/sqlite v1.59.0
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.48.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// The launcher shares internal/publicapi and internal/pz/gamever with pzman.
replace github.com/magne4000/easy-pz-docker => ../
