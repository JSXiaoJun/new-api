package traffic_control

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCountryHeader(t *testing.T) {
	testCases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "Cloudflare header", value: "CF-IPCountry"},
		{name: "custom header", value: "X-Visitor-Country"},
		{name: "surrounding whitespace", value: "  X-Country  "},
		{name: "empty", value: "", wantErr: true},
		{name: "spaces", value: "X Country", wantErr: true},
		{name: "newline", value: "X-Country\nInjected", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateCountryHeader(testCase.value)
			if testCase.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestWarningPageDefaults(t *testing.T) {
	assert.Equal(t, "WEB ACCESS UNAVAILABLE", WarningTitle())
	assert.Equal(t, "Mainland China IP addresses are not permitted to access web content.", WarningContent())
	assert.Equal(t, "This site has suspended access from mainland China and is available only to overseas users.", WarningAnnotation())
}
