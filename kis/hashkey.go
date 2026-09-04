package kis

import "encoding/json"

// Hash-key signing is intentionally absent: this read-only library has no
// account-mutation API that could use a hash key.
func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
