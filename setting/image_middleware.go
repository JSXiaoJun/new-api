package setting

import (
	"fmt"
	"net/url"
	"strings"
)

// ImageAssetPublicPath is the URL path an image middleware uses to expose a
// stored image publicly. Only links under this path can reach the drawing log.
const ImageAssetPublicPath = "/public/images/assets/"

// ImageMiddlewareAddress is the base address of the image middleware that
// issues the public image links shown in the drawing log, for example
// "https://video-admin.example.com" or "10.0.0.8:8795".
//
// The address is an allowlist: a link is recorded only when the middleware at
// this address serves it, so an upstream cannot plant its own URL in the log
// through a crafted "x-image-asset-links" header or an "url" field in the
// response body. An empty address keeps the permissive behaviour of accepting
// any well-formed public asset link.
var ImageMiddlewareAddress = ""

// ValidateImageMiddlewareAddress rejects an address that cannot be read as a
// host, so a typo is reported when it is saved instead of silently disabling
// the allowlist. A bare "host:port" is accepted, because that is what an
// operator usually has at hand.
func ValidateImageMiddlewareAddress(address string) error {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return nil
	}
	parsed, err := parseImageMiddlewareURL(trimmed)
	if err != nil {
		return err
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("image middleware address %q has no host", trimmed)
	}
	return nil
}

// IsImageMiddlewareLink reports whether link is a public image link served by
// the configured image middleware.
//
// The scheme is deliberately ignored: the gateway and the browser may reach the
// same middleware over different schemes behind a reverse proxy, while the host
// and the public path still identify it. A configured address without a port
// matches any port on that host, so pasting only the host name is enough when
// the middleware is not behind a reverse proxy.
func IsImageMiddlewareLink(link string) bool {
	trimmed := strings.TrimSpace(link)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" {
		return false
	}

	address := strings.TrimSpace(ImageMiddlewareAddress)
	if address == "" {
		// Not configured: only require the link to look like a public asset
		// link rather than an arbitrary upstream URL.
		return strings.Contains(parsed.Path, ImageAssetPublicPath)
	}

	base, err := parseImageMiddlewareURL(address)
	if err != nil || base.Hostname() == "" {
		return false
	}
	if !equalImageMiddlewareHost(base.Hostname(), parsed.Hostname()) {
		return false
	}
	if base.Port() != "" && base.Port() != effectiveURLPort(parsed) {
		return false
	}
	return strings.HasPrefix(parsed.Path, strings.TrimSuffix(base.Path, "/")+ImageAssetPublicPath)
}

// parseImageMiddlewareURL accepts a full URL as well as a bare host[:port].
func parseImageMiddlewareURL(address string) (*url.URL, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("invalid image middleware address: %w", err)
	}
	return parsed, nil
}

func equalImageMiddlewareHost(a string, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

func effectiveURLPort(parsed *url.URL) string {
	if port := parsed.Port(); port != "" {
		return port
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}
