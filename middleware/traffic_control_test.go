package middleware

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/traffic_control"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var paymentCallbackRoutes = []struct {
	method string
	path   string
}{
	{method: http.MethodPost, path: "/api/stripe/webhook"},
	{method: http.MethodPost, path: "/api/creem/webhook"},
	{method: http.MethodPost, path: "/api/waffo/webhook"},
	{method: http.MethodPost, path: "/api/waffo-pancake/webhook/production"},
	{method: http.MethodPost, path: "/api/user/epay/notify"},
	{method: http.MethodGet, path: "/api/user/epay/notify"},
	{method: http.MethodPost, path: "/api/subscription/epay/notify"},
	{method: http.MethodGet, path: "/api/subscription/epay/notify"},
	{method: http.MethodPost, path: "/api/subscription/epay/return"},
	{method: http.MethodGet, path: "/api/subscription/epay/return"},
}

func configureTrafficControl(t *testing.T, enabled bool, countryHeader string) {
	configureTrafficControlWithRegions(t, enabled, false, countryHeader)
}

func configureTrafficControlWithRegions(t *testing.T, enabled bool, includeHongKongTaiwan bool, countryHeader string) {
	t.Helper()
	setting := config.GlobalConfig.Get("traffic_control")
	require.NotNil(t, setting)
	require.NoError(t, config.UpdateConfigFromMap(setting, map[string]string{
		"mainland_web_block_enabled": strconv.FormatBool(enabled),
		"include_hong_kong_taiwan":   strconv.FormatBool(includeHongKongTaiwan),
		"country_header":             countryHeader,
		"warning_title":              "WEB ACCESS UNAVAILABLE",
		"warning_content":            "Mainland China IP addresses are not permitted to access web content.",
		"warning_annotation":         "This site has suspended access from mainland China and is available only to overseas users.",
	}))
	traffic_control.UpdateAndSync()
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(setting, map[string]string{
			"mainland_web_block_enabled": "false",
			"include_hong_kong_taiwan":   "false",
			"country_header":             "CF-IPCountry",
			"warning_title":              "WEB ACCESS UNAVAILABLE",
			"warning_content":            "Mainland China IP addresses are not permitted to access web content.",
			"warning_annotation":         "This site has suspended access from mainland China and is available only to overseas users.",
		}))
		traffic_control.UpdateAndSync()
	})
}

func TestTrafficControlCanAlsoBlockHongKongAndTaiwan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControlWithRegions(t, true, true, "CF-IPCountry")
	router := newTrafficControlRouter()

	for _, countryCode := range []string{"CN", "HK", "TW"} {
		response := performTrafficControlRequest(router, "/", "CF-IPCountry", countryCode)
		assert.Equal(t, http.StatusFound, response.Code, countryCode)
		assert.Equal(t, mainlandWebDeniedPath, response.Header().Get("Location"), countryCode)
	}

	allowedResponse := performTrafficControlRequest(router, "/", "CF-IPCountry", "US")
	assert.Equal(t, http.StatusOK, allowedResponse.Code)
	assert.Equal(t, "dashboard", allowedResponse.Body.String())
}

func performTrafficControlRequest(router http.Handler, path string, headerName string, countryCode string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if countryCode != "" {
		request.Header.Set(headerName, countryCode)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func newTrafficControlRouter() *gin.Engine {
	router := gin.New()
	router.GET("/api/probe", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	for _, callback := range paymentCallbackRoutes {
		router.Handle(callback.method, callback.path, func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
	}
	router.Use(TrafficControl())
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "dashboard")
	})
	router.GET(mainlandWebDeniedPath, func(c *gin.Context) {
		c.String(http.StatusOK, "dashboard")
	})
	return router
}

func TestTrafficControlBlocksOnlyMainlandChinaWebRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()

	mainlandResponse := performTrafficControlRequest(router, "/", "CF-IPCountry", "cn")
	assert.Equal(t, http.StatusFound, mainlandResponse.Code)
	assert.Equal(t, mainlandWebDeniedPath, mainlandResponse.Header().Get("Location"))

	for _, countryCode := range []string{"HK", "TW", "US", ""} {
		response := performTrafficControlRequest(router, "/", "CF-IPCountry", countryCode)
		assert.Equal(t, http.StatusOK, response.Code, countryCode)
		assert.Equal(t, "dashboard", response.Body.String(), countryCode)
	}
}

func TestTrafficControlServesEnglishWarningPageWithRequiredNotice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()

	response := performTrafficControlRequest(router, mainlandWebDeniedPath, "CF-IPCountry", "CN")

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	body := html.UnescapeString(response.Body.String())
	assert.Contains(t, body, "WEB ACCESS UNAVAILABLE")
	assert.Contains(t, body, "Mainland China IP addresses are not permitted to access web content.")
	assert.Contains(t, body, "This site has suspended access from mainland China and is available only to overseas users.")
	assert.NotContains(t, body, "API access remains available.")
	assert.Contains(t, body, "http://example.com/v1")
	assert.Contains(t, body, "window.setInterval(checkAccess, 5000)")
}

