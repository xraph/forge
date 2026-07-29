package typescript

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// findESBuild returns the argv prefix used to bundle generated TypeScript
// into a single runnable ESM file, or skips the test when no bundler is
// available. Bundling (rather than running .ts files directly under Node's
// native TS support) is required because the generated tsconfig.json uses
// "moduleResolution": "bundler" and every generated import is intentionally
// extensionless (e.g. `from './fetch'`) — Node's own module resolver cannot
// resolve that without a bundler doing the same resolution tsc does.
func findESBuild(t *testing.T) []string {
	t.Helper()

	if path, err := exec.LookPath("esbuild"); err == nil {
		return []string{path}
	}

	// npx being on PATH does NOT mean esbuild is reachable through it.
	// `npx --no-install esbuild` exits non-zero at RUN time with "npx canceled
	// due to missing packages and no YES option" when the package is absent,
	// which turned every runtime test into a failure rather than a skip on any
	// machine (including CI) that had npx but no esbuild. Probe the capability
	// instead of inferring it from npx's existence.
	if path, err := exec.LookPath("npx"); err == nil {
		probe := exec.CommandContext(context.Background(), path, "--no-install", "esbuild", "--version")
		if probe.Run() == nil {
			return []string{path, "--no-install", "esbuild"}
		}
	}

	t.Skip("esbuild not available (not on PATH, and not reachable via npx --no-install); skipping generated-client runtime test")

	return nil
}

// findNode returns the path to a Node.js binary, or skips the test when none
// is available.
func findNode(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found on PATH; skipping generated-client runtime test")
	}

	return path
}

// runNodeDriver bundles entry (a path relative to dir, e.g. "src/__driver.ts")
// with esbuild into a single Node-runnable ESM file and executes it under
// Node, returning stdout. It fails the test outright on any bundling or
// runtime error — a thrown error or non-zero exit means the driver script
// itself is broken, not that the assertion it encodes failed, so that must
// not be mistaken for a passing (or even a meaningfully failing) assertion.
//
// This exists to verify actual runtime behavior of generated code — e.g.
// that a declared `Promise<T | void>` return type is honored by what the
// generated fetch client actually resolves with for an empty-bodied
// response — which `tsc --noEmit` cannot check, since tsc never executes
// anything.
func runNodeDriver(t *testing.T, dir, entry string) string {
	t.Helper()

	esbuildArgv := findESBuild(t)
	nodePath := findNode(t)

	outFile := filepath.Join(dir, "__bundle.mjs")

	args := append(append([]string{}, esbuildArgv[1:]...),
		entry,
		"--bundle",
		"--platform=node",
		"--format=esm",
		"--outfile="+outFile,
	)

	cmd := exec.CommandContext(context.Background(), esbuildArgv[0], args...)
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("esbuild failed to bundle %s: %v\n%s", entry, err, out)
	}

	runCmd := exec.CommandContext(context.Background(), nodePath, outFile)
	runCmd.Dir = dir

	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node execution of bundled %s failed: %v\n%s", entry, err, out)
	}

	return string(out)
}

