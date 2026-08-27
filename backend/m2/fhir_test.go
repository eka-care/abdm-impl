package m2

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"abdm/backend/db"
)

func seed(t *testing.T) {
	t.Helper()
	t.Setenv("EKA_DB_PATH", filepath.Join(t.TempDir(), "test.sqlite"))
	t.Setenv("EKA_HIP_ID", "IN3610024527")
	t.Setenv("EKA_HIP_NAME", "Test Clinic")
	if err := db.Init(); err != nil {
		t.Fatalf("db init: %v", err)
	}
	// patients is owned by the Next.js side (server/db.ts); recreate it here.
	if _, err := db.DB.Exec(`CREATE TABLE IF NOT EXISTS patients (
		oid TEXT, abha_address TEXT, abha_number TEXT, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.DB.Exec(`INSERT INTO patients (oid, abha_address, abha_number, name)
		VALUES ('o1', 'ram@sbx', '91-1234-5678-9012', 'Ram Kumar')`)

	for _, cc := range []struct{ id, hiType, display string }{
		{"cc-doc", "HealthDocumentRecord", "Scan report"},
		{"cc-rx", "Prescription", "Prescription on 2026-08-10"},
		{"cc-bad", "Immunization", "Unsupported"},
		{"cc-empty", "Prescription", "Linked but never stored"},
	} {
		if _, err := db.DB.Exec(`INSERT INTO care_context_links
			(care_context_id, abha_address, hi_type, display, status) VALUES (?, 'ram@sbx', ?, ?, 'LINKED')`,
			cc.id, cc.hiType, cc.display); err != nil {
			t.Fatal(err)
		}
		if cc.id == "cc-empty" || cc.id == "cc-bad" {
			continue
		}
		if _, err := db.DB.Exec(`INSERT INTO documents
			(care_context_id, abha_address, oid, file_name, mime_type, content)
			VALUES (?, 'ram@sbx', 'o1', ?, 'application/pdf', ?)`,
			cc.id, cc.display+".pdf", []byte("%PDF-1.4 test")); err != nil {
			t.Fatal(err)
		}
	}
}

func parse(t *testing.T, ccID string) map[string]any {
	t.Helper()
	raw, err := FHIRForCareContext("ram@sbx", ccID)
	if err != nil {
		t.Fatalf("build %s: %v", ccID, err)
	}
	var b map[string]any
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	return b
}

func TestBundleShape(t *testing.T) {
	seed(t)
	b := parse(t, "cc-doc")

	if b["type"] != "document" {
		t.Errorf("type = %v, want document", b["type"])
	}
	if b["id"] != "cc-doc" {
		t.Errorf("bundle id = %v, want the care context id", b["id"])
	}
	profiles := b["meta"].(map[string]any)["profile"].([]any)
	if profiles[0] != nrces+"DocumentBundle" {
		t.Errorf("bundle profile = %v", profiles[0])
	}

	// Every urn:uuid reference must resolve to an entry in the same bundle,
	// and the Composition must come first (ABDM rejects a document bundle otherwise).
	urls := map[string]bool{}
	entries := b["entry"].([]any)
	for _, e := range entries {
		urls[e.(map[string]any)["fullUrl"].(string)] = true
	}
	first := entries[0].(map[string]any)["resource"].(map[string]any)
	if first["resourceType"] != "Composition" {
		t.Fatalf("first entry = %v, want Composition", first["resourceType"])
	}
	if first["id"] != "cc-doc" {
		t.Errorf("composition id = %v, want the care context id", first["id"])
	}
	for _, ref := range collectRefs(b) {
		if !urls[ref] {
			t.Errorf("dangling reference %s", ref)
		}
	}
	if len(entries) != 4 { // Composition + Patient + Organization + DocumentReference
		t.Errorf("entries = %d, want 4", len(entries))
	}

	// NRCES requires identifier.type on Patient/Practitioner/Organization (1..1);
	// omitting it fails validation and cascades to every reference pointing at them.
	for _, e := range entries {
		r := e.(map[string]any)["resource"].(map[string]any)
		switch r["resourceType"] {
		case "Patient", "Practitioner", "Organization":
			for _, id := range r["identifier"].([]any) {
				if _, ok := id.(map[string]any)["type"]; !ok {
					t.Errorf("%v identifier is missing the required type", r["resourceType"])
				}
			}
		}
	}

	// Resource.id is [A-Za-z0-9-.]{1,64}; an ABHA address ("ram@sbx") is not.
	for _, e := range entries {
		id := e.(map[string]any)["resource"].(map[string]any)["id"].(string)
		for _, r := range id {
			ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.'
			if !ok {
				t.Errorf("illegal character %q in Resource.id %q", r, id)
			}
		}
	}
}

// Guards care_abdm's import-time `care_context_id: str = uuid()` default, where every
// bundle in a process shares one id.
func TestBundlesAreDistinctPerCareContext(t *testing.T) {
	seed(t)
	doc, rx := parse(t, "cc-doc"), parse(t, "cc-rx")

	if doc["id"] == rx["id"] {
		t.Fatal("two care contexts produced the same bundle id")
	}
	profile := func(b map[string]any) any {
		comp := b["entry"].([]any)[0].(map[string]any)["resource"].(map[string]any)
		return comp["meta"].(map[string]any)["profile"].([]any)[0]
	}
	if profile(doc) != nrces+"HealthDocumentRecord" {
		t.Errorf("doc profile = %v", profile(doc))
	}
	if profile(rx) != nrces+"PrescriptionRecord" {
		t.Errorf("rx profile = %v", profile(rx))
	}
}

// A care context with nothing behind it must fail loudly rather than ship
// whatever bundle happens to be lying around.
func TestMissingRecordIsAnError(t *testing.T) {
	seed(t)
	for _, ccID := range []string{"cc-empty", "cc-bad", "cc-unknown"} {
		if _, err := FHIRForCareContext("ram@sbx", ccID); err == nil {
			t.Errorf("%s: expected an error, got a bundle", ccID)
		}
	}
}

func collectRefs(v any) []string {
	var out []string
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "reference" {
				if s, ok := val.(string); ok {
					out = append(out, s)
				}
				continue
			}
			out = append(out, collectRefs(val)...)
		}
	case []any:
		for _, item := range t {
			out = append(out, collectRefs(item)...)
		}
	}
	return out
}

// A bundle stored as application/fhir+json must go out byte-for-byte as supplied —
// no rebuilding, no re-wrapping.
func TestStoredBundleIsPassedThroughVerbatim(t *testing.T) {
	seed(t)
	raw := `{"resourceType":"Bundle","id":"handmade","type":"document","entry":[]}`

	db.DB.Exec(`INSERT INTO care_context_links
		(care_context_id, abha_address, hi_type, display, status)
		VALUES ('cc-raw', 'ram@sbx', 'Prescription', 'handmade.json', 'LINKED')`)
	db.DB.Exec(`INSERT INTO documents
		(care_context_id, abha_address, oid, file_name, mime_type, content)
		VALUES ('cc-raw', 'ram@sbx', 'o1', 'handmade.json', ?, ?)`, FHIRMediaType, []byte(raw))

	got, err := FHIRForCareContext("ram@sbx", "cc-raw")
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Errorf("bundle was modified\n got: %s\nwant: %s", got, raw)
	}
}
