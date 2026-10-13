// Command dashboard-contract-probe posts one contract envelope to a dashboard
// and prints the HTTP status and body it gets back.
package main

import (
	"bytes"
	"context"
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
	kind := flag.String("kind", "query", "query | command | subscribe")
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
	// An invalid -payload makes the RawMessage fail to marshal. Say so rather
	// than posting an empty body and printing whatever the server makes of it.
	body, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode request:", err)
		os.Exit(1)
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		*base+"/api/dashboard/v1", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "build request:", err)
		os.Exit(1)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		fmt.Fprintln(os.Stderr, "request:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(resp.Body)
	fmt.Fprintf(os.Stdout, "HTTP %d\n%s\n", resp.StatusCode, out)
}
