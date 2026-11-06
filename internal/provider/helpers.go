package provider

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

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

// serverManagedAdapterKeys are the adapter config keys Paperclip writes itself
// to record an agent's instruction bundle and assigned skills. The API replaces
// the whole adapter config on update, so these have to be sent back or the
// agent loses its instructions and skills.
var serverManagedAdapterKeys = []string{
	"instructionsBundleMode",
	"instructionsRootPath",
	"instructionsEntryFile",
	"instructionsFilePath",
	"agentsMdPath",
	"paperclipSkillSync",
}

// withServerManagedKeys returns desired plus any server-managed keys from
// remote that desired does not set itself.
func withServerManagedKeys(desired, remote map[string]any) map[string]any {
	out := maps.Clone(orEmpty(desired))
	for _, key := range serverManagedAdapterKeys {
		if _, declared := out[key]; declared {
			continue
		}
		if value, ok := remote[key]; ok {
			out[key] = value
		}
	}
	return out
}

// withoutServerManagedKeys returns remote without the server-managed keys.
func withoutServerManagedKeys(remote map[string]any) map[string]any {
	out := maps.Clone(orEmpty(remote))
	for _, key := range serverManagedAdapterKeys {
		delete(out, key)
	}
	return out
}

// adapterConfigState computes the state value for adapter_config. With nothing
// declared, for example after an import, it stores the remote value without the
// keys that other attributes cover.
func adapterConfigState(remote map[string]any, prior jsontypes.Normalized, attr path.Path, diags *diag.Diagnostics) jsontypes.Normalized {
	if prior.IsNull() || prior.IsUnknown() {
		return encodeJSONObject(withoutServerManagedKeys(remote), attr, diags)
	}
	return jsonObjectState(remote, prior, attr, diags)
}

// sameSkill reports whether a declared reference names a skill key. The API
// accepts a full key or a bare slug and always stores the full key.
func sameSkill(reference, key string) bool {
	reference, key = strings.ToLower(reference), strings.ToLower(key)
	return reference == key || strings.HasSuffix(key, "/"+reference)
}

// matchSkillReferences maps the remote skill keys back to the spelling used in
// the configuration, so a slug does not show up as drift against its key. Keys
// that nothing declares are returned as they are.
func matchSkillReferences(declared, remote []string) []string {
	out := make([]string, 0, len(remote))
	for _, key := range remote {
		match := key
		for _, reference := range declared {
			if sameSkill(reference, key) {
				match = reference
				break
			}
		}
		out = append(out, match)
	}
	return out
}

// skillSetsEqual reports whether the remote keys are exactly the declared set.
func skillSetsEqual(declared, remote []string) bool {
	a := slices.Clone(declared)
	b := matchSkillReferences(declared, remote)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

// frontmatterValue returns a single-line top-level value from a markdown file's
// YAML frontmatter. It reports false when the key is missing or uses a form it
// cannot read, such as a block scalar.
func frontmatterValue(markdown, key string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	if strings.TrimRight(lines[0], " \t") != "---" {
		return "", false
	}
	for _, line := range lines[1:] {
		if strings.TrimRight(line, " \t") == "---" {
			return "", false
		}
		name, value, found := strings.Cut(line, ":")
		if !found || name != key {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value[:1], "|>") {
			return "", false
		}
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		return value, true
	}
	return "", false
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
