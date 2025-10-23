module github.com/xraph/forge/extensions/mqtt

go 1.24.0

require (
	github.com/eclipse/paho.mqtt.golang v1.5.0
	github.com/xraph/forge v2.0.0
)

require (
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
)

replace github.com/xraph/forge => ../..
