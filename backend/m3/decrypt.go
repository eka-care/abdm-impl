package m3

import (
	"abdm/backend/db"
	"encoding/json"
	"log"
	"os"

	abdmecdh "github.com/eka-care/abdm-ecdh/go"
)

type hiuPushEntry struct {
	CareContextID string `json:"care_context_id"`
	Content       string `json:"content"` // encrypted base64
	Checksum      string `json:"checksum"`
	Media         string `json:"media"`
}

type hiuPushKeyInfo struct {
	Nonce       string `json:"nonce"`
	DHPublicKey struct {
		KeyValue string `json:"key_value"`
	} `json:"dh_public_key"`
}

// OnHiuDataPush decrypts received health data and stores it.
func OnHiuDataPush(transactionID string, data json.RawMessage) {
	var event struct {
		ConsentID   string         `json:"consent_id"`
		Entries     []hiuPushEntry `json:"entries"`
		KeyInfo     hiuPushKeyInfo `json:"key_information"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("[hiu_data_push] parse error: %v — raw: %s", err, data)
		return
	}
	log.Printf("[hiu_data_push] tx=%s consent=%s entries=%d", transactionID, event.ConsentID, len(event.Entries))

	privKey := os.Getenv("HIP_PRIVATE_KEY")
	nonce := os.Getenv("HIP_NONCE")
	if privKey == "" || nonce == "" {
		log.Printf("[hiu_data_push] HIP_PRIVATE_KEY/HIP_NONCE not set — cannot decrypt")
		return
	}

	ec := abdmecdh.New()
	for _, entry := range event.Entries {
		result, err := ec.Decrypt(abdmecdh.DecryptionRequest{
			EncryptedData:       entry.Content,
			RequesterNonce:      nonce,
			SenderNonce:         event.KeyInfo.Nonce,
			RequesterPrivateKey: privKey,
			SenderPublicKey:     event.KeyInfo.DHPublicKey.KeyValue,
		})
		if err != nil {
			log.Printf("[hiu_data_push] decrypt failed for %s: %v", entry.CareContextID, err)
			continue
		}
		db.DB.Exec(
			`INSERT OR REPLACE INTO consent_records (consent_id, care_context_id, fhir_json) VALUES (?, ?, ?)`,
			event.ConsentID, entry.CareContextID, result.DecryptedData,
		)
		log.Printf("[hiu_data_push] stored care_context=%s consent=%s", entry.CareContextID, event.ConsentID)
	}
}
