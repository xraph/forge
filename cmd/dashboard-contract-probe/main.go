// main.go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func main() {
	base := flag.String("base", "http://localhost:8080", "dashboard base URL (no trailing slash)")
	kind := flag.String("kind", "query", "graph | query | command")
	contributor := flag.String("contributor", "", "contributor name")
	intent := flag.String("intent", "", "intent name")
	payload := flag.String("payload", "{}", "JSON payload")
	csrf := flag.String("csrf", "", "CSRF token (required for command)")
	idem := flag.String("idem", "", "idempotency key (required for command)")
	flag.Parse()

	req := contract.Request{
		Envelope: "v1", Kind: contract.Kind(*kind),
		Contributor: *contributor, Intent: *intent,
		Payload: json.RawMessage(*payload),
		CSRF:    *csrf, IdempotencyKey: *idem,
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(*base+"/api/dashboard/v1", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "request:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, out)
}
