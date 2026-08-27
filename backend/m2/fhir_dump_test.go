package m2

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDumpWritesRawBundle(t *testing.T) {
	seed(t)
	dir := t.TempDir()
	t.Setenv("FHIR_DUMP_DIR", dir)

	fhir, err := FHIRForCareContext("ram@sbx", "cc-rx")
	if err != nil {
		t.Fatal(err)
	}
	dumpFHIR("cc-rx", "link", fhir)

	found, _ := filepath.Glob(filepath.Join(dir, "*_link_cc-rx.json"))
	if len(found) != 1 {
		t.Fatalf("expected one dump file, got %v", found)
	}
	onDisk, _ := os.ReadFile(found[0])
	if string(onDisk) != fhir {
		t.Error("dumped bytes differ from what gets encrypted")
	}
	info, _ := os.Stat(found[0])
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %v, want 0600 (contains PHI)", info.Mode().Perm())
	}
	t.Logf("wrote %s", found[0])
}
