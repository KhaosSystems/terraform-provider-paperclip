package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

// fakePaperclip is an in-memory stand-in for the parts of the Paperclip API
// the provider uses. It mirrors the behaviour of the real server that matters
// to the provider: server-side defaults, merge-or-replace semantics for the
// adapter config, redaction of plain environment values and the approval gate
// on direct agent creation.
//
// The acceptance tests run against it unless PAPERCLIP_API_URL points at a
// real instance.
type fakePaperclip struct {
	mu       sync.Mutex
	seq      int
	records  map[string]map[string]record // collection -> id -> record
	requests []string
}

type record = map[string]any

func newFakePaperclip() *httptest.Server {
	f := &fakePaperclip{records: map[string]map[string]record{
		"companies": {}, "agents": {}, "goals": {}, "projects": {},
	}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/companies", f.handle(func(_ *http.Request, _ record) (int, any) {
		return http.StatusOK, f.list("companies", "")
	}))
	mux.HandleFunc("POST /api/companies", f.handle(f.createCompany))
	mux.HandleFunc("GET /api/companies/{id}", f.handle(f.get("companies")))
	mux.HandleFunc("PATCH /api/companies/{id}", f.handle(f.patch("companies",
		"name", "description", "budgetMonthlyCents", "requireBoardApprovalForNewAgents")))
	mux.HandleFunc("DELETE /api/companies/{id}", f.handle(f.deleteCompany))

	mux.HandleFunc("GET /api/companies/{id}/agents", f.handle(func(r *http.Request, _ record) (int, any) {
		agents := f.list("agents", r.PathValue("id"))
		for i := range agents {
			agents[i] = redactAgent(agents[i])
		}
		return http.StatusOK, agents
	}))
	mux.HandleFunc("POST /api/companies/{id}/agents", f.handle(f.createAgent))
	mux.HandleFunc("GET /api/agents/{id}", f.handle(func(r *http.Request, body record) (int, any) {
		status, out := f.get("agents")(r, body)
		if rec, ok := out.(record); ok {
			return status, redactAgent(rec)
		}
		return status, out
	}))
	mux.HandleFunc("PATCH /api/agents/{id}", f.handle(f.patchAgent))
	mux.HandleFunc("DELETE /api/agents/{id}", f.handle(f.delete("agents")))

	mux.HandleFunc("POST /api/companies/{id}/goals", f.handle(f.createGoal))
	mux.HandleFunc("GET /api/goals/{id}", f.handle(f.get("goals")))
	mux.HandleFunc("PATCH /api/goals/{id}", f.handle(f.patch("goals",
		"title", "description", "level", "status", "parentId", "ownerAgentId")))
	mux.HandleFunc("DELETE /api/goals/{id}", f.handle(f.delete("goals")))

	mux.HandleFunc("POST /api/companies/{id}/projects", f.handle(f.createProject))
	mux.HandleFunc("GET /api/projects/{id}", f.handle(f.get("projects")))
	mux.HandleFunc("PATCH /api/projects/{id}", f.handle(f.patch("projects",
		"name", "description", "status", "goalIds", "leadAgentId", "targetDate", "color")))
	mux.HandleFunc("DELETE /api/projects/{id}", f.handle(f.delete("projects")))

	return httptest.NewServer(mux)
}

type fakeHandler func(r *http.Request, body record) (int, any)

func fakeError(status int, msg string) (int, any) {
	return status, record{"error": msg}
}

// handle decodes the JSON body, serialises access to the store and encodes
// the result.
func (f *fakePaperclip) handle(h fakeHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := record{}
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeFakeJSON(w, http.StatusBadRequest, record{"error": "Invalid JSON"})
				return
			}
		}

		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		status, out := h(r, body)
		f.mu.Unlock()

		writeFakeJSON(w, status, out)
	}
}

func writeFakeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakePaperclip) nextID() string {
	f.seq++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", f.seq)
}

func (f *fakePaperclip) list(collection, companyID string) []record {
	out := []record{}
	for _, rec := range f.records[collection] {
		if companyID == "" || rec["companyId"] == companyID {
			out = append(out, maps.Clone(rec))
		}
	}
	return out
}

