package provider

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

// clientFromProviderData extracts the API client that the provider stored
// during Configure. It returns nil without diagnostics when the provider has
// not been configured yet, which happens during early validation.
func clientFromProviderData(data any, diags *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider; please report it.", data))
		return nil
	}
	return c
}

// stringPointer converts a Terraform string to a pointer, mapping null and
// unknown to nil.
func stringPointer(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// int64Pointer converts a Terraform number to a pointer, mapping null and
// unknown to nil.
func int64Pointer(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

// boolPointer converts a Terraform bool to a pointer, mapping null and
// unknown to nil.
func boolPointer(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// decodeJSONObject parses a JSON attribute that must hold an object. Null and
// unknown values decode to a nil map.
func decodeJSONObject(v jsontypes.Normalized, attr path.Path, diags *diag.Diagnostics) map[string]any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(v.ValueString()), &out); err != nil {
		diags.AddAttributeError(attr, "Invalid JSON", "The value is not valid JSON: "+err.Error())
		return nil
	}
	obj, ok := out.(map[string]any)
	if !ok {
		diags.AddAttributeError(attr, "Invalid JSON object",
			"The value must be a JSON object, for example the result of jsonencode({ ... }).")
		return nil
	}
	return obj
}

// encodeJSONObject renders a map as a normalized JSON attribute value.
func encodeJSONObject(v map[string]any, attr path.Path, diags *diag.Diagnostics) jsontypes.Normalized {
	if v == nil {
		v = map[string]any{}
	}
	data, err := json.Marshal(v)
	if err != nil {
		diags.AddAttributeError(attr, "Unable to encode JSON", err.Error())
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(data))
}

// projectManaged reduces a remote JSON value to the parts that are declared in
// managed, so that defaults the server adds on its own do not show up as
// drift. Values the API redacts are replaced by the managed value, because the
// real value cannot be read back.
func projectManaged(remote, managed any) any {
	switch m := managed.(type) {
	case map[string]any:
		r, ok := remote.(map[string]any)
		if !ok {
			return restoreRedacted(remote, managed)
		}
		out := make(map[string]any, len(m))
		for key, managedValue := range m {
			remoteValue, ok := r[key]
			if !ok {
				continue
			}
			out[key] = projectManaged(remoteValue, managedValue)
		}
		return out
	case []any:
		r, ok := remote.([]any)
		if !ok || len(r) != len(m) {
			return restoreRedacted(remote, managed)
		}
		out := make([]any, len(r))
		for i := range r {
			out[i] = projectManaged(r[i], m[i])
		}
		return out
	default:
		return restoreRedacted(remote, managed)
	}
}

func restoreRedacted(remote, managed any) any {
	if s, ok := remote.(string); ok && s == client.RedactedValue {
		return managed
	}
	return remote
}

// containsRedacted reports whether a JSON value contains the placeholder the
// API uses for redacted values. Sending it back would overwrite the real
// secret with the placeholder.
func containsRedacted(v any) bool {
	switch t := v.(type) {
	case string:
		return t == client.RedactedValue
	case map[string]any:
		for _, child := range t {
			if containsRedacted(child) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if containsRedacted(child) {
				return true
			}
		}
	}
	return false
}

// jsonObjectState computes the state value for a JSON object attribute.
//
// When the attribute has a known prior value (the configuration, or the state
// from the previous run) only the keys declared there are tracked. Otherwise,
// for example right after an import, the full remote value is stored.
func jsonObjectState(remote map[string]any, prior jsontypes.Normalized, attr path.Path, diags *diag.Diagnostics) jsontypes.Normalized {
	if prior.IsNull() || prior.IsUnknown() {
		return encodeJSONObject(remote, attr, diags)
	}
	managed := decodeJSONObject(prior, attr, diags)
	if diags.HasError() {
		return prior
	}
	projected, ok := projectManaged(orEmpty(remote), managed).(map[string]any)
	if !ok {
		return encodeJSONObject(remote, attr, diags)
	}
	return encodeJSONObject(projected, attr, diags)
}

func orEmpty(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

// addAPIError records a failed API call as a diagnostic.
func addAPIError(diags *diag.Diagnostics, action string, err error) {
	diags.AddError("Paperclip API error", fmt.Sprintf("Unable to %s: %s", action, err))
}

// sortAgentsByName orders agents by name, then by identifier, so that list
// results are stable between runs.
func sortAgentsByName(agents []client.Agent) {
	slices.SortFunc(agents, func(a, b client.Agent) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
}
