package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/workbench"
	"golang.org/x/term"
)

func startFlags() []cli.CommandOption {
	return []cli.CommandOption{
		cli.WithFlag(cli.NewIntFlag("port", "", "Local page port (0 chooses an available port)", 0)),
		cli.WithFlag(cli.NewBoolFlag("no-open", "", "Print the URL without opening your browser", false)),
		cli.WithFlag(cli.NewStringFlag("token", "", "One-time sign-in token (at least 32 characters)", "")),
		cli.WithFlag(cli.NewStringFlag("store", "", "Settings authority: files, sqlite or postgres", "")),
		cli.WithFlag(cli.NewStringFlag("store-ref", "", "SQLite project file or PostgreSQL env:/file: reference", "")),
	}
}
func deploymentTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
func (p *DeployPlugin) start(ctx cli.CommandContext) error {
	if ctx.NArgs() != 0 {
		return output.Fail(output.ExitInvalidInput, "start does not accept positional arguments")
	}

	if port := ctx.Int("port"); port < 0 || port > 65535 {
		return output.Fail(output.ExitInvalidInput, "port must be between 0 and 65535")
	}

	if token := ctx.String("token"); token != "" && len(token) < 32 {
		return output.Fail(output.ExitInvalidInput, "token must have at least 32 characters")
	}

	backend, reference := ctx.String("store"), ctx.String("store-ref")
	if backend == "" && reference != "" {
		return output.Fail(output.ExitInvalidInput, "store-ref requires store")
	}

	if backend != "" && backend != "files" && backend != "sqlite" && backend != "postgres" {
		return output.Fail(output.ExitInvalidInput, "store must be files, sqlite or postgres")
	}

	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	if backend != "" {
		view, err := e.Files(ctx.Context())
		if err != nil {
			return err
		}

		if err := e.ConfigureStore(ctx.Context(), persistence.Options{Backend: backend, Reference: reference}, view.Hash); err != nil {
			return err
		}
	}

	s, err := workbench.New(workbench.Options{Engine: e, Root: e.Root(), Port: ctx.Int("port"), Token: ctx.String("token"), Timeout: time.Duration(ctx.Duration("timeout"))})
	if err != nil {
		return output.Fail(output.ExitInvalidInput, err.Error())
	}
	defer s.Close()

	ctxSignal, cancel := signal.NotifyContext(ctx.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if mode.JSON {
		if err := output.Emit(ctx, mode, output.Envelope{Command: "start", OK: true, Data: map[string]any{"url": s.URL(), "root": e.Root()}}); err != nil {
			return err
		}
	} else {
		ctx.Println("Deployment workbench: " + s.URL())
		ctx.Println("Press Ctrl+C to stop.")
	}

	if !ctx.Bool("no-open") && deploymentTTY() && !mode.JSON {
		if err := openDeploymentBrowser(ctxSignal, s.URL()); err != nil {
			ctx.Warning("Open the printed URL in your browser.")
		}
	}

	return s.Serve(ctxSignal)
}
func openDeploymentBrowser(ctx context.Context, url string) error {
	openCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var command *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(openCtx, "open", url)
	case "windows":
		command = exec.CommandContext(openCtx, "rundll32", "url.dll,FileProtocolHandler", url)
	case "linux":
		command = exec.CommandContext(openCtx, "xdg-open", url)
	default:
		return errors.New("browser launcher is unavailable")
	}

	return command.Run()
}
