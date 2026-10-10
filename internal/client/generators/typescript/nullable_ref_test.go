package typescript

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestNullableNamedRefWrapperGeneratesExactTypeAndCodec(t *testing.T) {
	quota := &client.Schema{Type: "object", Properties: map[string]*client.Schema{"remaining": {Type: "integer", Nullable: true}}}
	wrapper := &client.Schema{OneOf: []*client.Schema{{Ref: "#/components/schemas/Quota"}, {Type: "object", Nullable: true, Enum: []any{nil}}}}
	spec := &client.APISpec{Schemas: map[string]*client.Schema{"Quota": quota, "Health": {Type: "object", Properties: map[string]*client.Schema{"quota": wrapper}}}}
	g := &Generator{}
	got := g.schemaToTSType(wrapper, spec, "Health.quota", client.GeneratorConfig{})

	if got != "Quota | null" {
		t.Fatalf("nullable reference: %s", got)
	}
	// Null is handled before the codec walk; the object branch must still reach
	// the named quota codec so snake-case source fields retain their contract.
	codec, _ := NewCodecGenerator().Generate(spec, client.GeneratorConfig{})

	if !strings.Contains(codec, "Quota") {
		t.Fatal("nullable wrapper lost named codec", codec)
	}
}

func TestNullableNamedRefOptionalFieldsRuntime(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Reported"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"prompt_tokens": {Type: "integer", Nullable: true}}}
	spec.Schemas["Usage"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"reported": {OneOf: []*client.Schema{{Ref: "#/components/schemas/Reported"}, {Type: "object", Nullable: true, Enum: []any{nil}}}}}}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writeTree(t, dir, out.Files)
	writeTree(t, dir, map[string]string{"src/__nullable.ts": `import {decode,encode} from './codec-runtime'; import {CODECS} from './codecs';
const vals=[null,{}, {prompt_tokens:0},{prompt_tokens:null}];
console.log(JSON.stringify(vals.map(reported=>{const wire={reported};const value=decode(wire,CODECS["Usage"]);return {value,wire:encode(value,CODECS["Usage"])};})));`})
	raw := runNodeDriver(t, dir, "src/__nullable.ts")

	var got []struct {
		Value map[string]any `json:"value"`
		Wire  map[string]any `json:"wire"`
	}
	if err = json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}

	if len(got) != 4 || got[0].Value["reported"] != nil || got[2].Value["reported"].(map[string]any)["promptTokens"] != float64(0) || got[3].Value["reported"].(map[string]any)["promptTokens"] != nil || got[2].Wire["reported"].(map[string]any)["prompt_tokens"] != float64(0) {
		t.Fatal(raw)
	}
}