func (f *fakePaperclip) get(collection string) fakeHandler {
	return func(r *http.Request, _ record) (int, any) {
		rec, ok := f.records[collection][r.PathValue("id")]
		if !ok {
			return fakeError(http.StatusNotFound, "Not found")
		}
		return http.StatusOK, maps.Clone(rec)
	}
}

// patch applies the allowed keys that are present in the body, so an explicit
// null clears a field while an absent key leaves it alone.
func (f *fakePaperclip) patch(collection string, allowed ...string) fakeHandler {
	return func(r *http.Request, body record) (int, any) {
		rec, ok := f.records[collection][r.PathValue("id")]
		if !ok {
			return fakeError(http.StatusNotFound, "Not found")
		}
		for _, key := range allowed {
			if v, present := body[key]; present {
				rec[key] = v
			}
		}
		if name, ok := rec["name"].(string); ok && rec["urlKey"] != nil {
			rec["urlKey"] = slugify(name)
		}
		return http.StatusOK, maps.Clone(rec)
	}
}

func (f *fakePaperclip) delete(collection string) fakeHandler {
	return func(r *http.Request, _ record) (int, any) {
		id := r.PathValue("id")
		if _, ok := f.records[collection][id]; !ok {
			return fakeError(http.StatusNotFound, "Not found")
		}
		delete(f.records[collection], id)
		return http.StatusOK, record{"ok": true}
	}
}

func (f *fakePaperclip) createCompany(_ *http.Request, body record) (int, any) {
	name, _ := body["name"].(string)
	if name == "" {
		return fakeError(http.StatusBadRequest, "Validation error")
	}
	id := f.nextID()
	rec := record{
		"id":                               id,
		"name":                             name,
		"description":                      body["description"],
		"status":                           "active",
		"issuePrefix":                      strings.ToUpper(slugify(name))[:min(3, len(slugify(name)))],
		"budgetMonthlyCents":               orDefault(body["budgetMonthlyCents"], float64(0)),
		"spentMonthlyCents":                float64(0),
		"requireBoardApprovalForNewAgents": true,
	}
	f.records["companies"][id] = rec
	return http.StatusCreated, maps.Clone(rec)
}

func (f *fakePaperclip) deleteCompany(r *http.Request, body record) (int, any) {
	id := r.PathValue("id")
	status, out := f.delete("companies")(r, body)
	if status != http.StatusOK {
		return status, out
	}
	for _, collection := range []string{"agents", "goals", "projects"} {
		for childID, rec := range f.records[collection] {
			if rec["companyId"] == id {
				delete(f.records[collection], childID)
			}
		}
	}
	return status, out
}

func (f *fakePaperclip) company(r *http.Request) (record, bool) {
	rec, ok := f.records["companies"][r.PathValue("id")]
	return rec, ok
}

func (f *fakePaperclip) createAgent(r *http.Request, body record) (int, any) {
	company, ok := f.company(r)
	if !ok {
		return fakeError(http.StatusNotFound, "Company not found")
	}
	if company["requireBoardApprovalForNewAgents"] == true {
		return fakeError(http.StatusConflict, "Direct agent creation requires board approval.")
	}
	name, _ := body["name"].(string)
	adapterType, _ := body["adapterType"].(string)
	if name == "" || adapterType == "" {
		return fakeError(http.StatusBadRequest, "Validation error")
	}
	id := f.nextID()
	rec := record{
		"id":                 id,
		"companyId":          company["id"],
		"name":               name,
		"urlKey":             slugify(name),
		"role":               orDefault(body["role"], "general"),
		"title":              body["title"],
		"status":             "idle",
		"reportsTo":          body["reportsTo"],
		"capabilities":       body["capabilities"],
		"adapterType":        adapterType,
		"adapterConfig":      orDefault(body["adapterConfig"], record{}),
		"runtimeConfig":      withRuntimeDefaults(body["runtimeConfig"]),
		"budgetMonthlyCents": orDefault(body["budgetMonthlyCents"], float64(0)),
		"spentMonthlyCents":  float64(0),
		"metadata":           body["metadata"],
	}
	f.records["agents"][id] = rec
	return http.StatusCreated, redactAgent(maps.Clone(rec))
}

