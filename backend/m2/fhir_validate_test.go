package m2

import (
	"os"
	"testing"
)

func TestGenerateForValidator(t *testing.T) {
	out := os.Getenv("VALIDATE_OUT")
	if out == "" {
		t.Skip("set VALIDATE_OUT")
	}
	seed(t)
	fhir, err := FHIRForCareContext("ram@sbx", "cc-doc")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte(fhir), 0o600); err != nil {
		t.Fatal(err)
	}
}
