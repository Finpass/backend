package crypto

import (
	"encoding/json"
	"fmt"
)

// ProtectJSON stores a value in a JSONB column. With keys, the JSON text is sealed and stored as a JSON string
// ("v1:..."), so the column holds nothing readable; without keys (development) it stores the value as it is.
func (k *Keyring) ProtectJSON(label, rowID string, v any) (json.RawMessage, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return plain, nil
	}
	sealed, err := k.Seal(label, rowID, string(plain))
	if err != nil {
		return nil, err
	}
	return json.Marshal(sealed)
}

// RevealJSON reads a JSONB value written by ProtectJSON into dst. A plain JSON value written before encryption was
// enabled is read as it is, so old rows keep working. An empty or null value leaves dst untouched.
func (k *Keyring) RevealJSON(label, rowID string, raw []byte, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && IsSealed(s) {
		plain, err := k.Reveal(label, rowID, s)
		if err != nil {
			return fmt.Errorf("open %s: %w", label, err)
		}
		raw = []byte(plain)
	}
	return json.Unmarshal(raw, dst)
}
