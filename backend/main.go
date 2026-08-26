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
	// Load ../.env.local (Next.js convention); ignore error if file absent
	_ = godotenv.Load("../.env.local")

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
