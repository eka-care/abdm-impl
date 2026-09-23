package m2

import (
	"abdm/backend/db"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
)

// StoreAndLink saves the file to the documents table and triggers HIP-initiated linking.
func StoreAndLink(oid, partnerPtID, abhaAddress, fileName, mimeType, hiType string, content []byte) (string, error) {
	if DummyFHIR() {
		hiType = "DiagnosticReport" // must match the dummy bundle's Composition type
	} else if hiType == "" {
		hiType = "HealthDocumentRecord"
	}
	ccID := uuid.NewString()

	if _, err := db.DB.Exec(
		`INSERT INTO documents (care_context_id, abha_address, oid, file_name, mime_type, content) VALUES (?, ?, ?, ?, ?, ?)`,
		ccID, abhaAddress, oid, fileName, mimeType, content,
	); err != nil {
		return "", fmt.Errorf("store document: %w", err)
	}

	if _, err := db.DB.Exec(
		`INSERT OR IGNORE INTO care_context_links (care_context_id, abha_address, hi_type, display, status) VALUES (?, ?, ?, ?, 'PENDING')`,
		ccID, abhaAddress, hiType, fileName,
	); err != nil {
		return "", fmt.Errorf("record care context: %w", err)
	}

	// Send the bundle inline (Eka's recommended path): Eka stores it and serves the
	// HIU itself, so the record stays retrievable when this process is not running.
	// Omitting it opts into the abha.hip_data_fetch webhook instead, which only fires
	// while we are reachable. Fall back to that if the bundle cannot be built.
	cc := CareContext{
		CareContextID: ccID,
		Display:       fileName,
		HiType:        hiType,
	}
	if fhir, err := FHIRForCareContext(abhaAddress, ccID); err != nil {
		log.Printf("care context %s: no inline bundle (%v) — falling back to hip_data_fetch", ccID, err)
	} else {
		dumpFHIR(ccID, "link", fhir)
		cc.Data = base64.StdEncoding.EncodeToString([]byte(fhir))
		cc.HiTypes = []string{hiType}
	}

	hipID := os.Getenv("EKA_HIP_ID")
	if err := LinkCareContexts(oid, partnerPtID, hipID, LinkRequest{
		AbhaAddress:   abhaAddress,
		Oid:           oid,
		PartnerUserID: partnerPtID,
		CareContexts:  []CareContext{cc},
	}); err != nil {
		return "", fmt.Errorf("link care context: %w", err)
	}

	return ccID, nil
}
