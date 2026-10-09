package main

import (
	"github.com/xraph/forge"
	groveext "github.com/xraph/grove/extension"
	_ "github.com/xraph/grove/drivers/pgdriver"
	kvext "github.com/xraph/grove/kv/extension"
	_ "github.com/xraph/grove/kv/drivers/redisdriver"
	troveext "github.com/xraph/trove/extension"
	_ "github.com/xraph/trove/drivers/s3driver"
)

func main() {
	app := forge.New(forge.WithAppName("api"))
	_ = app.RegisterExtension(groveext.New())
	_ = app.RegisterExtension(kvext.New())
	_ = app.RegisterExtension(troveext.New())
	_ = app.Run()
}