func (f *fakePaperclip) patchAgent(r *http.Request, body record) (int, any) {
	rec, ok := f.records["agents"][r.PathValue("id")]
	if !ok {
		return fakeError(http.StatusNotFound, "Agent not found")
	}

	// The real server merges a partial adapterConfig into the stored one
	// unless the caller asks for a replacement.
	if incoming, present := body["adapterConfig"].(record); present {
		if body["replaceAdapterConfig"] == true {
			rec["adapterConfig"] = incoming
		} else {
			merged := maps.Clone(rec["adapterConfig"].(record))
			maps.Copy(merged, incoming)
			rec["adapterConfig"] = merged
		}
	}
	if incoming, present := body["runtimeConfig"]; present {
		rec["runtimeConfig"] = withRuntimeDefaults(incoming)
	}

	status, out := f.patch("agents", "name", "role", "title", "reportsTo", "capabilities",
		"adapterType", "budgetMonthlyCents", "metadata")(r, body)
	if patched, ok := out.(record); ok {
		return status, redactAgent(patched)
	}
	return status, out
}

func (f *fakePaperclip) createGoal(r *http.Request, body record) (int, any) {
	company, ok := f.company(r)
	if !ok {
		return fakeError(http.StatusNotFound, "Company not found")
	}
	title, _ := body["title"].(string)
	if title == "" {
		return fakeError(http.StatusBadRequest, "Validation error")
	}
	id := f.nextID()
	rec := record{
		"id":           id,
		"companyId":    company["id"],
		"title":        title,
		"description":  body["description"],
		"level":        orDefault(body["level"], "task"),
		"status":       orDefault(body["status"], "planned"),
		"parentId":     body["parentId"],
		"ownerAgentId": body["ownerAgentId"],
	}
	f.records["goals"][id] = rec
	return http.StatusCreated, maps.Clone(rec)
}

func (f *fakePaperclip) createProject(r *http.Request, body record) (int, any) {
	company, ok := f.company(r)
	if !ok {
		return fakeError(http.StatusNotFound, "Company not found")
	}
	name, _ := body["name"].(string)
	if name == "" {
		return fakeError(http.StatusBadRequest, "Validation error")
	}
	id := f.nextID()
	rec := record{
		"id":          id,
		"companyId":   company["id"],
		"urlKey":      slugify(name),
		"name":        name,
		"description": body["description"],
		"status":      orDefault(body["status"], "backlog"),
		"goalIds":     orDefault(body["goalIds"], []any{}),
		"leadAgentId": body["leadAgentId"],
		"targetDate":  body["targetDate"],
		"color":       orDefault(body["color"], "#6366f1"),
	}
	f.records["projects"][id] = rec
	return http.StatusCreated, maps.Clone(rec)
}

// redactAgent hides plain environment values the way the real API does. Only
// secret references are returned as they were sent.
func redactAgent(rec record) record {
	cfg, ok := rec["adapterConfig"].(record)
	if !ok {
		return rec
	}
	env, ok := cfg["env"].(record)
	if !ok {
		return rec
	}
	redacted := make(record, len(env))
	for key, value := range env {
		switch v := value.(type) {
		case record:
			if v["type"] == "secret_ref" {
				redacted[key] = v
				continue
			}
			entry := maps.Clone(v)
			entry["value"] = client.RedactedValue
			redacted[key] = entry
		default:
			redacted[key] = client.RedactedValue
		}
	}
	cfg = maps.Clone(cfg)
	cfg["env"] = redacted
	rec["adapterConfig"] = cfg
	return rec
}

// withRuntimeDefaults adds a server-side default to the runtime config, which
// lets the tests prove that undeclared keys do not show up as drift.
func withRuntimeDefaults(v any) record {
	out := record{}
	if cfg, ok := v.(record); ok {
		out = maps.Clone(cfg)
	}
	if _, ok := out["heartbeat"]; !ok {
		out["heartbeat"] = record{"enabled": false}
	}
	return out
}

func orDefault(v, fallback any) any {
	if v == nil {
		return fallback
	}
	return v
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}
