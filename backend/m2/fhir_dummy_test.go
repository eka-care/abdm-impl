package m2

import "testing"

func TestDummyFHIR(t *testing.T) {
	t.Setenv("DUMMY_FHIR", "true")
	got, err := FHIRForCareContext("ram@sbx", "no-such-cc") // no DB row needed
	if err != nil || got != dummyFHIR || len(got) == 0 {
		t.Fatalf("want embedded dummy bundle, got err=%v len=%d", err, len(got))
	}
}
