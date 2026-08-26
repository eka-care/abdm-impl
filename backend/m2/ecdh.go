package m2

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	abdmecdh "github.com/eka-care/abdm-ecdh/go"
)

type hiuKeyInfo struct {
	CryptoAlg  string     `json:"crypto_alg"`
	Curve      string     `json:"curve"`
	Nonce      string     `json:"nonce"`
	DHPublicKey dhPublicKey `json:"dh_public_key"`
}

type dhPublicKey struct {
	KeyValue   string `json:"key_value"`
	Parameters string `json:"parameters"`
	Expiry     string `json:"expiry"`
}

type dataFetchEntry struct {
	CareContextID string `json:"care_context_id"`
	Content       string `json:"content"`
	Checksum      string `json:"checksum"`
	Media         string `json:"media"`
}

type dataFetchRequest struct {
	TransactionID  string         `json:"transaction_id"`
	PageNumber     int            `json:"page_number"`
	PageCount      int            `json:"page_count"`
	KeyInformation hiuKeyInfo     `json:"key_information"`
	Entries        []dataFetchEntry `json:"entries"`
}

// encryptAndPush builds FHIR bundles for each requested care context,
// encrypts them with the HIU's key material, and pushes to the data-on-fetch API.
func encryptAndPush(transactionID, oid, partnerPtID, hipID, abhaAddress string, careContextIDs []string, hiuKey hiuKeyInfo) error {
	ec := abdmecdh.New()
	km, err := ec.GenerateKeyMaterial()
	if err != nil {
		return fmt.Errorf("generate key material: %w", err)
	}

	entries := make([]dataFetchEntry, 0, len(careContextIDs))
	for _, ccID := range careContextIDs {
		fhir, err := FHIRForCareContext(abhaAddress, ccID)
		if err != nil {
			return fmt.Errorf("build fhir bundle for %s: %w", ccID, err)
		}

		encrypted, err := ec.Encrypt(abdmecdh.EncryptionRequest{
			StringToEncrypt:    fhir,
			SenderNonce:        km.Nonce,
			RequesterNonce:     hiuKey.Nonce,
			SenderPrivateKey:   km.PrivateKey,
			RequesterPublicKey: hiuKey.DHPublicKey.KeyValue, // X.509 or uncompressed, library auto-detects
		})
		if err != nil {
			return fmt.Errorf("encrypt care context %s: %w", ccID, err)
		}

		sum := sha256.Sum256([]byte(fhir))
		entries = append(entries, dataFetchEntry{
			CareContextID: ccID,
			Content:       encrypted.EncryptedData,
			Checksum:      fmt.Sprintf("%x", sum),
			Media:         "application/fhir+json",
		})
	}

	payload := dataFetchRequest{
		TransactionID: transactionID,
		PageNumber:    1,
		PageCount:     1,
		KeyInformation: hiuKeyInfo{
			CryptoAlg: "ECDH",
			Curve:     "Curve25519",
			Nonce:     km.Nonce,
			DHPublicKey: dhPublicKey{
				KeyValue:   km.X509PublicKey,
				Parameters: "",
				Expiry:     time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
			},
		},
		Entries: entries,
	}

	token, err := getAccessToken()
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/abdm/v1/hip/care-context/data/on-fetch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Pt-Id", oid)
	req.Header.Set("X-Partner-Pt-Id", partnerPtID)
	req.Header.Set("X-Hip-Id", hipID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("data-on-fetch push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("data-on-fetch returned %d — %s", resp.StatusCode, errBody)
	}
	return nil
}
