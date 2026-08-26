package m2

import _ "embed"

//go:embed testdata/hardcoded_fhir.json
var hardcodedFHIR string

// dummyFHIRBundle returns the hardcoded test FHIR bundle.
// Swap with real builders once FHIR schema is validated end-to-end.
// HARDCODED FHIR: backend/m2/testdata/hardcoded_fhir.json
func dummyFHIRBundle(_, _ string) (string, error) {
	return hardcodedFHIR, nil
}

func fhirEntry(fullURL string, resource map[string]any) map[string]any {
	return map[string]any{"fullUrl": fullURL, "resource": resource}
}
