package agent

import (
	"strings"
)

// getMaxRetries returns higher max retries for B.AI/DeepSeek providers
// to handle rate limits gracefully. Other providers use default (2).
func getMaxRetries(model Model) *int {
	if strings.HasPrefix(model.ModelCfg.Provider, "b-ai") {
		n := 10 // More retries for rate-limited providers
		return &n
	}
	return nil // Use fantasy default (2)
}
