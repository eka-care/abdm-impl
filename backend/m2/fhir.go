package m2

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"abdm/backend/db"

	"github.com/google/uuid"
)

// FHIR document-bundle generation, ported from care_abdm (abdm/utils/fhir).
//
// care_abdm builds one NRCES DocumentBundle per care context: a Composition whose
// profile/type is chosen by hi_type, followed by every resource the Composition
// references, wired together with urn:uuid full URLs. This is the same thing in Go,
// minus the resource builders care_abdm feeds from its EMR (Condition, Observation,
// MedicationRequest, ...) — here every care context is backed by a stored document,
// so each Composition has one section pointing at a DocumentReference.

const nrces = "https://nrces.in/ndhm/fhir/r4/StructureDefinition/"

// FHIRMediaType marks a stored record that is already a FHIR bundle.
const FHIRMediaType = "application/fhir+json"

// hiTypeSpec is the per-hi_type variation between care_abdm's composition mixins:
// same skeleton, different NRCES profile and SNOMED type coding.
type hiTypeSpec struct {
	profile string // NRCES StructureDefinition name
	code    string // SNOMED CT code; empty means text-only CodeableConcept
	display string
	section string // Composition.section[0].title
}

var hiTypes = map[string]hiTypeSpec{
	"Prescription":         {"PrescriptionRecord", "440545006", "Prescription record", "Prescription"},
	"OPConsultation":       {"OPConsultRecord", "371530004", "Clinical consultation report", "Document Reference"},
	"DischargeSummary":     {"DischargeSummaryRecord", "373942005", "Discharge summary", "Document Reference"},
	"DiagnosticReport":     {"DiagnosticReportRecord", "721981007", "Diagnostic studies report", "Diagnostic Report"},
	"HealthDocumentRecord": {"HealthDocumentRecord", "419891008", "Record artifact", "Document"},
	"WellnessRecord":       {"WellnessRecord", "", "Wellness Record", "Other Observations"},
	"Invoice":              {"InvoiceRecord", "371530004", "Invoice Record", "Invoice Details"},
}

// bundle is care_abdm's FhirBase: a resource cache keyed by caller-supplied string,
// plus the stable urn:uuid each cached resource is referenced by.
type bundle struct {
	entries []map[string]any
	refs    map[string]string
}

func newBundle() *bundle { return &bundle{refs: map[string]string{}} }

// add caches a resource under key (first write wins) and returns a Reference to it.
// Equivalent to care_abdm's @cache_profiles + _reference pair.
func (b *bundle) add(key string, resource map[string]any) map[string]any {
	if _, seen := b.refs[key]; !seen {
		b.refs[key] = "urn:uuid:" + uuid.NewString()
		b.entries = append(b.entries, map[string]any{
			"fullUrl":  b.refs[key],
			"resource": resource,
		})
	}
	return map[string]any{"reference": b.refs[key]}
}

// fhirID coerces a value into a legal Resource.id ([A-Za-z0-9-.]{1,64}) — an ABHA
// address contains "@", which validators reject.
func fhirID(v string) string {
	out := []rune(v)
	for i, r := range out {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
		default:
			out[i] = '-'
		}
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return string(out)
}

// ndhmIdentifierTypeCS is the NRCES-bound value set for identifier.type.
const ndhmIdentifierTypeCS = "https://nrces.in/ndhm/fhir/r4/CodeSystem/ndhm-identifier-type-code"

func ndhmIdentifier(code, display, system, value string) map[string]any {
	return map[string]any{
		"type": map[string]any{
			"coding": []any{map[string]any{
				"system":  ndhmIdentifierTypeCS,
				"code":    code,
				"display": display,
			}},
			"text": display,
		},
		"system": system,
		"value":  value,
	}
}

func narrative(text string) map[string]any {
	return map[string]any{
		"status": "generated",
		"div":    `<div xmlns="http://www.w3.org/1999/xhtml">` + text + `</div>`,
	}
}

func meta(profile string, now string) map[string]any {
	return map[string]any{
		"versionId":   "1",
		"lastUpdated": now,
		"profile":     []any{nrces + profile},
	}
}

func (s hiTypeSpec) concept() map[string]any {
	if s.code == "" {
		return map[string]any{"text": s.display}
	}
	return map[string]any{
		"coding": []any{map[string]any{
			"system":  "http://snomed.info/sct",
			"code":    s.code,
			"display": s.display,
		}},
		"text": s.display,
	}
}

