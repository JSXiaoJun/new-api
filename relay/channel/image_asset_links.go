package channel

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Image asset link relay contract between New API and an image middleware.
//
// The middleware stores every generated image and exposes it through an opaque
// public link. It only advertises those links when the caller opts in, so the
// default relay behaviour (upstream bodies relayed byte-for-byte) is unchanged:
//
//	request  X-Image-Asset-Links: 1
//	response x-image-asset-links: ["https://.../public/images/assets/..."]
//
// The response header is always a JSON array of strings and never carries image
// payloads, so a relay can persist a viewable link in its own log without
// keeping base64 blobs around.
const (
	ImageAssetLinksRequestHeader  = "X-Image-Asset-Links"
	ImageAssetLinksResponseHeader = "x-image-asset-links"
	// A response header is small; no real request needs more links than this.
	imageAssetLinksMaxCount = 32
)

// applyImageAssetLinksRequestHeader opts the upstream into reporting asset links.
func applyImageAssetLinksRequestHeader(info *relaycommon.RelayInfo, req *http.Header) {
	if req == nil || !relaycommon.IsImageGenerationRequest(info) {
		return
	}
	req.Set(ImageAssetLinksRequestHeader, "1")
}

// captureImageAssetLinksFromHeader stores the desensitized links reported by the
// upstream middleware on the request context, where the billing path can attach
// them to the consume log.
//
// The header covers images the upstream returned inline as base64, because the
// middleware never rewrites those payloads and cannot advertise a link in-band.
func captureImageAssetLinksFromHeader(c *gin.Context, resp *http.Response) {
	if c == nil || resp == nil {
		return
	}
	raw := strings.TrimSpace(resp.Header.Get(ImageAssetLinksResponseHeader))
	if raw == "" {
		return
	}
	var links []string
	if err := common.Unmarshal([]byte(raw), &links); err != nil {
		return
	}
	RecordImageAssetLinks(c, links)
}

// imageAssetLinkPath marks a public link served by an image middleware or by
// this gateway's own public media proxy. Only these links are recorded: an
// upstream URL that was never desensitized stays out of the log.
const imageAssetLinkPath = "/public/images/assets/"

// imageAssetLinksFromJSON collects public asset links from an OpenAI-shaped
// images payload, either a whole response body or a single stream event.
//
// Only "url" fields are inspected. Base64 payloads are left untouched and are
// never copied into the log.
func ImageAssetLinksFromJSON(data []byte) []string {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return nil
	}
	items := gjson.GetBytes(data, "data")
	if !items.IsArray() {
		return nil
	}
	links := make([]string, 0, len(items.Array()))
	for _, item := range items.Array() {
		link := strings.TrimSpace(item.Get("url").String())
		if link == "" || !strings.Contains(link, imageAssetLinkPath) {
			continue
		}
		links = append(links, link)
	}
	return links
}

// recordImageAssetLinks merges links into the request context, deduplicating by
// URL so the same image reported by both the body and the header is logged once.
func RecordImageAssetLinks(c *gin.Context, links []string) {
	if c == nil || len(links) == 0 {
		return
	}
	existing := common.GetContextKeyStringSlice(c, constant.ContextKeyImageAssetLinks)
	seen := make(map[string]struct{}, len(existing)+len(links))
	merged := make([]string, 0, len(existing)+len(links))
	for _, link := range append(append([]string{}, existing...), links...) {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		merged = append(merged, link)
		if len(merged) >= imageAssetLinksMaxCount {
			break
		}
	}
	if len(merged) == 0 {
		return
	}
	common.SetContextKey(c, constant.ContextKeyImageAssetLinks, merged)
}
