package httpapi

import (
	"io"
	"net/http"
)

// writeRawData answers {"success":true,"data":<raw>} with raw spliced in as
// it came from the service.
//
// raw is the service's own json.Marshal output. Handing it to json.Encoder as
// a json.RawMessage made the encoder walk every byte again — validate,
// compact, HTML-escape — which changes nothing on json.Marshal output (already
// compact, already escaped) and is not free: a 70-inquiry company's list is
// ~577 KB, and that walk was ~15% of the list's CPU (2026-10-04 profile).
//
// The bytes on the wire are the ones the encoder wrote, trailing newline
// included; TestRawDataIsWhatTheEncoderWrote holds that. An empty raw is
// dropped as `omitempty` dropped it.
//
// Only for json.Marshal output. Nothing here checks raw: anything else —
// protojson, which adds whitespace on purpose, or hand-built text — would go
// out as given, where the encoder used to compact it or refuse it.
func writeRawData(w http.ResponseWriter, raw string) {
	w.Header().Set("Content-Type", "application/json")
	if raw == "" {
		_, _ = io.WriteString(w, `{"success":true}`+"\n")
		return
	}
	_, _ = io.WriteString(w, `{"success":true,"data":`)
	_, _ = io.WriteString(w, raw)
	_, _ = io.WriteString(w, "}\n")
}
