package main

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
	"net/http"
	"os"
)

func main() {
	cli.RunApp(func(cli.CommandContext) (forge.App, error) {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		app := forge.New(forge.WithAppName("gateway"), forge.WithHTTPAddress(":"+port))
		if err := app.Router().GET("/probe", func(ctx forge.Context) error {
			url := os.Getenv("API_URL")
			request, err := http.NewRequestWithContext(ctx.Context(), http.MethodGet, url+"/_/health/ready", nil)
			if err != nil {
				return err
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			return ctx.JSON(response.StatusCode, map[string]string{"upstream": url})
		}); err != nil {
			return nil, err
		}
		return app, nil
	})
}
