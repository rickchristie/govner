package antigravity

import (
	"encoding/json"
	"strings"
)

const ProofMarker = "COOPER_ANTIGRAVITY_OK"

// ProofCommand bounds both native waiting and process lifetime. Do not use
// auto-approval here: the prompt needs no tools or workspace changes.
const ProofCommand = `timeout --kill-after=5s 45s agy --print='Reply with only COOPER_ANTIGRAVITY_OK. Do not use tools.' --print-timeout=35s --output-format=json 2>&1`

// CompleteProof checks the native 1.2.2 result contract, observed with an
// isolated local model fixture. A partial response can exit zero. It cannot
// pass unless the final status, exact answer, turn, and usage all agree.
func CompleteProof(output string) bool {
	if len(output) > 64<<10 {
		return false
	}
	var result struct {
		Conversation string `json:"conversation_id"`
		Status       string `json:"status"`
		Response     string `json:"response"`
		Turns        int    `json:"num_turns"`
		Usage        struct {
			Output int `json:"output_tokens"`
		} `json:"usage"`
		Error    json.RawMessage `json:"error"`
		TimedOut bool            `json:"timed_out"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return false
	}
	return result.Conversation != "" && result.Status == "SUCCESS" && strings.TrimSpace(result.Response) == ProofMarker && result.Turns == 1 && result.Usage.Output > 0 && len(result.Error) == 0 && !result.TimedOut
}
