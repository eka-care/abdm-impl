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

	"abdm/backend/m3"
)

// Read at call time: package vars initialise before main() loads ../.env.local.
func baseURL() string {
	if u := os.Getenv("EKA_BASE_URL"); u != "" {
		return u
	}
	return "https://api.eka.care"
}

// Every ABDM call goes through this client: an unbounded default client leaks a
// goroutine per hung gateway connection.
var httpClient = &http.Client{Timeout: 30 * time.Second}

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
	resp, err := httpClient.Post(baseURL()+"/connect-auth/v1/account/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("eka login failed: %d %s", resp.StatusCode, msg)
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
	// Eka only stores the inline bundle when hi_types is non-empty; without it the
	// bundle is silently dropped and the PHR app later shows "data unavailable".
	HiTypes []string `json:"hi_types,omitempty"`
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
	req, _ := http.NewRequest(http.MethodPatch, m3.NdhmURL()+"/abdm/v1/hiu/keyset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	m3.SetAuth(req, token)
	resp, err := httpClient.Do(req)
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
	r, _ := http.NewRequest(http.MethodPost, m3.NdhmURL()+"/abdm/v1/care-contexts/link", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	m3.SetAuth(r, token)
	r.Header.Set("X-Pt-Id", oid)
	r.Header.Set("X-Partner-Pt-Id", partnerPtID)
	r.Header.Set("X-Hip-Id", hipID)
	resp, err := httpClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("link request failed: %d — %s (X-Hip-Id=%q X-Pt-Id=%q)",
			resp.StatusCode, errBody, hipID, oid)
	}
	return nil
}

// OnboardFacility registers the facility with Eka (POST /abdm/v1/hip/onboard).
// One-time per facility, and a hard prerequisite for M2: until it succeeds, every
// care-contexts/link call comes back as {"error":"HFR not found"}. The ABDM-portal
// Software Linkage step (entering Eka's bridge id on the facility dashboard) must
// be done first, otherwise this call has nothing to attach to.
func OnboardFacility(hipID, name, clinicID string) (map[string]any, error) {
	token, err := getAccessToken()
	if err != nil {
		return nil, err
	}
	payload := map[string]string{"hip_id": hipID, "name": name}
	if clinicID != "" {
		payload["clinic_id"] = clinicID
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest(http.MethodPost, m3.NdhmURL()+"/abdm/v1/hip/onboard", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	m3.SetAuth(req, token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("onboard failed: %d — %s", resp.StatusCode, raw)
	}
	var out map[string]any
	return out, json.Unmarshal(raw, &out)
}