func TestTrafficControlWarningPageUsesEscapedConfiguredCopy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	setting := config.GlobalConfig.Get("traffic_control")
	require.NotNil(t, setting)
	require.NoError(t, config.UpdateConfigFromMap(setting, map[string]string{
		"warning_title":      "Restricted <script>alert(1)</script>",
		"warning_content":    "Custom & configurable content",
		"warning_annotation": `<img src=x onerror="alert(1)">`,
	}))
	traffic_control.UpdateAndSync()
	router := newTrafficControlRouter()

	response := performTrafficControlRequest(router, mainlandWebDeniedPath, "CF-IPCountry", "CN")
	body := response.Body.String()

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, body, "Restricted &lt;script&gt;alert(1)&lt;/script&gt;")
	assert.Contains(t, body, "Custom &amp; configurable content")
	assert.Contains(t, body, "&lt;img src=x onerror=&#34;alert(1)&#34;&gt;")
	assert.NotContains(t, body, "<script>alert(1)</script>")
	assert.NotContains(t, body, "<img src=x")
}

func TestTrafficControlDoesNotAffectRegisteredAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()

	response := performTrafficControlRequest(router, "/api/probe", "CF-IPCountry", "CN")

	assert.Equal(t, http.StatusNoContent, response.Code)
}

func TestTrafficControlDoesNotAffectPaymentCallbackRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()
	for _, testCase := range paymentCallbackRoutes {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			request.Header.Set("CF-IPCountry", "CN")

			router.ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusNoContent, recorder.Code)
			assert.Empty(t, recorder.Header().Get("Location"))
		})
	}
}

func TestTrafficControlLeavesUnknownAPIPathsUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()
	paths := []string{
		"/api/future-payment-callback",
		"/v1/future-endpoint",
		"/v1beta/future-endpoint",
		"/mj/future-endpoint",
		"/suno/future-endpoint",
		"/kling/future-endpoint",
		"/jimeng/future-endpoint",
		"/pg/future-endpoint",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			response := performTrafficControlRequest(router, path, "CF-IPCountry", "CN")

			assert.Equal(t, http.StatusNotFound, response.Code)
			assert.Empty(t, response.Header().Get("Location"))
		})
	}
}

func TestTrafficControlUsesConfiguredCountryHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "X-Visitor-Country")
	router := newTrafficControlRouter()

	ignoredDefaultHeader := performTrafficControlRequest(router, "/", "CF-IPCountry", "CN")
	assert.Equal(t, http.StatusOK, ignoredDefaultHeader.Code)

	configuredHeader := performTrafficControlRequest(router, "/", "X-Visitor-Country", "CN")
	assert.Equal(t, http.StatusFound, configuredHeader.Code)
}

func TestTrafficControlDisabledAllowsMainlandWebRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, false, "CF-IPCountry")
	router := newTrafficControlRouter()

	response := performTrafficControlRequest(router, "/", "CF-IPCountry", "CN")

	assert.Equal(t, http.StatusOK, response.Code)
}

func TestTrafficControlWarningPageCanBePreviewedDirectly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, false, "CF-IPCountry")
	router := newTrafficControlRouter()

	response := performTrafficControlRequest(router, mainlandWebDeniedPath+"?preview=1", "CF-IPCountry", "")

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "Mainland China IP addresses")
}

func TestTrafficControlWarningPageRedirectsHomeWhenAccessIsRestored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()

	response := performTrafficControlRequest(router, mainlandWebDeniedPath, "CF-IPCountry", "US")

	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, "/", response.Header().Get("Location"))
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
}

func TestTrafficControlAccessCheckReflectsCurrentCountry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()

	blocked := performTrafficControlRequest(router, mainlandWebDeniedPath+"?check=1", "CF-IPCountry", "CN")
	assert.Equal(t, http.StatusForbidden, blocked.Code)
	assert.Equal(t, "blocked", blocked.Header().Get(trafficControlHeader))

	allowed := performTrafficControlRequest(router, mainlandWebDeniedPath+"?check=1", "CF-IPCountry", "HK")
	assert.Equal(t, http.StatusNoContent, allowed.Code)
	assert.Empty(t, allowed.Header().Get(trafficControlHeader))
}

func TestTrafficControlImageCheckSupportsCrossOriginCountryDetection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()

	blocked := performTrafficControlRequest(router, mainlandWebDeniedPath+"?check=image", "CF-IPCountry", "CN")
	assert.Equal(t, http.StatusOK, blocked.Code)
	assert.Equal(t, "image/gif", blocked.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", blocked.Header().Get("Cache-Control"))
	assert.Equal(t, trafficAccessBlockedGIF, blocked.Body.Bytes())

	allowed := performTrafficControlRequest(router, mainlandWebDeniedPath+"?check=image", "CF-IPCountry", "US")
	assert.Equal(t, http.StatusNoContent, allowed.Code)
	assert.Empty(t, allowed.Body.Bytes())
}

func TestTrafficControlUsesForwardedHTTPSForAPIEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrafficControl(t, true, "CF-IPCountry")
	router := newTrafficControlRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, mainlandWebDeniedPath, nil)
	request.Host = "api.example.com"
	request.Header.Set("CF-IPCountry", "CN")
	request.Header.Set("X-Forwarded-Proto", "https")

	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "https://api.example.com/v1")
}
