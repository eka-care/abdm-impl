package main

import (
	"abdm/backend/db"
	"abdm/backend/m2"
	"abdm/backend/m3"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	// Load ../.env.local (Next.js convention). Path is CWD-relative, so this only
	// resolves when the binary runs from backend/ — say so instead of silently
	// starting with an empty environment.
	if err := godotenv.Load("../.env.local"); err != nil {
		log.Printf("no ../.env.local loaded (%v) — relying on the ambient environment", err)
	}

	// X-Hip-Id goes out on every ABDM call; an empty or unregistered one comes back
	// from Eka as "HFR not found", which reads like a gateway problem but is config.
	hipID := os.Getenv("EKA_HIP_ID")
	if hipID == "" {
		log.Fatal("EKA_HIP_ID is required — set it to the HFR ID onboarded to your Eka account")
	}
	log.Printf("HIP: %s (%s)", hipID, os.Getenv("EKA_HIP_NAME"))

	if err := db.Init(); err != nil {
		log.Fatalf("db init: %v", err)
	}

	if err := m2.RegisterPublicKey(); err != nil {
		log.Fatalf("register HIP public key: %v", err)
	}
	log.Println("HIP public key registered with Eka")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/m2/link", m2.HandleLink)
	mux.HandleFunc("POST /api/m2/link-document", m2.HandleLinkDocument)
	mux.HandleFunc("GET /api/m2/care-context/{id}", m2.HandleGetCareContext)
	mux.HandleFunc("POST /api/m3/consent", m3.HandleCreateConsent)
	mux.HandleFunc("POST /api/m3/consent/list", m3.HandleListConsents)
	mux.HandleFunc("POST /api/m3/consent/details", m3.HandleConsentDetails)
	mux.HandleFunc("GET /api/m3/consent/{id}/records", m3.HandleConsentRecords)
	mux.HandleFunc("POST /webhooks/eka", m2.HandleWebhook)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("backend listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, corsMiddleware(mux)))
}

// corsMiddleware allows Next.js server-side fetch (same host, different port in dev).
func corsMiddleware(next http.Handler) http.Handler {
	allowed := os.Getenv("CORS_ORIGIN")
	if allowed == "" {
		allowed = "http://localhost:3000"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowed)
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
