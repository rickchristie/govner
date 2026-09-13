package antigravity

import (
	"strings"
	"testing"
)

func TestNativeProofRequiresCompleteResponse(t *testing.T) {
	const complete = `{"conversation_id":"fixture-conversation","status":"SUCCESS","response":"COOPER_ANTIGRAVITY_OK\n","duration_seconds":0.05,"num_turns":1,"usage":{"output_tokens":1}}`
	if !CompleteProof(complete) {
		t.Fatal("complete native response rejected")
	}
	for _, output := range []string{
		"1.2.2", ProofMarker, "error: " + ProofMarker, complete + "\nerror: timeout",
		strings.Replace(complete, "SUCCESS", "TIMEOUT", 1),
		strings.Replace(complete, "SUCCESS", "ERROR", 1),
		strings.Replace(complete, ProofMarker, "partial "+ProofMarker, 1),
		strings.Replace(complete, `"num_turns":1`, `"num_turns":0`, 1),
		strings.Replace(complete, `"output_tokens":1`, `"output_tokens":0`, 1),
		strings.Replace(complete, `"status":`, `"timed_out":true,"status":`, 1),
		strings.Replace(complete, `"status":`, `"error":"permission denied","status":`, 1),
		strings.Repeat(" ", 64<<10) + complete,
	} {
		if CompleteProof(output) {
			t.Fatalf("incomplete or failed response accepted: %.150s", output)
		}
	}
}
