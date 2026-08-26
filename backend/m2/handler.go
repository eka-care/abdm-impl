package m2

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"abdm/backend/db"
	"abdm/backend/m3"
)

// GET /api/m2/care-context/{id} — return care context status + document metadata.
func HandleGetCareContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	var result struct {
		CareContextID string  `json:"care_context_id"`
		AbhaAddress   string  `json:"abha_address"`
		HiType        string  `json:"hi_type"`
		Display       string  `json:"display"`
		Status        string  `json:"status"`
		LinkedAt      *string `json:"linked_at,omitempty"`
		Error         *string `json:"error,omitempty"`
		// document fields if present
		FileName *string `json:"file_name,omitempty"`
		MimeType *string `json:"mime_type,omitempty"`
	}

	row := db.DB.QueryRow(`
		SELECT c.care_context_id, c.abha_address, c.hi_type, c.display, c.status,
		       c.linked_at, c.error, d.file_name, d.mime_type
		FROM care_context_links c
		LEFT JOIN documents d ON d.care_context_id = c.care_context_id
		WHERE c.care_context_id = ?`, id)

	if err := row.Scan(
		&result.CareContextID, &result.AbhaAddress, &result.HiType, &result.Display, &result.Status,
		&result.LinkedAt, &result.Error, &result.FileName, &result.MimeType,
	); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// POST /api/m2/link — trigger HIP-initiated care context linking.
func HandleLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Oid          string        `json:"oid"`
		PartnerPtID  string        `json:"partner_pt_id"`
		AbhaAddress  string        `json:"abha_address"`
		CareContexts []CareContext `json:"care_contexts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Oid == "" || req.AbhaAddress == "" || len(req.CareContexts) == 0 {
		http.Error(w, "oid, abha_address, care_contexts required", http.StatusBadRequest)
		return
	}
	hipID := os.Getenv("EKA_HIP_ID")
	if err := LinkCareContexts(req.Oid, req.PartnerPtID, hipID, LinkRequest{
		AbhaAddress:   req.AbhaAddress,
		Oid:           req.Oid,
		PartnerUserID: req.PartnerPtID,
		CareContexts:  req.CareContexts,
	}); err != nil {
		log.Printf("link error: %v", err)
		http.Error(w, "link failed", http.StatusBadGateway)
		return
	}
	for _, cc := range req.CareContexts {
		db.DB.Exec(`INSERT OR IGNORE INTO care_context_links
			(care_context_id, abha_address, hi_type, display, status)
			VALUES (?, ?, ?, ?, 'PENDING')`,
			cc.CareContextID, req.AbhaAddress, cc.HiType, cc.Display)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
}

// POST /api/m2/link-document — store a PDF/image and link it as HealthDocumentRecord.
func HandleLinkDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Oid         string `json:"oid"`
		PartnerPtID string `json:"partner_pt_id"`
		AbhaAddress string `json:"abha_address"`
		FileName    string `json:"file_name"`
		MimeType    string `json:"mime_type"`
		Content     string `json:"content"` // base64-encoded file bytes
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Oid == "" || req.AbhaAddress == "" || req.Content == "" {
		http.Error(w, "oid, abha_address, content required", http.StatusBadRequest)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		http.Error(w, "content must be base64", http.StatusBadRequest)
		return
	}
	ccID, err := StoreAndLink(req.Oid, req.PartnerPtID, req.AbhaAddress, req.FileName, req.MimeType, raw)
	if err != nil {
		log.Printf("link-document error: %v", err)
		http.Error(w, "link failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"care_context_id": ccID, "status": "pending"})
}

// POST /webhooks/eka — single inbound webhook endpoint; routes on event_type.
func HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Log every incoming webhook for visibility
	log.Printf("[webhook] headers: Eka-Webhook-Signature=%q", r.Header.Get("Eka-Webhook-Signature"))
	log.Printf("[webhook] body: %s", body)

	if !verifySignature(body, r.Header.Get("Eka-Webhook-Signature")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var payload struct {
		EventType     string          `json:"event"`
		TransactionID string          `json:"transaction_id"`
		Data          json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[webhook] failed to parse body: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Printf("[webhook] event=%s transaction_id=%s", payload.EventType, payload.TransactionID)

	switch payload.EventType {
	case "abha.link_care_context":
		onLinkResult(payload.Data)
	case "abha.hip_data_fetch":
		onDataFetch(payload.TransactionID, payload.Data)
	case "abha.consent_update":
		m3.OnConsentUpdate(payload.Data)
	case "abha.hiu_data_push":
		go m3.OnHiuDataPush(payload.TransactionID, payload.Data)
	case "abha.discover_care_context":
		onDiscover(w, payload.Data)
		return
	default:
		log.Printf("[webhook] unhandled event: %s — data: %s", payload.EventType, payload.Data)
	}
	w.WriteHeader(http.StatusOK)
}

// verifySignature checks Eka's webhook signature.
// Header format: "t=<unix_ts>,v1=<hmac_sha256(ts.body)>"
// Rejects payloads older than 3 minutes.
func verifySignature(body []byte, sig string) bool {
	secret := os.Getenv("EKA_WEBHOOK_SECRET")
	if secret == "" {
		return true // skip in dev
	}
	// parse t=... v1=...
	var ts, v1 string
	for _, part := range strings.Split(sig, ",") {
		if strings.HasPrefix(part, "t=") {
			ts = strings.TrimPrefix(part, "t=")
		} else if strings.HasPrefix(part, "v1=") {
			v1 = strings.TrimPrefix(part, "v1=")
		}
	}
	if ts == "" || v1 == "" {
		log.Printf("webhook: malformed signature header: %q", sig)
		return false
	}
	// replay-attack guard: reject if older than 3 minutes
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Now().Unix()-tsInt > 180 {
		log.Printf("webhook: signature timestamp too old or invalid: %s", ts)
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%s", ts, body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(v1), []byte(expected))
}

func onLinkResult(data json.RawMessage) {
	var event struct {
		CareContextID string `json:"care_context_id"`
		Status        string `json:"status"` // LINKED | ERRORED
		Error         string `json:"error"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("link webhook parse error: %v", err)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	db.DB.Exec(`UPDATE care_context_links SET status=?, linked_at=?, error=? WHERE care_context_id=?`,
		event.Status, now, event.Error, event.CareContextID)
	log.Printf("care context %s → %s", event.CareContextID, event.Status)
}

