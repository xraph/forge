package main

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
	_ "github.com/xraph/grove/drivers/pgdriver"
	groveext "github.com/xraph/grove/extension"
	_ "github.com/xraph/grove/kv/drivers/redisdriver"
	kvext "github.com/xraph/grove/kv/extension"
	"os"
)

func main() {
	cli.RunApp(func(cli.CommandContext) (forge.App, error) {
		app := forge.New(forge.WithAppName("worker"), forge.WithHTTPAddress(":"+port()))
		if err := app.RegisterExtension(groveext.New()); err != nil {
			return nil, err
		}
		if err := app.RegisterExtension(kvext.New()); err != nil {
			return nil, err
		}
		return app, nil
	})
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
