package main

import (
	"github.com/xraph/forge"
	groveext "github.com/xraph/grove/extension"
	_ "github.com/xraph/grove/drivers/pgdriver"
	kvext "github.com/xraph/grove/kv/extension"
	_ "github.com/xraph/grove/kv/drivers/redisdriver"
)

func main() {
	app := forge.New(forge.WithAppName("worker"))
	_ = app.RegisterExtension(groveext.New())
	_ = app.RegisterExtension(kvext.New())
	_ = app.Run()
}
