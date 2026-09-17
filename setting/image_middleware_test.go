package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withImageMiddlewareAddress(t *testing.T, address string) {
	t.Helper()
	previous := ImageMiddlewareAddress
	ImageMiddlewareAddress = address
	t.Cleanup(func() { ImageMiddlewareAddress = previous })
}

// TestValidateImageMiddlewareAddress protects what an operator can save from
// the settings form: a bare "host:port" has to be accepted as typed, while a
// value with no host is rejected instead of being stored as an allowlist that
// can never match a link.
func TestValidateImageMiddlewareAddress(t *testing.T) {
	cases := []struct {
		name    string
		address string
		wantErr bool
	}{
		{name: "empty disables the allowlist", address: "   "},
		{name: "bare host", address: " video-admin.example "},
		{name: "host and port", address: "10.0.0.8:8795"},
		{name: "full url", address: "https://video-admin.example/"},
		{name: "url with base path", address: "https://video-admin.example/mw"},
		{name: "ipv6 host and port", address: "[::1]:8795"},
		{name: "missing host", address: "https://", wantErr: true},
		{name: "malformed ipv6 host", address: "http://[::1", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateImageMiddlewareAddress(tc.address)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestIsImageMiddlewareLinkEnforcesConfiguredAddress protects the drawing log
// from an upstream that answers with a link of its own: once a middleware
// address is configured, only links served by that middleware are accepted.
func TestIsImageMiddlewareLinkEnforcesConfiguredAddress(t *testing.T) {
	cases := []struct {
		name    string
		address string
		link    string
		want    bool
	}{
		{name: "unconfigured accepts asset link", link: "https://any.example/public/images/assets/a1", want: true},
		{name: "unconfigured rejects other path", link: "https://any.example/tracking.gif", want: false},
		{name: "unconfigured rejects non http scheme", link: "javascript:alert(1)", want: false},
		{name: "empty link", address: "https://video-admin.example", link: "", want: false},

		{name: "same origin", address: "https://video-admin.example", link: "https://video-admin.example/public/images/assets/a1", want: true},
		{name: "query string allowed", address: "https://video-admin.example", link: "https://video-admin.example/public/images/assets/a1?exp=1", want: true},
		{name: "host is case insensitive", address: "https://video-admin.example", link: "https://VIDEO-ADMIN.example/public/images/assets/a1", want: true},
		{name: "other host", address: "https://video-admin.example", link: "https://evil.example/public/images/assets/a1", want: false},
		{name: "host suffix attack", address: "https://video-admin.example", link: "https://video-admin.example.evil.com/public/images/assets/a1", want: false},
		{name: "other path", address: "https://video-admin.example", link: "https://video-admin.example/other/a1", want: false},
		{name: "scheme is ignored", address: "https://video-admin.example", link: "http://video-admin.example/public/images/assets/a1", want: true},
		{name: "bare host matches any scheme", address: "video-admin.example", link: "https://video-admin.example/public/images/assets/a1", want: true},

		{name: "port matches", address: "10.0.0.8:8795", link: "http://10.0.0.8:8795/public/images/assets/a1", want: true},
		{name: "port differs", address: "10.0.0.8:8795", link: "http://10.0.0.8:9999/public/images/assets/a1", want: false},
		{name: "port omitted in link", address: "10.0.0.8:8795", link: "http://10.0.0.8/public/images/assets/a1", want: false},
		{name: "default port matches explicit", address: "video-admin.example:443", link: "https://video-admin.example/public/images/assets/a1", want: true},
		{name: "host without port accepts any port", address: "10.0.0.8", link: "http://10.0.0.8:8795/public/images/assets/a1", want: true},

		{name: "trailing slash on base", address: "http://127.0.0.1:8795/", link: "http://127.0.0.1:8795/public/images/assets/a1", want: true},
		{name: "base path prefix", address: "https://video-admin.example/mw", link: "https://video-admin.example/mw/public/images/assets/a1", want: true},
		{name: "base path missing", address: "https://video-admin.example/mw", link: "https://video-admin.example/public/images/assets/a1", want: false},
		{name: "unparsable address", address: "https://", link: "https://video-admin.example/public/images/assets/a1", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withImageMiddlewareAddress(t, tc.address)
			assert.Equal(t, tc.want, IsImageMiddlewareLink(tc.link))
		})
	}
}
