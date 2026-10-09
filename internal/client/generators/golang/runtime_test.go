package golang_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
)

func TestGeneratedClientRuntimeServiceConfiguration(t *testing.T) {
	spec := &client.APISpec{Info: client.APIInfo{Title: "API"}, Endpoints: []client.Endpoint{{ID: "ping", OperationID: "ping", Method: "GET", Path: "/ping"}}}
	config := client.GeneratorConfig{PackageName: "runtimeclient", Module: "example.test/runtimeclient", APIName: "Client", BaseURL: "http://generation.default", Features: client.Features{Timeout: true}}

	result, err := golang.NewGenerator().Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()

	for name, source := range result.Files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}

		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/runtimeclient\n\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "runtime_test.go"), []byte(runtimeClientTests), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(context.Background(), "go", "test", "./...")
	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime behavior: %v\n%s", err, out)
	}
}

const runtimeClientTests = `package runtimeclient
import("context";"net/http";"net/http/httptest";"testing";"time";"sync/atomic")
type serviceConfig map[string]string
func(c serviceConfig)GetString(key string,defaults ...string)string{if value,ok:=c[key];ok{return value};if len(defaults)>0{return defaults[0]};return ""}
func TestRuntimeEndpoints(t *testing.T){
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(204)}));defer server.Close()
 t.Setenv("API_URL",server.URL)
 c:=NewClient(WithServiceName("api"))
 if c.baseURL!=server.URL{t.Fatalf("service environment ignored: %s",c.baseURL)}
 if err:=c.doRequest(context.Background(),"GET","/ping",nil,nil);err!=nil{t.Fatal(err)}
 cfg:=serviceConfig{"services.api.url":server.URL,"services.api.timeout":"5s"}
 shared:=&http.Client{Timeout:30*time.Second}
 c=NewClient(WithHTTPClient(shared),WithServiceConfig("api",cfg))
 if c.baseURL!=server.URL || c.httpClient.Timeout!=5*time.Second{t.Fatal("runtime config ignored")}
 if shared.Timeout!=30*time.Second{t.Fatal("shared HTTP client mutated")}
 for _,opts:=range [][]ClientOption{{WithServiceConfig("api",cfg),WithBaseURL("https://explicit.internal")},{WithBaseURL("https://explicit.internal"),WithServiceConfig("api",cfg)}}{
  c=NewClient(opts...);if c.baseURL!="https://explicit.internal"{t.Fatal("explicit endpoint lost")}
 }
 c=NewClient(WithTimeout(2*time.Second),WithServiceConfig("api",cfg))
 if c.httpClient.Timeout!=2*time.Second{t.Fatal("explicit timeout lost")}
 if _,err:=NewClientChecked(WithServiceConfig("api",serviceConfig{"services.api.url":"not a URL"}));err==nil{t.Fatal("invalid runtime URL accepted")}
 if _,err:=NewClientChecked(WithServiceConfig("api",serviceConfig{"services.api.url":server.URL,"services.api.timeout":"wrong"}));err==nil{t.Fatal("invalid runtime timeout accepted")}
 c=NewClient(WithServiceConfig("api",serviceConfig{"services.api.url":"not a URL"}))
 if err:=c.doRequest(context.Background(),"GET","/ping",nil,nil);err==nil{t.Fatal("legacy constructor hid configuration failure")}
  var calls atomic.Int32
 retryServer:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){count:=calls.Add(1);if r.Method=="POST" || count<3{w.WriteHeader(503);return};w.WriteHeader(204)}));defer retryServer.Close()
 c=NewClient(WithServiceConfig("api",serviceConfig{"services.api.url":retryServer.URL,"services.api.retry.attempts":"2"}))
 if err:=c.doRequest(context.Background(),"GET","/ping",nil,nil);err!=nil || calls.Load()!=3{t.Fatal("safe retries failed",err,calls.Load())}
 calls.Store(0)
 if err:=c.doRequest(context.Background(),"POST","/ping",nil,nil);err==nil || calls.Load()!=1{t.Fatal("write retried without explicit policy",err,calls.Load())}
 if _,err:=NewClientChecked(WithServiceConfig("api",serviceConfig{"services.api.url":server.URL,"services.api.retry.attempts":"6"}));err==nil{t.Fatal("unbounded retries accepted")}
 t.Setenv("API_URL","")
 if c:=NewClient(WithServiceName("api"));c.baseURL!="http://generation.default"{t.Fatal("generated fallback lost")}
}
`
