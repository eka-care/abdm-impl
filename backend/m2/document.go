package m2

import (
	"abdm/backend/db"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// StoreAndLink saves the file to the documents table and triggers HIP-initiated linking.
func StoreAndLink(oid, partnerPtID, abhaAddress, fileName, mimeType string, content []byte) (string, error) {
	ccID := uuid.NewString()

	if _, err := db.DB.Exec(
		`INSERT INTO documents (care_context_id, abha_address, oid, file_name, mime_type, content) VALUES (?, ?, ?, ?, ?, ?)`,
		ccID, abhaAddress, oid, fileName, mimeType, content,
	); err != nil {
		return "", fmt.Errorf("store document: %w", err)
	}

	hipID := os.Getenv("EKA_HIP_ID")
	if err := LinkCareContexts(oid, partnerPtID, hipID, LinkRequest{
		AbhaAddress:   abhaAddress,
		Oid:           oid,
		PartnerUserID: partnerPtID,
		CareContexts: []CareContext{{
			CareContextID: ccID,
			Display:       fileName,
			HiType:        "HealthDocumentRecord",
		}},
	}); err != nil {
		return "", fmt.Errorf("link care context: %w", err)
	}

	db.DB.Exec(
		`INSERT OR IGNORE INTO care_context_links (care_context_id, abha_address, hi_type, display, status) VALUES (?, ?, 'HealthDocumentRecord', ?, 'PENDING')`,
		ccID, abhaAddress, fileName,
	)
	return ccID, nil
}

// FHIRForCareContext builds a FHIR document bundle from the stored file.
func FHIRForCareContext(abhaAddress, ccID string) (string, error) {
	return dummyFHIRBundle(abhaAddress, ccID) // hardcoded test bundle — re-enable to debug against a known-good payload

	//var fileName, mimeType string
	//var content []byte
	//err := db.DB.QueryRow(
	//	`SELECT file_name, mime_type, content FROM documents WHERE care_context_id = ?`, ccID,
	//).Scan(&fileName, &mimeType, &content)
	//if err != nil {
	//	return "", fmt.Errorf("load document %s: %w", ccID, err)
	//}
	//return documentFHIRBundle(abhaAddress, ccID, fileName, mimeType, content)
}

func documentFHIRBundle(abhaAddress, ccID, fileName, mimeType string, content []byte) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	compID := uuid.NewString()
	patID := uuid.NewString()
	docRefID := uuid.NewString()
	bundleID := uuid.NewString()

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           bundleID,
		"meta":         map[string]any{"lastUpdated": now},
		"identifier": map[string]any{
			"system": fmt.Sprintf("https://example.org/bundles/%s", ccID),
			"value":  bundleID,
		},
		"type":      "document",
		"timestamp": now,
		"entry": []any{
			fhirEntry("urn:uuid:"+compID, map[string]any{
				"resourceType": "Composition",
				"id":           compID,
				"status":       "final",
				"type": map[string]any{
					"coding": []any{map[string]any{
						"system":  "http://snomed.info/sct",
						"code":    "419891008",
						"display": "Record artefact",
					}},
				},
				"subject": map[string]any{"reference": "urn:uuid:" + patID},
				"date":    now[:10],
				"author":  []any{map[string]any{"reference": "urn:uuid:" + patID}},
				"title":   fileName,
				"section": []any{map[string]any{
					"title": "Document",
					"entry": []any{map[string]any{"reference": "urn:uuid:" + docRefID}},
				}},
			}),
			fhirEntry("urn:uuid:"+patID, map[string]any{
				"resourceType": "Patient",
				"id":           patID,
				"identifier": []any{map[string]any{
					"system": "http://abdm.gov.in/patients",
					"value":  abhaAddress,
				}},
			}),
			fhirEntry("urn:uuid:"+docRefID, map[string]any{
				"resourceType": "DocumentReference",
				"id":           docRefID,
				"status":       "current",
				"type": map[string]any{
					"coding": []any{map[string]any{
						"system":  "http://snomed.info/sct",
						"code":    "419891008",
						"display": "Record artefact",
					}},
				},
				"subject": map[string]any{"reference": "urn:uuid:" + patID},
				"content": []any{map[string]any{
					"attachment": map[string]any{
						"contentType": mimeType,
						"data":        base64.StdEncoding.EncodeToString(content),
						"title":       fileName,
						"creation":    now,
					},
				}},
			}),
		},
	}

	b, err := json.Marshal(bundle)
	return string(b), err
}
