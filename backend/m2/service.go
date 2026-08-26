package m2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)


var baseURL = func() string {
	if u := os.Getenv("EKA_BASE_URL"); u != "" {
		return u
	}
	return "https://api.eka.care"
}()

var (
	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time
)

func getAccessToken() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if cachedToken != "" && time.Now().Before(expiresAt) {
		return cachedToken, nil
	}
	body, _ := json.Marshal(map[string]string{
		"client_id":     os.Getenv("NEXT_PUBLIC_EKA_CLIENT_ID"),
		"client_secret": os.Getenv("EKA_CLIENT_SECRET"),
	})
	resp, err := http.Post(baseURL+"/connect-auth/v1/account/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("eka login failed: %d", resp.StatusCode)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	cachedToken = result.AccessToken
	expiresAt = time.Now().Add(time.Duration(result.ExpiresIn-30) * time.Second)
	return cachedToken, nil
}

// CareContext is one logical record group sent to ABDM.
// Set Data to a base64 FHIR bundle to let Eka store it (Option A — no on-fetch webhook needed).
type CareContext struct {
	CareContextID string `json:"care_context_id"`
	Display       string `json:"display"`
	HiType        string `json:"hi_type"` // OPConsultation | DiagnosticReport | Prescription | ...
	Data          string `json:"data,omitempty"`
}

type LinkRequest struct {
	AbhaAddress   string        `json:"abha_address"`
	CareContexts  []CareContext `json:"care_contexts"`
	Oid           string        `json:"oid"`
	PartnerUserID string        `json:"partner_user_id"`
}

// RegisterPublicKey pushes the static HIP public key to Eka's HIU keyset endpoint.
// Call once at startup. Uses HIP_X509_PUBLIC_KEY and HIP_NONCE from env.
func RegisterPublicKey() error {
	pubKey := os.Getenv("HIP_X509_PUBLIC_KEY")
	nonce := os.Getenv("HIP_NONCE")
	if pubKey == "" || nonce == "" {
		return fmt.Errorf("HIP_X509_PUBLIC_KEY and HIP_NONCE must be set")
	}
	token, err := getAccessToken()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"public_key": pubKey, "nonce": nonce})
	req, _ := http.NewRequest(http.MethodPatch, baseURL+"/abdm/v1/hiu/keyset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register key failed: %d — %s", resp.StatusCode, errBody)
	}
	return nil
}

// LinkCareContexts calls POST /abdm/v1/care-contexts/link (returns 202 — result via webhook).
func LinkCareContexts(oid, partnerPtID, hipID string, req LinkRequest) error {
	token, err := getAccessToken()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(req)
	r, _ := http.NewRequest(http.MethodPost, baseURL+"/abdm/v1/care-contexts/link", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Pt-Id", oid)
	r.Header.Set("X-Partner-Pt-Id", partnerPtID)
	r.Header.Set("X-Hip-Id", hipID)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("link request failed: %d — %s", resp.StatusCode, errBody)
	}
	return nil
}
