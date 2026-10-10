//go:build integration

package hosted

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHostedActualCtrlplaneContracts(t *testing.T) {
	checkout := os.Getenv("FORGE_DEPLOY_CTRLPLANE_ROOT")
	if checkout == "" {
		t.Skip("set FORGE_DEPLOY_CTRLPLANE_ROOT to the ctrlplane checkout for typed conformance")
	}
	checkout, e := filepath.Abs(checkout)
	if e != nil {
		t.Fatal(e)
	}
	mod, e := os.ReadFile(filepath.Join(checkout, "go.mod"))
	if e != nil {
		t.Fatal(e)
	}
	sums, e := os.ReadFile(filepath.Join(checkout, "go.sum"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(string(mod), "module github.com/xraph/ctrlplane\n") {
		t.Fatal("expected ctrlplane module")
	}
	dir := t.TempDir()
	mod = []byte(strings.Replace(string(mod), "module github.com/xraph/ctrlplane", "module example.com/forge-contract-check", 1) + "\nrequire github.com/xraph/ctrlplane v0.0.0\nreplace github.com/xraph/ctrlplane => " + strconv.Quote(checkout) + "\n")
	for name, data := range map[string][]byte{"go.mod": mod, "go.sum": sums, "main.go": []byte(contractProgram)} {
		if e = os.WriteFile(filepath.Join(dir, name), data, 0o600); e != nil {
			t.Fatal(e)
		}
	}
	bundle, e := New(dir).Render(t.Context(), fixture())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "export.json"), bundle.Files["forge-hosted.json"].Content, 0o600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(t.Context(), "go", "run", "-mod=readonly", ".", "export.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("actual ctrlplane contract: %v\n%s", e, out)
	}
}

const contractProgram = `package main
import("bytes";"encoding/json";"fmt";"os";p "github.com/xraph/ctrlplane/provider")
func strict(raw json.RawMessage,v any){d:=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields();if e:=d.Decode(v);e!=nil{panic(e)}}
func main(){raw,e:=os.ReadFile(os.Args[1]);if e!=nil{panic(e)};var export struct{Workloads []struct{Services []json.RawMessage;SecretBindings []json.RawMessage ` + "`json:\"secret_bindings\"`" + `}};if e=json.Unmarshal(raw,&export);e!=nil{panic(e)};count:=0;for _,w:=range export.Workloads{for _,raw:=range w.Services{var s p.ServiceSpec;strict(raw,&s);if s.Name==""||s.Role!=p.RoleMain||s.Resources.Replicas!=2{panic("service mapping")};for _,c:=range s.ConfigFiles{b,_:=json.Marshal(c);var config p.ConfigFile;strict(b,&config);if config.Path==""{panic("config mapping")}};count++};for _,raw:=range w.SecretBindings{var binding p.SecretBinding;strict(raw,&binding);if binding.EnvKey!="ATLAS_PRIMARY_DSN"||binding.Ref.Key!="vault-primary"||string(binding.Ref.Type)!="env"{panic("secret mapping")}}};if count!=1{panic("workload mapping")};fmt.Println("actual ctrlplane ServiceSpec, ConfigFile and SecretBinding conform")}
`
