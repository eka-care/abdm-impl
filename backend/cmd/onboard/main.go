// One-time facility onboarding to Eka. Run once per facility, after the ABDM-portal
// Software Linkage step:
//
//	cd backend && go run ./cmd/onboard
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"abdm/backend/m2"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load("../.env.local"); err != nil {
		log.Printf("no ../.env.local loaded (%v) — relying on the ambient environment", err)
	}
	hipID, name, clinicID := os.Getenv("EKA_HIP_ID"), os.Getenv("EKA_HIP_NAME"), os.Getenv("EKA_CLINIC_ID")
	if hipID == "" || name == "" {
		log.Fatal("EKA_HIP_ID and EKA_HIP_NAME must be set")
	}

	log.Printf("onboarding %s (%s) clinic_id=%q", hipID, name, clinicID)
	out, err := m2.OnboardFacility(hipID, name, clinicID)
	if err != nil {
		log.Fatalf("%v", err)
	}
	pretty, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(pretty))
}
