package newznab_indexer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateTunnel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		wantErr string
	}{
		{"empty", "", ""},
		{"forced", TunnelForced, ""},
		{"none", TunnelNone, ""},
		{"http proxy", "http://proxy:8080", ""},
		{"https proxy", "https://proxy:443", ""},
		{"socks5 proxy", "socks5://proxy:1080", ""},
		{"socks5h proxy", "socks5h://proxy:1080", ""},
		{"unsupported scheme", "ftp://proxy:21", "unsupported scheme"},
		{"missing scheme", "//proxy:8080", "missing scheme"},
		{"missing host", "http://", "missing host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTunnel(tc.input)
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