// e2eSnakeCaseSpec returns a spec built specifically to prove Phase 3 end to
// end: every schema property is snake_case on the wire, and the shapes cover
// every codec kind the phase touches in one place --
//
//   - Order.order_id:      a plain scalar rename
//   - Order.customer:      a $ref'd NESTED OBJECT (its own rename namespace)
//   - Order.line_items:    an ARRAY OF $ref'd OBJECTS
//   - Order.metadata:      a RECORD (additionalProperties) -- keys are data,
//     never renamed, only declared field names are
//   - Order.payment_info:  a DISCRIMINATED UNION (CardPayment | BankPayment)
//
// Every schema declares its properties as required so the generated
// TypeScript never makes a field optional -- which would force the driver
// below to thread optional-chaining through every access purely to satisfy
// strict mode, obscuring the thing actually under test.
func e2eSnakeCaseSpec() *client.APISpec {
	return &client.APISpec{
		Info: client.APIInfo{Title: "E2E Probe API", Version: "1.0.0"},
		Endpoints: []client.Endpoint{
			{
				Method: "POST", Path: "/orders", OperationID: "orders.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
				}},
				Responses: map[int]*client.Response{
					201: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
					}},
				},
			},
		},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type:     "object",
				Required: []string{"order_id", "customer", "line_items", "metadata", "payment_info"},
				Properties: map[string]*client.Schema{
					"order_id":     {Type: "string"},
					"customer":     {Ref: "#/components/schemas/Customer"},
					"line_items":   {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/LineItem"}},
					"metadata":     {Type: "object", AdditionalProperties: &client.Schema{Type: "string"}},
					"payment_info": {Ref: "#/components/schemas/PaymentMethod"},
				},
			},
			"Customer": {
				Type:     "object",
				Required: []string{"full_name", "contact_email"},
				Properties: map[string]*client.Schema{
					"full_name":     {Type: "string"},
					"contact_email": {Type: "string"},
				},
			},
			"LineItem": {
				Type:     "object",
				Required: []string{"item_id", "unit_price"},
				Properties: map[string]*client.Schema{
					"item_id":    {Type: "string"},
					"unit_price": {Type: "number"},
				},
			},
			"PaymentMethod": {
				OneOf: []*client.Schema{
					{Ref: "#/components/schemas/CardPayment"},
					{Ref: "#/components/schemas/BankPayment"},
				},
				Discriminator: &client.Discriminator{
					PropertyName: "payment_type",
					Mapping: map[string]string{
						"card": "#/components/schemas/CardPayment",
						"bank": "#/components/schemas/BankPayment",
					},
				},
			},
			"CardPayment": {
				Type:     "object",
				Required: []string{"payment_type", "card_number"},
				Properties: map[string]*client.Schema{
					"payment_type": {Type: "string", Enum: []any{"card"}},
					"card_number":  {Type: "string"},
				},
			},
			"BankPayment": {
				Type:     "object",
				Required: []string{"payment_type", "account_number"},
				Properties: map[string]*client.Schema{
					"payment_type":   {Type: "string", Enum: []any{"bank"}},
					"account_number": {Type: "string"},
				},
			},
		},
	}
}

