package m3

import (
	"abdm/backend/db"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

// POST /api/m3/consent — create a consent request.
func HandleCreateConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Oid         string   `json:"oid"`
		PartnerPtID string   `json:"partner_pt_id"`
		AbhaAddress string   `json:"abha_address"`
		Purpose     string   `json:"purpose"`
		RecordTypes []string `json:"record_types"`
		DateFrom    string   `json:"date_from"` // YYYY-MM-DD
		DateTo      string   `json:"date_to"`
		Expiry      string   `json:"expiry"`
		CareContexts []string `json:"care_contexts"` // optional override; auto-populated if empty
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Oid == "" || req.AbhaAddress == "" {
		http.Error(w, "oid, abha_address required", http.StatusBadRequest)
		return
	}

	// Auto-populate care contexts from linked records for this patient
	var ccIDs []string
	if len(req.CareContexts) == 0 {
		rows, err := db.DB.Query(
			`SELECT care_context_id FROM care_context_links WHERE abha_address=? AND status='LINKED'`,
			req.AbhaAddress,
		)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					ccIDs = append(ccIDs, id)
				}
			}
		}
	} else {
		ccIDs = req.CareContexts
	}

	careContexts := make([]CareContextIdentifier, len(ccIDs))
	for i, id := range ccIDs {
		careContexts[i] = CareContextIdentifier{CcRef: id, PatientRef: req.AbhaAddress}
	}

	if len(req.RecordTypes) == 0 {
		req.RecordTypes = []string{"OPConsultation", "Prescription", "DiagnosticReport",
			"DischargeSummary", "ImmunizationRecord", "HealthDocumentRecord",
			"WellnessRecord", "Invoice"}
	}
	if req.Purpose == "" {
		req.Purpose = "Care management"
	}

	hipID := os.Getenv("EKA_HIP_ID")
	hipName := os.Getenv("EKA_HIP_NAME")
	if hipName == "" {
		hipName = hipID
	}
	clinicID := os.Getenv("EKA_CLINIC_ID")

	apiReq := ConsentCreateReq{
		Patient:       map[string]string{"health_id": req.AbhaAddress, "oid": req.Oid},
		HipIdentifier: map[string]string{"id": hipID},
		Hiu: map[string]any{
			"clinic_id": clinicID,
			"requester": map[string]string{"name": hipName},
		},
		CareContexts: careContexts,
		Period: ConsentPeriod{
			From:   toDatetime(req.DateFrom),
			To:     toDatetime(req.DateTo),
			Expiry: toDatetime(req.Expiry),
		},
		Purpose:     req.Purpose,
		RecordTypes: req.RecordTypes,
	}

	initID, err := CreateConsent(apiReq, req.Oid, req.PartnerPtID)
	if err != nil {
		log.Printf("consent create error: %v", err)
		http.Error(w, "consent creation failed", http.StatusBadGateway)
		return
	}

	hiTypesJSON, _ := json.Marshal(req.RecordTypes)
	db.DB.Exec(
		`INSERT OR IGNORE INTO consents (consent_init_id, patient_abha, oid, purpose, hi_types, date_from, date_to, expiry)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		initID, req.AbhaAddress, req.Oid, req.Purpose, string(hiTypesJSON),
		req.DateFrom, req.DateTo, req.Expiry,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"consent_init_id": initID})
}

// POST /api/m3/consent/list
func HandleListConsents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Oid         string `json:"oid"`
		PartnerPtID string `json:"partner_pt_id"`
		AbhaAddress string `json:"abha_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AbhaAddress == "" {
		http.Error(w, "abha_address required", http.StatusBadRequest)
		return
	}
	consents, err := ListConsents(req.AbhaAddress, req.Oid, req.PartnerPtID)
	if err != nil {
		log.Printf("consent list error: %v", err)
		http.Error(w, "list failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"consents": consents})
}

// POST /api/m3/consent/details
func HandleConsentDetails(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Oid         string `json:"oid"`
		PartnerPtID string `json:"partner_pt_id"`
		AbhaAddress string `json:"abha_address"`
		ConsentID   string `json:"consent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConsentID == "" {
		http.Error(w, "consent_id required", http.StatusBadRequest)
		return
	}
	details, err := ConsentDetails(req.ConsentID, req.AbhaAddress, req.Oid, req.PartnerPtID)
	if err != nil {
		log.Printf("consent details error: %v", err)
		http.Error(w, "details failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": details})
}

// GET /api/m3/consent/{id}/records — locally decrypted + stored FHIR records
func HandleConsentRecords(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rows, err := db.DB.Query(
		`SELECT care_context_id, fhir_json, received_at FROM consent_records WHERE consent_id=? ORDER BY received_at DESC`,
		id,
	)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type record struct {
		CareContextID string `json:"care_context_id"`
		FhirJSON      string `json:"fhir_json"`
		ReceivedAt    string `json:"received_at"`
	}
	var records []record
	for rows.Next() {
		var rec record
		if rows.Scan(&rec.CareContextID, &rec.FhirJSON, &rec.ReceivedAt) == nil {
			records = append(records, rec)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"records": records})
}

// toDatetime converts "YYYY-MM-DD" → "YYYY-MM-DDT00:00:00Z" (ABDM period fields require date-time).
func toDatetime(date string) string {
	if len(date) == 10 {
		return date + "T00:00:00Z"
	}
	return date
}

// OnConsentUpdate handles abha.consent_update webhook data.
func OnConsentUpdate(data json.RawMessage) {
	var event struct {
		Status       string `json:"status"`
		Notification struct {
			ConsentRequestID string `json:"consentRequestId"`
			Status           string `json:"status"`
			ConsentArtefacts []struct {
				ID string `json:"id"`
			} `json:"consentArtefacts"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("[consent_update] parse error: %v — raw: %s", err, data)
		return
	}
	status := strings.ToUpper(event.Notification.Status)
	if status == "" {
		status = strings.ToUpper(event.Status)
	}
	log.Printf("[consent_update] consent_request_id=%s status=%s artefacts=%d",
		event.Notification.ConsentRequestID, status, len(event.Notification.ConsentArtefacts))

	// Map first artefact id as the consent_id
	consentID := ""
	if len(event.Notification.ConsentArtefacts) > 0 {
		consentID = event.Notification.ConsentArtefacts[0].ID
	}
	if consentID != "" {
		db.DB.Exec(
			`UPDATE consents SET consent_id=?, status=?, granted_at=datetime('now') WHERE consent_init_id=?`,
			consentID, status, event.Notification.ConsentRequestID,
		)
	} else {
		db.DB.Exec(
			`UPDATE consents SET status=? WHERE consent_init_id=?`,
			status, event.Notification.ConsentRequestID,
		)
	}
}
