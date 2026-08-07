package middleware

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/traffic_control"
	"github.com/gin-gonic/gin"
)

const TrafficControlPath = "/web-access-denied"
const trafficControlHeader = "X-Traffic-Control"
const trafficControlBlocked = "blocked"
const trafficControlAllowed = "allowed"
const trafficControlUnavailable = "unavailable"

var trafficAccessBlockedGIF = []byte{
	'G', 'I', 'F', '8', '9', 'a', 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01,
	0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00,
	0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

var apiPathPrefixes = []string{
	"/api",
	"/v1",
	"/v1beta",
	"/mj",
	"/suno",
	"/kling",
	"/jimeng",
	"/pg",
}

var mainlandWebDeniedTemplate = template.Must(template.New("mainland-web-denied").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex,nofollow">
  <title>403 Forbidden</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-width: 320px; min-height: 100vh; overflow: hidden; color: #edf6ff; background-color: #050914; }
    body::before { content: ""; position: fixed; inset: 0 58% 0 0; border-right: 1px solid rgba(75, 185, 232, .13); background: #071631; pointer-events: none; }
    body::after { content: ""; position: fixed; inset: 0 0 0 42%; border-left: 1px solid rgba(221, 68, 104, .12); background: #170b18; clip-path: polygon(22% 0, 100% 0, 100% 100%, 0 100%); pointer-events: none; }
    main { position: relative; z-index: 1; display: grid; min-height: 100vh; place-items: center; padding: 24px; }
    main::before { content: ""; position: fixed; width: min(86vw, 1120px); height: min(72vh, 680px); border: 1px solid rgba(70, 132, 177, .1); transform: rotate(-4deg); pointer-events: none; }
    main::after { content: ""; position: fixed; width: min(70vw, 900px); height: min(82vh, 760px); border: 1px solid rgba(174, 62, 91, .09); transform: rotate(5deg); pointer-events: none; }
    article { position: relative; z-index: 1; width: min(100%, 760px); overflow: hidden; padding: clamp(30px, 5vw, 52px); border: 1px solid #2b506d; border-radius: 8px; background: rgba(5, 16, 34, .96); box-shadow: 0 32px 100px rgba(0, 0, 0, .5), 0 0 0 1px rgba(90, 199, 238, .04) inset; }
    article > * { position: relative; z-index: 1; }
    article .rail { position: absolute; z-index: 0; inset: 0 auto 0 0; width: 4px; background: #35b9e8; }
    article .rail::after { content: ""; position: absolute; top: 45%; left: 0; width: 4px; height: 55%; background: #d7476a; }
    article::before, article::after { content: ""; position: absolute; width: 24px; height: 24px; border-color: #48b9e8; }
    article::before { top: 12px; left: 12px; border-top: 2px solid; border-left: 2px solid; }
    article::after { right: 12px; bottom: 12px; border-right: 2px solid; border-bottom: 2px solid; }
    .status { display: inline-flex; min-height: 30px; align-items: center; padding: 0 12px; border: 1px solid #28789b; border-radius: 999px; color: #78d4fa; background: #0a2538; font: 700 12px/1 ui-monospace, SFMono-Regular, Consolas, monospace; letter-spacing: .08em; }
    h1 { margin: 20px 0 10px; overflow-wrap: anywhere; white-space: pre-line; font-size: clamp(30px, 6vw, 48px); line-height: 1.08; letter-spacing: 0; }
    .notice { margin: 0; overflow-wrap: anywhere; white-space: pre-line; color: #d4e0ed; font-size: clamp(16px, 3vw, 20px); line-height: 1.6; }
    .secondary { margin: 10px 0 0; max-width: 620px; overflow-wrap: anywhere; white-space: pre-line; color: #8fa6bd; font-size: 15px; line-height: 1.65; }
    footer { margin-top: 22px; padding-top: 18px; border-top: 1px solid #20344b; color: #91a4bb; font: 14px/1.5 ui-monospace, SFMono-Regular, Consolas, monospace; }
    code { color: #6ee7d2; }
  </style>
</head>
<body>
  <main>
    <article aria-labelledby="title">
      <span class="rail" aria-hidden="true"></span>
      <span class="status">403 FORBIDDEN</span>
      <h1 id="title">{{.Title}}</h1>
      <p class="notice">{{.Content}}</p>
      <p class="secondary">{{.Annotation}}</p>
      <footer>API Endpoint: <code>{{.APIEndpoint}}</code></footer>
    </article>
  </main>
  <script>
    if (!new URLSearchParams(window.location.search).has("preview")) {
      const checkAccess = async () => {
        try {
          const response = await fetch("/web-access-denied?check=1", { cache: "no-store" });
          if (response.status === 204) window.location.replace("/");
        } catch {}
      };
      window.setInterval(checkAccess, 15000);
    }
  </script>
</body>
</html>`))

func evaluateMainlandWebRequest(c *gin.Context) (bool, string) {
	if !traffic_control.MainlandWebBlockEnabled() {
		return false, trafficControlAllowed
	}

	countryCode := strings.ToUpper(strings.TrimSpace(c.GetHeader(traffic_control.CountryHeader())))
	if len(countryCode) != 2 || countryCode == "XX" || countryCode[0] < 'A' || countryCode[0] > 'Z' || countryCode[1] < 'A' || countryCode[1] > 'Z' {
		return false, trafficControlUnavailable
	}
	if countryCode == "CN" {
		return true, trafficControlBlocked
	}
	if traffic_control.IncludeHongKongTaiwan() && (countryCode == "HK" || countryCode == "TW") {
		return true, trafficControlBlocked
	}
	return false, trafficControlAllowed
}

func primaryWebOrigin(c *gin.Context) (string, bool) {
	serverAddress, err := url.Parse(strings.TrimSpace(system_setting.ServerAddress))
	if err != nil || serverAddress.Scheme == "" || serverAddress.Host == "" {
		return "", false
	}
	if strings.EqualFold(serverAddress.Hostname(), "localhost") || strings.EqualFold(serverAddress.Host, c.Request.Host) {
		return "", false
	}
	return serverAddress.Scheme + "://" + serverAddress.Host, true
}

func isAPIPath(path string) bool {
	for _, prefix := range apiPathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isWebDocumentPath(requestPath string) bool {
	extension := path.Ext(requestPath)
	return extension == "" || extension == ".html"
}

func serveMainlandWebDeniedPage(c *gin.Context) {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	} else {
		forwardedProto := strings.ToLower(strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0]))
		if forwardedProto == "https" {
			scheme = "https"
		}
	}
	var page bytes.Buffer
	err := mainlandWebDeniedTemplate.Execute(&page, struct {
		Title       string
		Content     string
		Annotation  string
		APIEndpoint string
	}{
		Title:       traffic_control.WarningTitle(),
		Content:     traffic_control.WarningContent(),
		Annotation:  traffic_control.WarningAnnotation(),
		APIEndpoint: scheme + "://" + c.Request.Host + "/v1",
	})
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusForbidden, "text/html; charset=utf-8", page.Bytes())
	c.Abort()
}

func RestrictMainlandWebAccess(c *gin.Context) bool {
	blocked, decision := evaluateMainlandWebRequest(c)
	primaryOrigin, redirectToPrimary := "", false
	if traffic_control.MainlandWebBlockEnabled() {
		primaryOrigin, redirectToPrimary = primaryWebOrigin(c)
	}
	if redirectToPrimary {
		blocked = false
		decision = trafficControlUnavailable
	}
	if c.Request.URL.Path == TrafficControlPath {
		if c.Query("check") == "image" {
			c.Header("Cache-Control", "no-store")
			if blocked {
				c.Data(http.StatusOK, "image/gif", trafficAccessBlockedGIF)
			} else {
				c.Status(http.StatusNoContent)
			}
			c.Abort()
			return true
		}
		if c.Query("check") == "1" {
			c.Header("Cache-Control", "no-store")
			c.Header(trafficControlHeader, decision)
			if blocked {
				c.Status(http.StatusForbidden)
			} else {
				c.Status(http.StatusNoContent)
			}
			c.Abort()
			return true
		}
		if c.Query("preview") == "1" || blocked {
			serveMainlandWebDeniedPage(c)
			return true
		}
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusFound, "/")
		c.Abort()
		return true
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	if isAPIPath(c.Request.URL.Path) {
		return false
	}
	documentRequest := isWebDocumentPath(c.Request.URL.Path)
	if redirectToPrimary && documentRequest {
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusFound, primaryOrigin+c.Request.URL.RequestURI())
		c.Abort()
		return true
	}
	if traffic_control.MainlandWebBlockEnabled() && documentRequest {
		c.Header("Cache-Control", "no-store")
	}

	if !blocked {
		return false
	}

	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, TrafficControlPath)
	c.Abort()
	return true
}

func TrafficControlEndpoint() gin.HandlerFunc {
	return func(c *gin.Context) {
		RestrictMainlandWebAccess(c)
	}
}

func TrafficControl() gin.HandlerFunc {
	return func(c *gin.Context) {
		if RestrictMainlandWebAccess(c) {
			return
		}
		c.Next()
	}
}