type patientRow struct {
	name        string
	abhaAddress string
	abhaNumber  string
}

func (b *bundle) patient(p patientRow, now string) map[string]any {
	// NRCES Patient requires identifier.type (1..1), and inside it system, code and
	// display are each 1..1. ABHA = the address (self-declared, name@abdm);
	// HIN = the 14-digit ABHA number.
	identifiers := []any{ndhmIdentifier(
		"ABHA", "Ayushman Bharat Health Account (ABHA) ID",
		"https://healthid.ndhm.gov.in", p.abhaAddress)}
	if p.abhaNumber != "" {
		identifiers = append(identifiers, ndhmIdentifier(
			"HIN", "Health ID issued by NDHM",
			"https://healthid.abdm.gov.in", p.abhaNumber))
	}
	// ponytail: no gender/birthDate — the patients table only keeps name + ABHA ids.
	// Persist them from the M1 ABHA profile and add them here; NRCES accepts 0..1 for both.
	return b.add("Patient/"+p.abhaAddress, map[string]any{
		"resourceType": "Patient",
		"id":           fhirID(p.abhaAddress),
		"meta":         meta("Patient", now),
		"text":         narrative(p.name),
		"identifier":   identifiers,
		"name":         []any{map[string]any{"text": p.name}},
	})
}

func (b *bundle) organization(now string) map[string]any {
	hipID := os.Getenv("EKA_HIP_ID")
	name := os.Getenv("EKA_HIP_NAME")
	if name == "" {
		name = hipID
	}
	return b.add("Organization/"+hipID, map[string]any{
		"resourceType": "Organization",
		"id":           fhirID(hipID),
		"meta":         meta("Organization", now),
		"text":         narrative(name),
		"identifier": []any{map[string]any{
			"system": "https://facility.ndhm.gov.in",
			"value":  hipID,
			"type": map[string]any{
				"coding": []any{map[string]any{
					"system":  "http://terminology.hl7.org/CodeSystem/v2-0203",
					"code":    "FI",
					"display": "Facility ID",
				}},
				"text": "Facility ID",
			},
		}},
		"type": []any{map[string]any{
			"coding": []any{map[string]any{
				"system":  "http://terminology.hl7.org/CodeSystem/organization-type",
				"code":    "prov",
				"display": "Healthcare Provider",
			}},
			"text": "Healthcare Provider",
		}},
		"name": name,
	})
}

func (b *bundle) documentReference(doc docRow, subject map[string]any, spec hiTypeSpec, now string) map[string]any {
	return b.add("DocumentReference/"+doc.careContextID, map[string]any{
		"resourceType": "DocumentReference",
		"id":           doc.careContextID,
		"meta":         meta("DocumentReference", now),
		"text":         narrative(doc.fileName),
		"identifier":   []any{map[string]any{"value": doc.careContextID}},
		"status":       "current",
		"type":         spec.concept(),
		"subject":      subject,
		"content": []any{map[string]any{
			"attachment": map[string]any{
				"contentType": doc.mimeType,
				"data":        base64.StdEncoding.EncodeToString(doc.content),
				"title":       doc.fileName,
				"creation":    now,
			},
		}},
	})
}