func onDataFetch(webhookTxID string, data json.RawMessage) {
	var event struct {
		AbhaAddress      string     `json:"abha_address"`
		Oid              string     `json:"oid"`
		PartnerPatientID string     `json:"partner_patient_id"`
		HipID            string     `json:"hip_id"`
		CareContexts     []string   `json:"care_contexts"`
		KeyInformation   hiuKeyInfo `json:"key_information"`
		// ABDM transaction ID — must use this (not the outer webhook tx id) for the on-fetch push
		TransactionID string `json:"transaction_id"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("hip_data_fetch parse error: %v", err)
		return
	}
	// Prefer the inner ABDM transaction_id; fall back to webhook envelope id
	txID := event.TransactionID
	if txID == "" {
		txID = webhookTxID
	}
	hipID := os.Getenv("EKA_HIP_ID")
	if event.HipID != "" {
		hipID = event.HipID
	}
	// partner_patient_id is not always present; use oid as fallback
	partnerPtID := event.PartnerPatientID
	if partnerPtID == "" {
		partnerPtID = event.Oid
	}
	log.Printf("[data-fetch] tx=%s oid=%s care_contexts=%v", txID, event.Oid, event.CareContexts)
	go func() {
		if err := encryptAndPush(
			txID,
			event.Oid,
			partnerPtID,
			hipID,
			event.AbhaAddress,
			event.CareContexts,
			event.KeyInformation,
		); err != nil {
			log.Printf("data-on-fetch failed (tx=%s): %v", txID, err)
		} else {
			log.Printf("data-on-fetch pushed (tx=%s, %d contexts)", txID, len(event.CareContexts))
		}
	}()
}

func onDiscover(w http.ResponseWriter, data json.RawMessage) {
	// TODO: match patient by name+gender+YOB+mobile against patients table,
	// return unlinked care contexts. Skill: care-contexts/discover/introduction.md
	log.Printf("[webhook] discover (not yet implemented): %s", data)
	w.WriteHeader(http.StatusOK)
}

