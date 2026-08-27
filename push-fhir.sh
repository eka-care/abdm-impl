#!/usr/bin/env bash
# Link a care context using a FHIR bundle you already have, bypassing the builder.
# The bundle is sent verbatim — inline at link time, and again on any hip_data_fetch.
#
#   ./push-fhir.sh                          # pushes backend/m2/testdata/hardcoded_fhir.json
#   ./push-fhir.sh bundle.json              # hi_type inferred from the Composition profile
#   ./push-fhir.sh bundle.json Prescription # or state it explicitly
#
# ABHA address and oid come from the env; override per-call if you need to.
set -euo pipefail

FILE=${1:-backend/m2/testdata/hardcoded_fhir.json}
HI_TYPE=${2:-}
ABHA=${ABHA_ADDRESS:-blabla002@abdm}
OID=${PT_OID:-178766041216489}
BACKEND=${GO_BACKEND_URL:-http://localhost:8080}

python3 - "$FILE" "$HI_TYPE" "$ABHA" "$OID" <<'PY' | curl -sS -X POST "$BACKEND/api/m2/link-document" \
     -H 'Content-Type: application/json' --data-binary @- -w '\n'
import base64, json, sys, os

path, hi_type, abha, oid = sys.argv[1:5]
raw = open(path, 'rb').read()
bundle = json.loads(raw)  # fail here rather than at the server

# hi_type has to match the Composition inside the bundle, or the PHR app files the
# record under the wrong heading. Derive it from the NRCES profile when not given.
PROFILE_TO_HI_TYPE = {
    "PrescriptionRecord": "Prescription",
    "OPConsultRecord": "OPConsultation",
    "DischargeSummaryRecord": "DischargeSummary",
    "DiagnosticReportRecord": "DiagnosticReport",
    "HealthDocumentRecord": "HealthDocumentRecord",
    "WellnessRecord": "WellnessRecord",
    "InvoiceRecord": "Invoice",
}
if not hi_type:
    profiles = [
        p.rsplit("/", 1)[-1]
        for e in bundle.get("entry", [])
        if e.get("resource", {}).get("resourceType") == "Composition"
        for p in e["resource"].get("meta", {}).get("profile", [])
    ]
    for p in profiles:
        if p in PROFILE_TO_HI_TYPE:
            hi_type = PROFILE_TO_HI_TYPE[p]
            break
    if not hi_type:
        sys.exit(f"cannot infer hi_type from Composition profile {profiles} — pass it as the 2nd argument")
    print(f"hi_type: {hi_type} (from {p})", file=sys.stderr)

json.dump({
    "oid": oid,
    "partner_pt_id": oid,
    "abha_address": abha,
    "file_name": os.path.basename(path),
    "mime_type": "application/fhir+json",
    "hi_type": hi_type,
    "content": base64.b64encode(raw).decode(),
}, sys.stdout)
PY
