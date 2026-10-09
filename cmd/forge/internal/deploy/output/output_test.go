package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xraph/forge/cli"
)

func TestFailCarriesExitCode(t *testing.T) {
	err := Fail(ExitUnresolved, "two decisions open", Diagnostic{Code: CodeDecisionOpen, Severity: SeverityError, Message: "x"})
	if cli.GetExitCode(err) != 3 {
		t.Fatalf("exit code %d", cli.GetExitCode(err))
	}

	var oe *Error
	if !errors.As(err, &oe) || len(oe.Diagnostics) != 1 || oe.Code != ExitUnresolved {
		t.Fatalf("%+v", err)
	}
}

func TestUnsupportedIsExit4(t *testing.T) {
	err := Unsupported("forge cloud deploy", "export the artifacts and use the provider's tool")
	if cli.GetExitCode(err) != 4 || !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestDiagnosticsSortedAndErrors(t *testing.T) {
	d := Diagnostics{
		{Code: "B", Severity: SeverityWarning, File: "b.yml", Line: 2},
		{Code: "A", Severity: SeverityError, File: "a.yml", Line: 9},
		{Code: "C", Severity: SeverityError, File: "a.yml", Line: 3},
	}

	s := d.Sorted()
	if s[0].Code != "C" || s[1].Code != "A" || s[2].Code != "B" {
		t.Fatalf("%v", s)
	}

	if len(d.Errors()) != 2 || !d.HasErrors() {
		t.Fatal("errors")
	}
}

func TestEmitJSONWritesOneDocument(t *testing.T) {
	var out bytes.Buffer

	ctx := testContext(t, &out)

	env := Envelope{Schema: SchemaVersion, Command: "inspect", OK: true, Data: map[string]any{"n": 1}}
	if err := Emit(ctx, Mode{JSON: true}, env); err != nil {
		t.Fatal(err)
	}

	var back Envelope
	if err := json.Unmarshal(out.Bytes(), &back); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, out.String())
	}

	if back.Diagnostics == nil {
		t.Fatal("diagnostics must serialise as [] not null")
	}
}

func TestEmitTextRendersDiagnosticsTable(t *testing.T) {
	var out bytes.Buffer

	ctx := testContext(t, &out)

	env := Envelope{Schema: SchemaVersion, Command: "doctor", OK: false,
		Diagnostics: Diagnostics{{Code: "DEPLOY_TOOL_MISSING", Severity: SeverityError, Message: "docker is not on PATH", Fix: "install docker"}}}
	if err := Emit(ctx, Mode{}, env); err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(out.Bytes(), []byte("DEPLOY_TOOL_MISSING")) || !bytes.Contains(out.Bytes(), []byte("install docker")) {
		t.Fatalf("%s", out.String())
	}
}

func testContext(t *testing.T, out *bytes.Buffer) cli.CommandContext {
	t.Helper()

	app := cli.New(cli.Config{Name: "t"})
	app.SetOutput(out)

	var captured cli.CommandContext

	err := app.AddCommand(cli.NewCommand("cmd", "test", func(ctx cli.CommandContext) error {
		captured = ctx

		return nil
	},
		cli.WithFlag(cli.NewStringFlag("output", "o", "format", "text")),
		cli.WithFlag(cli.NewBoolFlag("non-interactive", "", "no prompts", false)),
		cli.WithFlag(cli.NewBoolFlag("no-color", "", "no color", false))))
	if err != nil {
		t.Fatal(err)
	}

	if err := app.Run([]string{"t", "cmd"}); err != nil {
		t.Fatal(err)
	}

	return captured
}
