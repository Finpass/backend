package app

import (
	"fmt"
	"strings"

	"shenzen/backend/internal/platform/config"
	"shenzen/backend/internal/platform/crypto"
)

// newKeyring builds the column-encryption keyring; nil (encryption off) is only possible outside production.
func newKeyring(c config.Config) (*crypto.Keyring, error) {
	if !c.Data.Enabled() {
		return nil, nil
	}
	old := map[string]string{}
	for _, kv := range strings.FieldsFunc(c.Data.OldKEKs, func(r rune) bool { return r == ',' || r == ' ' }) {
		id, key, ok := strings.Cut(kv, ":")
		if !ok {
			return nil, fmt.Errorf("DATA_KEK_OLD entry %q must be id:base64key", kv)
		}
		old[id] = key
	}
	return crypto.NewKeyring(c.Data.KEKID, c.Data.KEK, c.Data.IndexKey, old)
}
