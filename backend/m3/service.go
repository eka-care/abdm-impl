package m3

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Read at call time: package vars initialise before main() loads ../.env.local.
func base() string {
	if u := os.Getenv("EKA_BASE_URL"); u != "" {
		return u
	}
	return "https://api.eka.care"
}

// NdhmURL is where /abdm/* calls go: Eka's API gateway by default, or a locally running ndhm
// when NDHM_BASE_URL is set (login still goes to EKA_BASE_URL).
func NdhmURL() string {
	if u := os.Getenv("NDHM_BASE_URL"); u != "" {
		return u
	}
	return base()
}

// SetAuth adds the bearer token. When calling ndhm directly there is no gateway to turn the
// token into the jwt-payload header ndhm authenticates with, so add that too.
func SetAuth(r *http.Request, token string) {
	r.Header.Set("Authorization", "Bearer "+token)
	if os.Getenv("NDHM_BASE_URL") == "" {
		return
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return
	}
	if claims, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
		r.Header.Set("jwt-payload", string(claims))
	}
}

var (
	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time
)

func getToken() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if cachedToken != "" && time.Now().Before(expiresAt) {
		return cachedToken, nil
	}
	body, _ := json.Marshal(map[string]string{
		"client_id":     os.Getenv("NEXT_PUBLIC_EKA_CLIENT_ID"),
		"client_secret": os.Getenv("EKA_CLIENT_SECRET"),
	})
	resp, err := http.Post(base()+"/connect-auth/v1/account/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	cachedToken = r.AccessToken
	expiresAt = time.Now().Add(time.Duration(r.ExpiresIn-30) * time.Second)
	return cachedToken, nil
}

func ekaPost(path, oid, partnerPtID string, reqBody, out any) error {
	token, err := getToken()
	if err != nil {
		return err
	}
	b, _ := json.Marshal(reqBody)
	log.Printf("[m3] POST %s oid=%s partner=%s body=%s", path, oid, partnerPtID, b)
	req, _ := http.NewRequest(http.MethodPost, NdhmURL()+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	SetAuth(req, token)
	req.Header.Set("X-Pt-Id", oid)
	req.Header.Set("X-Partner-Pt-Id", partnerPtID)
	req.Header.Set("X-Hip-Id", os.Getenv("EKA_HIP_ID"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[m3] POST %s → %d %s", path, resp.StatusCode, respBody)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("eka %s returned %d — %s", path, resp.StatusCode, respBody)
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// ── Create ────────────────────────────────────────────────────────────────────

type ConsentPeriod struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Expiry string `json:"expiry"`
}

type CareContextIdentifier struct {
	CcRef      string `json:"cc_ref"`
	PatientRef string `json:"patient_ref"`
}

type ConsentCreateReq struct {
	Patient       map[string]string    `json:"patient"`
	HipIdentifier map[string]string    `json:"hip_identifier"`
	Hiu           map[string]any       `json:"hiu"`
	CareContexts  []CareContextIdentifier `json:"care_contexts"`
	Period        ConsentPeriod        `json:"period"`
	Purpose       string               `json:"purpose"`
	RecordTypes   []string             `json:"record_types"`
}

func CreateConsent(req ConsentCreateReq, oid, partnerPtID string) (string, error) {
	var out struct {
		ConsentInitID string `json:"consent_init_id"`
	}
	if err := ekaPost("/abdm/v1/consents/create", oid, partnerPtID, req, &out); err != nil {
		return "", err
	}
	return out.ConsentInitID, nil
}

// ── List ──────────────────────────────────────────────────────────────────────

type ConsentSummary struct {
	ConsentID     string        `json:"consent_id"`
	ConsentInitID string        `json:"consent_init_id"`
	Status        string        `json:"status"`
	HiTypes       []string      `json:"hi_types"`
	Period        ConsentPeriod `json:"period"`
	CreatedAt     string        `json:"c_at"`
	UpdatedAt     string        `json:"u_at"`
}

func ListConsents(abhaAddress, oid, partnerPtID string) ([]ConsentSummary, error) {
	body := map[string]any{
		"patient":        map[string]string{"health_id": abhaAddress, "oid": oid},
		"hip_identifier": map[string]string{"id": os.Getenv("EKA_HIP_ID")},
	}
	var out struct {
		Consents []ConsentSummary `json:"consents"`
	}
	if err := ekaPost("/abdm/v1/consents/list", oid, partnerPtID, body, &out); err != nil {
		return nil, err
	}
	return out.Consents, nil
}

// ── Details ───────────────────────────────────────────────────────────────────

type ConsentDetailDoc struct {
	ID     string `json:"id"`
	FhirURL string `json:"fhir_url"`
}

type ConsentDetailCC struct {
	ID        string             `json:"id"`
	Display   string             `json:"display"`
	Status    string             `json:"status"`
	CreatedAt string             `json:"created_at"`
	Documents []ConsentDetailDoc `json:"documents"`
}

type ConsentDetailHIP struct {
	HIP          map[string]string `json:"hip"`
	CareContexts []ConsentDetailCC `json:"care_contexts"`
}

func ConsentDetails(consentID, abhaAddress, oid, partnerPtID string) ([]ConsentDetailHIP, error) {
	body := map[string]string{"consent_id": consentID, "health_id": abhaAddress}
	var out struct {
		Data []ConsentDetailHIP `json:"data"`
	}
	if err := ekaPost("/abdm/v1/consents/details", oid, partnerPtID, body, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
