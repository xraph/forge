package plugins

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
)

func runCLI(t *testing.T, plugin cli.Plugin, args ...string) (string, error) {
	t.Helper()

	app := cli.New(cli.Config{Name: "forge", Version: "test"})

	var out bytes.Buffer
	app.SetOutput(&out)

	if err := app.RegisterPlugin(plugin); err != nil {
		t.Fatal(err)
	}

	err := app.Run(append([]string{"forge"}, args...))

	return out.String(), err
}

func TestCloudCommandsAreUnsupported(t *testing.T) {
	for _, sub := range [][]string{
		{"cloud", "deploy"}, {"cloud", "status"}, {"cloud", "login"}, {"cloud", "logout"},
		{"cloud", "logs", "--service", "x"}, {"cloud", "rollback", "--service", "x"}, {"cloud", "scale", "--service", "x"},
	} {
		t.Run(strings.Join(sub, " "), func(t *testing.T) {
			out, err := runCLI(t, NewCloudPlugin(nil), sub...)
			if cli.GetExitCode(err) != 4 {
				t.Fatalf("exit %d, out %q, err %v", cli.GetExitCode(err), out, err)
			}

			if strings.Contains(out, "✓") || strings.Contains(strings.ToLower(out), "success") {
				t.Fatalf("fixture success leaked: %q", out)
			}
		})
	}
}