// buildBundle assembles the whole document for one care context.
func buildBundle(p patientRow, doc docRow, hiType, display string) (string, error) {
	spec, ok := hiTypes[hiType]
	if !ok {
		return "", fmt.Errorf("unsupported hi_type %q for care context %s", hiType, doc.careContextID)
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	b := newBundle()

	// Order matters, as in care_abdm: building the Composition is what populates the
	// resource cache that the bundle entries are then drawn from.
	subject := b.patient(p, now)
	author := b.organization(now)
	entry := b.documentReference(doc, subject, spec, now)

	if display == "" {
		display = doc.fileName
	}

	composition := map[string]any{
		"resourceType": "Composition",
		"id":           doc.careContextID,
		"meta":         meta(spec.profile, now),
		"text":         narrative(display),
		"identifier":   map[string]any{"value": doc.careContextID},
		"status":       "final",
		"type":         spec.concept(),
		"title":        display,
		"date":         now,
		"subject":      subject,
		"author":       []any{author},
		"custodian":    author,
		"section": []any{map[string]any{
			"title": spec.section,
			"code":  spec.concept(),
			"entry": []any{entry},
		}},
	}

	// Composition first, then everything it referenced (care_abdm: cached_profiles()).
	entries := append([]any{map[string]any{
		"fullUrl":  "urn:uuid:" + uuid.NewString(),
		"resource": composition,
	}}, toAny(b.entries)...)

	out := map[string]any{
		"resourceType": "Bundle",
		"id":           doc.careContextID,
		"meta": map[string]any{
			"versionId":   "1",
			"lastUpdated": now,
			"profile":     []any{nrces + "DocumentBundle"},
			"security": []any{map[string]any{
				"system":  "http://terminology.hl7.org/CodeSystem/v3-Confidentiality",
				"code":    "V",
				"display": "very restricted",
			}},
		},
		"identifier": map[string]any{
			"system": identifierSystem() + "/bundle",
			"value":  doc.careContextID,
		},
		"type":      "document",
		"timestamp": now,
		"entry":     entries,
	}

	raw, err := json.Marshal(out)
	return string(raw), err
}

func toAny(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func identifierSystem() string {
	if s := os.Getenv("HIP_IDENTIFIER_SYSTEM"); s != "" {
		return s
	}
	if s := os.Getenv("PUBLIC_BASE_URL"); s != "" {
		return s + "/abdm"
	}
	return baseURL + "/abdm"
}

type docRow struct {
	careContextID string
	fileName      string
	mimeType      string
	content       []byte
}

// FHIRForCareContext loads the record behind a care context and renders it as a
// FHIR document bundle. This is care_abdm's transfer-loop dispatch: hi_type decides
// which document profile is emitted, and a care context with no stored record is
// skipped rather than substituted.
func FHIRForCareContext(abhaAddress, ccID string) (string, error) {
	var (
		hiType, display string
		doc             = docRow{careContextID: ccID}
		fileName        *string
		mimeType        *string
		content         []byte
	)
	err := db.DB.QueryRow(`
		SELECT c.hi_type, c.display, d.file_name, d.mime_type, d.content
		FROM care_context_links c
		LEFT JOIN documents d ON d.care_context_id = c.care_context_id
		WHERE c.care_context_id = ?`, ccID,
	).Scan(&hiType, &display, &fileName, &mimeType, &content)
	if err != nil {
		return "", fmt.Errorf("load care context %s: %w", ccID, err)
	}
	if fileName == nil {
		return "", fmt.Errorf("care context %s has no stored record to render", ccID)
	}
	doc.fileName, doc.mimeType, doc.content = *fileName, *mimeType, content

	// A bundle supplied whole is sent exactly as given — no wrapping, no rebuilding.
	// This is the escape hatch for pushing a known-good or hand-edited bundle.
	if doc.mimeType == FHIRMediaType {
		log.Printf("[fhir] care context %s: using the stored bundle verbatim (%d bytes)", ccID, len(content))
		return string(content), nil
	}

	p := patientRow{abhaAddress: abhaAddress, name: abhaAddress}
	var name, abhaNumber *string
	if err := db.DB.QueryRow(
		`SELECT name, abha_number FROM patients WHERE abha_address = ?`, abhaAddress,
	).Scan(&name, &abhaNumber); err == nil {
		if name != nil && *name != "" {
			p.name = *name
		}
		if abhaNumber != nil {
			p.abhaNumber = *abhaNumber
		}
	}

	return buildBundle(p, doc, hiType, display)
}

// dumpFHIR writes the bundle to disk exactly as it will be sent — before base64 at
// link time, before ECDH at on-fetch time — and logs its absolute path, so a bundle
// ABDM rejects can be inspected or replayed against a validator.
//
// Debug aid: these files hold the full patient record in the clear. Point
// FHIR_DUMP_DIR somewhere disposable and do not enable this in production.
func dumpFHIR(ccID, stage, fhir string) {
	dir := os.Getenv("FHIR_DUMP_DIR")
	if dir == "" {
		dir = "fhir-dumps"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[fhir] dump dir %s: %v", dir, err)
		return
	}

	path := filepath.Join(dir, fmt.Sprintf("%s_%s_%s.json",
		time.Now().UTC().Format("20060102T150405Z"), stage, ccID))
	if err := os.WriteFile(path, []byte(fhir), 0o600); err != nil {
		log.Printf("[fhir] dump %s: %v", path, err)
		return
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	log.Printf("[fhir] raw bundle (%s, %d bytes) → %s", stage, len(fhir), abs)
}
