package main

import (
	"example.com/atlas/internal/config"
	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
	_ "github.com/xraph/grove/drivers/pgdriver"
	_ "github.com/xraph/grove/drivers/pgdriver/pgmigrate"
	groveext "github.com/xraph/grove/extension"
	_ "github.com/xraph/grove/kv/drivers/redisdriver"
	kvext "github.com/xraph/grove/kv/extension"
	_ "github.com/xraph/trove/drivers/s3driver"
	troveext "github.com/xraph/trove/extension"
	"io"
	"os"
	"strings"
)

func main() {
	cli.RunApp(func(cli.CommandContext) (forge.App, error) {
		app := forge.New(forge.WithAppName("api"), forge.WithHTTPAddress(":"+port()))
		if err := app.RegisterExtension(groveext.New()); err != nil {
			return nil, err
		}
		if err := app.RegisterExtension(kvext.New()); err != nil {
			return nil, err
		}
		storage := troveext.New()
		if err := app.RegisterExtension(storage); err != nil {
			return nil, err
		}
		if err := app.Router().GET("/storage/probe", func(ctx forge.Context) error {
			if _, err := storage.Trove().Put(ctx.Context(), "atlas-uploads", "probe.txt", strings.NewReader("persisted-object")); err != nil {
				return err
			}
			return ctx.JSON(200, map[string]string{"stored": "probe.txt"})
		}); err != nil {
			return nil, err
		}
		if err := app.Router().GET("/storage/read", func(ctx forge.Context) error {
			object, err := storage.Trove().Get(ctx.Context(), "atlas-uploads", "probe.txt")
			if err != nil {
				return err
			}
			defer object.Close()
			raw, err := io.ReadAll(object)
			if err != nil {
				return err
			}
			return ctx.JSON(200, map[string]string{"body": string(raw)})
		}); err != nil {
			return nil, err
		}
		return app, nil
	})
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return config.HTTPPort
}
