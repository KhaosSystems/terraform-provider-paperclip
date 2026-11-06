package provider

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid test JSON %q: %s", s, err)
	}
	return v
}

func TestProjectManaged(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		remote, managed, want string
	}{
		"identical": {
			remote:  `{"command":"echo","args":["a"]}`,
			managed: `{"command":"echo","args":["a"]}`,
			want:    `{"command":"echo","args":["a"]}`,
		},
		"server defaults are ignored": {
			remote:  `{"command":"echo","timeoutSec":0,"graceSec":15}`,
			managed: `{"command":"echo"}`,
			want:    `{"command":"echo"}`,
		},
		"nested server defaults are ignored": {
			remote:  `{"heartbeat":{"enabled":true,"intervalSec":300,"cooldownSec":10}}`,
			managed: `{"heartbeat":{"enabled":true,"intervalSec":300}}`,
			want:    `{"heartbeat":{"enabled":true,"intervalSec":300}}`,
		},
		"changed value is reported": {
			remote:  `{"command":"python3"}`,
			managed: `{"command":"echo"}`,
			want:    `{"command":"python3"}`,
		},
		"removed key is reported": {
			remote:  `{}`,
			managed: `{"command":"echo"}`,
			want:    `{}`,
		},
		"redacted string keeps the managed value": {
			remote:  `{"env":{"TOKEN":"***REDACTED***"}}`,
			managed: `{"env":{"TOKEN":"s3cret"}}`,
			want:    `{"env":{"TOKEN":"s3cret"}}`,
		},
		"redacted nested value keeps the managed value": {
			remote:  `{"env":{"TOKEN":{"type":"plain","value":"***REDACTED***"}}}`,
			managed: `{"env":{"TOKEN":{"type":"plain","value":"s3cret"}}}`,
			want:    `{"env":{"TOKEN":{"type":"plain","value":"s3cret"}}}`,
		},
		"redacted value replacing an object keeps the managed value": {
			remote:  `{"env":{"TOKEN":"***REDACTED***"}}`,
			managed: `{"env":{"TOKEN":{"type":"plain","value":"s3cret"}}}`,
			want:    `{"env":{"TOKEN":{"type":"plain","value":"s3cret"}}}`,
		},
		"redacted value inside a list keeps the managed value": {
			remote:  `{"headers":[{"name":"Authorization","value":"***REDACTED***"}]}`,
			managed: `{"headers":[{"name":"Authorization","value":"Bearer x"}]}`,
			want:    `{"headers":[{"name":"Authorization","value":"Bearer x"}]}`,
		},
		"list of a different length is reported as is": {
			remote:  `{"args":["a","b"]}`,
			managed: `{"args":["a"]}`,
			want:    `{"args":["a","b"]}`,
		},
		"type change is reported": {
			remote:  `{"args":"a b"}`,
			managed: `{"args":["a","b"]}`,
			want:    `{"args":"a b"}`,
		},
		"secret reference is compared as usual": {
			remote:  `{"env":{"TOKEN":{"type":"secret_ref","secretId":"new"}}}`,
			managed: `{"env":{"TOKEN":{"type":"secret_ref","secretId":"old"}}}`,
			want:    `{"env":{"TOKEN":{"type":"secret_ref","secretId":"new"}}}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := projectManaged(mustJSON(t, tc.remote), mustJSON(t, tc.managed))
			if want := mustJSON(t, tc.want); !reflect.DeepEqual(got, want) {
				t.Errorf("projectManaged() = %v, want %v", got, want)
			}
		})
	}
}

func TestContainsRedacted(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value string
		want  bool
	}{
		"empty":           {`{}`, false},
		"plain":           {`{"command":"echo","args":["a"]}`, false},
		"top level":       {`{"token":"***REDACTED***"}`, true},
		"nested":          {`{"env":{"TOKEN":{"type":"plain","value":"***REDACTED***"}}}`, true},
		"in list":         {`{"args":["a","***REDACTED***"]}`, true},
		"substring only":  {`{"note":"shows ***REDACTED*** in logs"}`, false},
		"key is not data": {`{"***REDACTED***":"x"}`, false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := containsRedacted(mustJSON(t, tc.value)); got != tc.want {
				t.Errorf("containsRedacted(%s) = %t, want %t", tc.value, got, tc.want)
			}
		})
	}
}

func TestJSONObjectState(t *testing.T) {
	t.Parallel()
	attr := path.Root("adapter_config")

	remote := map[string]any{
		"command":    "echo",
		"timeoutSec": float64(0),
		"env":        map[string]any{"TOKEN": client.RedactedValue},
	}

	tests := map[string]struct {
		prior jsontypes.Normalized
		want  string
	}{
		"null prior stores the full remote value": {
			prior: jsontypes.NewNormalizedNull(),
			want:  `{"command":"echo","timeoutSec":0,"env":{"TOKEN":"***REDACTED***"}}`,
		},
		"unknown prior stores the full remote value": {
			prior: jsontypes.NewNormalizedUnknown(),
			want:  `{"command":"echo","timeoutSec":0,"env":{"TOKEN":"***REDACTED***"}}`,
		},
		"known prior tracks declared keys only": {
			prior: jsontypes.NewNormalizedValue(`{"command":"echo","env":{"TOKEN":"s3cret"}}`),
			want:  `{"command":"echo","env":{"TOKEN":"s3cret"}}`,
		},
		"empty prior tracks nothing": {
			prior: jsontypes.NewNormalizedValue(`{}`),
			want:  `{}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var diags diag.Diagnostics
			got := jsonObjectState(remote, tc.prior, attr, &diags)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if !reflect.DeepEqual(mustJSON(t, got.ValueString()), mustJSON(t, tc.want)) {
				t.Errorf("jsonObjectState() = %s, want %s", got.ValueString(), tc.want)
			}
		})
	}
}