// TestEndToEndSnakeCaseRoundTripThroughGeneratedMethod is the execution proof
// that Phase 3 delivered its stated goal, through REAL generated code -- not
// a hand-built config, and not encode()/decode() called directly (every
// codec-level test elsewhere in this package does that; this is the one test
// that instead drives a generated RESTClient METHOD, exactly as a real
// consumer would).
//
// It generates a full client for e2eSnakeCaseSpec() (every property
// snake_case on the wire, camel-case naming configured), bundles it with
// esbuild, and under Node:
//
//  1. calls client.orders.create(...) with a camelCase request object and
//     captures the exact JSON string handed to global fetch as the request
//     body -- proving encode puts snake_case ON THE WIRE;
//  2. has the mocked fetch return a snake_case JSON response, and inspects
//     what the awaited call resolves to -- proving decode hands back
//     camelCase;
//  3. covers, in that one round trip, a nested object (customer), an array
//     of objects (line_items), a record (metadata -- keys are caller-chosen
//     data and must survive UNCHANGED, never renamed), and a discriminated
//     union (payment_info: CardPayment | BankPayment). The record's keys are
//     deliberately shaped so a case-conversion regression is visible: at
//     least one already looks snake_case ("billing_region"/"shipping_region")
//     and at least one already looks camelCase ("expressZone"/"deliveryZone")
//     on EACH side of the round trip, so neither shape is invariant under
//     toCamel/toSnake the way single-word keys ("region", "sku123") would
//     have been -- an earlier version of this test used exactly such
//     invariant keys and still passed under an injected record-key
//     case-conversion bug (codecs.go's 'record' kind, which must never
//     rename keys at all);
//  4. includes an unknown key on BOTH sides (request: loyaltyPoints, not
//     part of Order at all; response: server_note at the top level and
//     internal_note nested inside customer) and asserts each survives
//     verbatim, proving codecRuntime's "unknown key passes through, name and
//     value untouched" rule holds through a real method call, not just
//     encode()/decode() in isolation;
//  5. reads the awaited result's camelCase fields directly off the
//     DECLARED TypeScript type (types.Order, through the PaymentMethod
//     union, narrowed on paymentType) rather than through `any` -- so this
//     same driver file is also run through tsc (typeCheck), proving the
//     declared type actually matches what arrives at runtime, not just that
//     *something* arrives.
func TestEndToEndSnakeCaseRoundTripThroughGeneratedMethod(t *testing.T) {
	config := client.DefaultConfig()
	config.Language = "typescript"
	config.PackageName = "e2eprobe"
	config.FieldNaming = client.NamingCamel

	out, err := NewGenerator().Generate(context.Background(), e2eSnakeCaseSpec(), config)
	require.NoError(t, err, "generation must succeed for a conforming, fully-discriminated spec")

	types := out.Files["src/types.ts"]
	require.Contains(t, types, "orderId", "sanity check: Order.order_id must render as camelCase orderId")
	require.NotContains(t, types, "order_id", "sanity check: the wire name order_id must not leak into the rendered type")
	require.Contains(t, types, "export type PaymentMethod = CardPayment | BankPayment;",
		"sanity check: PaymentMethod must render as the exact discriminated union this test narrows on")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';
import * as types from './types';

let capturedBody: string | undefined;

(globalThis as any).fetch = async (_url: string, init: any) => {
  capturedBody = init.body;

  // The WIRE response: every field snake_case, plus two unknown keys (one
  // top-level, one nested inside customer) that Order/Customer never
  // declare at all -- proving decode leaves what it doesn't recognize alone.
  const wireResponse = {
    order_id: 'ord_99',
    customer: {
      full_name: 'Grace Hopper',
      contact_email: 'grace@example.com',
      internal_note: 'server-added, unknown to Customer',
    },
    line_items: [
      { item_id: 'sku-100', unit_price: 12.5 },
      { item_id: 'sku-200', unit_price: 3.25 },
    ],
    // Both keys are deliberately shaped so a record-key case-conversion
    // regression is visible: "billing_region" is already snake_case, so
    // decoding it through toCamel (the bug) turns it into "billingRegion"
    // and the exact-key lookup below finds nothing; "expressZone" is
    // camelCase, so a decode-direction bug that (wrongly) ran EVERY record
    // key through toSnake would turn it into "express_zone" instead. Either
    // wrong transform is caught; neither key is invariant under case
    // conversion the way "region"/"channel" were.
    metadata: { billing_region: 'north-zone', expressZone: 'zone-3' },
    payment_info: { payment_type: 'bank', account_number: '000-111-222' },
    server_note: 'server-added, unknown to Order',
  };

  return new Response(JSON.stringify(wireResponse), {
    status: 201,
    headers: { 'content-type': 'application/json' },
  });
};

async function main() {
  const client = new RESTClient({ baseURL: 'http://example.invalid' });

  // The CLIENT-SIDE (camelCase) request. loyaltyPoints is not part of Order
  // at all -- proving encode leaves an unrecognized key alone, name and
  // value untouched, rather than dropping it.
  const rawInput = {
    orderId: 'ord_1',
    customer: { fullName: 'Ada Lovelace', contactEmail: 'ada@example.com' },
    lineItems: [
      { itemId: 'item-a', unitPrice: 5 },
      { itemId: 'item-b', unitPrice: 7.5 },
    ],
    // Same reasoning as the response's metadata, mirrored for the encode
    // direction: "shipping_region" is snake_case-shaped caller data that
    // must reach the wire completely unchanged (an encode-direction bug
    // that ran record keys through toCamel would turn it into
    // "shippingRegion" on the wire), and "deliveryZone" is camelCase-shaped
    // caller data that must likewise survive (a bug running record keys
    // through toSnake would turn it into "delivery_zone" on the wire).
    metadata: { shipping_region: 'east-coast', deliveryZone: 'zone-9' },
    paymentInfo: { paymentType: 'card' as const, cardNumber: '4242424242424242' },
    loyaltyPoints: 42,
  };

  const created: types.Order = await client.orders.create(rawInput);

  // --- Consumer snippet: reads camelCase fields straight off the DECLARED
  // return type. tsc checks these accesses against types.Order,
  // types.Customer, types.LineItem, and the PaymentMethod union (including
  // the narrowing branch); the assertions in Go below check the VALUES.
  const orderId: string = created.orderId;
  const customerName: string = created.customer.fullName;
  const customerEmail: string = created.customer.contactEmail;
  const firstItemId: string = created.lineItems[0].itemId;
  const firstItemPrice: number = created.lineItems[0].unitPrice;
  const secondItemId: string = created.lineItems[1].itemId;
  const billingRegion: string = created.metadata['billing_region'];
  const expressZone: string = created.metadata['expressZone'];

  let accountNumber = '';
  if (created.paymentInfo.paymentType === 'bank') {
    accountNumber = created.paymentInfo.accountNumber;
  }

  console.log(JSON.stringify({
    wireBody: capturedBody ? JSON.parse(capturedBody) : null,
    decoded: {
      orderId,
      customerName,
      customerEmail,
      firstItemId,
      firstItemPrice,
      secondItemId,
      billingRegion,
      expressZone,
      accountNumber,
      // Unknown keys must have survived decode verbatim -- cast to any
      // because the DECLARED type correctly has no member for them at all.
      serverNote: (created as any).server_note,
      customerInternalNote: (created.customer as any).internal_note,
    },
  }));
}

// No process.exit(1) in the catch handler here (unlike other drivers in
// this package): this file is ALSO run through tsc (see below), and the
// generated tsconfig's "lib" has no Node type definitions, so referencing
// the ambient "process" global would fail to type-check. An unhandled
// rejection already exits Node with a non-zero status on its own, which is
// all runNodeDriver needs to detect a broken driver.
main().catch((err) => {
  console.error(err);
  throw err;
});
`
	writeTree(t, dir, map[string]string{"src/__driver_e2e_snake_case.ts": driver})

	// tsc first: confirm the declared type actually accepts every access the
	// driver makes, BEFORE esbuild strips the types away and the runtime
	// portion below runs regardless of whether they were ever valid.
	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("generated client + consumer snippet must type-check with zero errors, got:\n%s", strings.Join(errs, "\n"))
	}

	stdout := runNodeDriver(t, dir, "src/__driver_e2e_snake_case.ts")

	var result struct {
		WireBody map[string]any `json:"wireBody"`
		Decoded  struct {
			OrderID              string  `json:"orderId"`
			CustomerName         string  `json:"customerName"`
			CustomerEmail        string  `json:"customerEmail"`
			FirstItemID          string  `json:"firstItemId"`
			FirstItemPrice       float64 `json:"firstItemPrice"`
			SecondItemID         string  `json:"secondItemId"`
			BillingRegion        string  `json:"billingRegion"`
			ExpressZone          string  `json:"expressZone"`
			AccountNumber        string  `json:"accountNumber"`
			ServerNote           string  `json:"serverNote"`
			CustomerInternalNote string  `json:"customerInternalNote"`
		} `json:"decoded"`
	}

	decodeLastLine(t, stdout, &result)

	// --- 1. camelCase input put snake_case ON THE WIRE. ---
	wireJSON, err := json.Marshal(result.WireBody)
	require.NoError(t, err)
	wireStr := string(wireJSON)

	assert.Equal(t, "ord_1", result.WireBody["order_id"], "wire body:\n%s", wireStr)
	assert.NotContains(t, result.WireBody, "orderId", "the camelCase client name must not leak onto the wire; wire body:\n%s", wireStr)

	customer, ok := result.WireBody["customer"].(map[string]any)
	require.True(t, ok, "wire body customer must be an object; wire body:\n%s", wireStr)
	assert.Equal(t, "Ada Lovelace", customer["full_name"], "nested object rename on the wire; wire body:\n%s", wireStr)
	assert.Equal(t, "ada@example.com", customer["contact_email"], "nested object rename on the wire; wire body:\n%s", wireStr)

	lineItems, ok := result.WireBody["line_items"].([]any)
	require.True(t, ok, "wire body line_items must be an array; wire body:\n%s", wireStr)
	require.Len(t, lineItems, 2)
	firstWireItem, ok := lineItems[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "item-a", firstWireItem["item_id"], "array-of-objects rename on the wire; wire body:\n%s", wireStr)
	assert.Equal(t, float64(5), firstWireItem["unit_price"], "array-of-objects rename on the wire; wire body:\n%s", wireStr)

	metadata, ok := result.WireBody["metadata"].(map[string]any)
	require.True(t, ok, "wire body metadata must be an object; wire body:\n%s", wireStr)
	assert.Equal(t, "east-coast", metadata["shipping_region"],
		"record KEYS are caller data and must survive unrenamed even when they are already snake_case-shaped -- a bug that ran record keys through toCamel on encode would turn this into \"shippingRegion\"; wire body:\n%s", wireStr)
	assert.Equal(t, "zone-9", metadata["deliveryZone"],
		"record KEYS are caller data and must survive unrenamed even when they are camelCase-shaped -- a bug that ran record keys through toSnake on encode would turn this into \"delivery_zone\"; wire body:\n%s", wireStr)

	paymentInfo, ok := result.WireBody["payment_info"].(map[string]any)
	require.True(t, ok, "wire body payment_info must be an object; wire body:\n%s", wireStr)
	assert.Equal(t, "card", paymentInfo["payment_type"], "discriminated union tag on the wire; wire body:\n%s", wireStr)
	assert.Equal(t, "4242424242424242", paymentInfo["card_number"], "discriminated union member field rename on the wire; wire body:\n%s", wireStr)

	assert.Equal(t, float64(42), result.WireBody["loyaltyPoints"],
		"an UNKNOWN key (not declared anywhere on Order) must survive encode verbatim -- same name, same value; wire body:\n%s", wireStr)

	// --- 2. a snake_case response arrived as camelCase. ---
	assert.Equal(t, "ord_99", result.Decoded.OrderID, "top-level scalar rename on decode")
	assert.Equal(t, "Grace Hopper", result.Decoded.CustomerName, "nested object rename on decode")
	assert.Equal(t, "grace@example.com", result.Decoded.CustomerEmail, "nested object rename on decode")
	assert.Equal(t, "sku-100", result.Decoded.FirstItemID, "array-of-objects rename on decode")
	assert.Equal(t, 12.5, result.Decoded.FirstItemPrice, "array-of-objects rename on decode")
	assert.Equal(t, "sku-200", result.Decoded.SecondItemID, "array-of-objects rename on decode")
	assert.Equal(t, "north-zone", result.Decoded.BillingRegion,
		"record KEYS must survive decode unrenamed even when they are already snake_case-shaped -- a bug that ran record keys through toCamel on decode would turn \"billing_region\" into \"billingRegion\", and this exact-key lookup would find nothing")
	assert.Equal(t, "zone-3", result.Decoded.ExpressZone,
		"record KEYS must survive decode unrenamed even when they are camelCase-shaped -- a bug that ran record keys through toSnake on decode would turn \"expressZone\" into \"express_zone\", and this exact-key lookup would find nothing")
	assert.Equal(t, "000-111-222", result.Decoded.AccountNumber,
		"discriminated union: the BankPayment branch must be reachable and its own field renamed")

	// --- 3. unknown keys survive decode too, verbatim. ---
	assert.Equal(t, "server-added, unknown to Order", result.Decoded.ServerNote,
		"an UNKNOWN top-level key (not declared anywhere on Order) must survive decode verbatim")
	assert.Equal(t, "server-added, unknown to Customer", result.Decoded.CustomerInternalNote,
		"an UNKNOWN nested key (not declared anywhere on Customer) must survive decode verbatim")
}

// TestWireCodecExprEscapesHostileSchemaNames pins that the codec id embedded
// by wireEncodeExpr/wireDecodeExpr cannot break out of its own literal.
//
// CodeQL flags webtransport.go's `this.emit('incomingUniStream', %s)` under
// go/unsafe-quoting: "if this JSON value contains a single quote, it could
// break out of the enclosing quotes." That alert is a FALSE POSITIVE, and
// this test is the evidence. The single quotes in that template belong to a
// constant event name; the interpolated value is a json.Marshal result, which
// is double-quoted and escapes everything that matters. A single quote inside
// a double-quoted JS string is inert.
//
// It is still worth pinning, because the failure mode is real and this repo
// has already hit it once: an earlier task had to replace
// fmt.Sprintf("'%v'", v) with json.Marshal in enumTSType after a schema value
// containing an apostrophe produced an unterminated literal that broke the
// whole generated file. If anyone "simplifies" these helpers back to manual
// quoting, this fails.
//
// Counting quotes is deliberately NOT the assertion — that heuristic reports
// `"it's"` as unbalanced when it is perfectly valid. Parsing is the assertion.
func TestWireCodecExprEscapesHostileSchemaNames(t *testing.T) {
	node := findNode(t)

	hostile := []string{
		`it's`,                   // apostrophe -- the enumTSType regression
		`a"b`,                    // double quote
		`a'; alert(1); '`,        // attempted statement injection
		"line\nbreak",            // raw newline
		`</script>`,              // HTML context escape
		`back\slash`,             // backslash
		" sep",                   // JS line separator, valid in JSON but not in JS source
		`"; process.exit(1); //`, // attempted break-out of the double-quoted form
	}

	for _, id := range hostile {
		for name, expr := range map[string]string{
			"decode": wireDecodeExpr(id, "JSON.parse(data)"),
			"encode": wireEncodeExpr(id, "payload"),
		} {
			dir := t.TempDir()
			file := filepath.Join(dir, "probe.mjs")
			src := "const decode=(v,c)=>({v,c});\nconst encode=(v,c)=>({v,c});\nconst payload={};\n" +
				"function f(data){ return " + expr + "; }\n"

			if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}

			out, err := exec.CommandContext(context.Background(), node, "--check", file).CombinedOutput()
			if err != nil {
				t.Errorf("%s: schema id %q produced unparseable JavaScript: %v\n%s\nemitted: %s",
					name, id, err, out, expr)
			}
		}
	}
}