func TestJSONObjectChanged(t *testing.T) {
	t.Parallel()
	attr := path.Root("adapter_config")
	value := jsontypes.NewNormalizedValue

	tests := map[string]struct {
		plan, state jsontypes.Normalized
		want        bool
	}{
		"same text":          {value(`{"a":1}`), value(`{"a":1}`), false},
		"same meaning":       {value(`{"a":1,"b":[1,2]}`), value(`{ "b": [1, 2], "a": 1.0 }`), false},
		"different value":    {value(`{"a":1}`), value(`{"a":2}`), true},
		"removed key":        {value(`{}`), value(`{"a":1}`), true},
		"unknown plan":       {jsontypes.NewNormalizedUnknown(), value(`{"a":1}`), false},
		"null to value":      {value(`{}`), jsontypes.NewNormalizedNull(), true},
		"value to null":      {jsontypes.NewNormalizedNull(), value(`{}`), true},
		"both null":          {jsontypes.NewNormalizedNull(), jsontypes.NewNormalizedNull(), false},
		"list order matters": {value(`{"a":[1,2]}`), value(`{"a":[2,1]}`), true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var diags diag.Diagnostics
			if got := jsonObjectChanged(tc.plan, tc.state, attr, &diags); got != tc.want {
				t.Errorf("jsonObjectChanged() = %t, want %t", got, tc.want)
			}
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
		})
	}
}

func TestDecodeJSONObject(t *testing.T) {
	t.Parallel()
	attr := path.Root("adapter_config")

	t.Run("null decodes to nil", func(t *testing.T) {
		t.Parallel()
		var diags diag.Diagnostics
		if got := decodeJSONObject(jsontypes.NewNormalizedNull(), attr, &diags); got != nil || diags.HasError() {
			t.Errorf("got %v, diagnostics %v", got, diags)
		}
	})

	t.Run("array is rejected", func(t *testing.T) {
		t.Parallel()
		var diags diag.Diagnostics
		decodeJSONObject(jsontypes.NewNormalizedValue(`[1,2]`), attr, &diags)
		if !diags.HasError() {
			t.Error("expected an error for a JSON array")
		}
	})

	t.Run("invalid JSON is rejected", func(t *testing.T) {
		t.Parallel()
		var diags diag.Diagnostics
		decodeJSONObject(jsontypes.NewNormalizedValue(`{`), attr, &diags)
		if !diags.HasError() {
			t.Error("expected an error for invalid JSON")
		}
	})
}

func TestPointerHelpers(t *testing.T) {
	t.Parallel()

	if stringPointer(types.StringNull()) != nil || stringPointer(types.StringUnknown()) != nil {
		t.Error("stringPointer must map null and unknown to nil")
	}
	if got := stringPointer(types.StringValue("")); got == nil || *got != "" {
		t.Error("stringPointer must keep an empty string")
	}
	if int64Pointer(types.Int64Null()) != nil || int64Pointer(types.Int64Unknown()) != nil {
		t.Error("int64Pointer must map null and unknown to nil")
	}
	if got := int64Pointer(types.Int64Value(0)); got == nil || *got != 0 {
		t.Error("int64Pointer must keep zero")
	}
	if boolPointer(types.BoolNull()) != nil || boolPointer(types.BoolUnknown()) != nil {
		t.Error("boolPointer must map null and unknown to nil")
	}
	if got := boolPointer(types.BoolValue(false)); got == nil || *got {
		t.Error("boolPointer must keep false")
	}
}
