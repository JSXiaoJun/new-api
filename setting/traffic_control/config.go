package traffic_control

import (
	"errors"
	"net/textproto"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
	"golang.org/x/net/http/httpguts"
)

const (
	defaultCountryHeader     = "CF-IPCountry"
	defaultWarningTitle      = "WEB ACCESS UNAVAILABLE"
	defaultWarningContent    = "Mainland China IP addresses are not permitted to access web content."
	defaultWarningAnnotation = "This site has suspended access from mainland China and is available only to overseas users."
)

type TrafficControlSetting struct {
	MainlandWebBlockEnabled bool   `json:"mainland_web_block_enabled"`
	IncludeHongKongTaiwan   bool   `json:"include_hong_kong_taiwan"`
	CountryHeader           string `json:"country_header"`
	WarningTitle            string `json:"warning_title"`
	WarningContent          string `json:"warning_content"`
	WarningAnnotation       string `json:"warning_annotation"`
}

var (
	trafficControlSetting = TrafficControlSetting{
		MainlandWebBlockEnabled: false,
		IncludeHongKongTaiwan:   false,
		CountryHeader:           defaultCountryHeader,
		WarningTitle:            defaultWarningTitle,
		WarningContent:          defaultWarningContent,
		WarningAnnotation:       defaultWarningAnnotation,
	}
	mainlandWebBlockEnabled atomic.Bool
	includeHongKongTaiwan   atomic.Bool
	countryHeader           atomic.Value
	warningTitle            atomic.Value
	warningContent          atomic.Value
	warningAnnotation       atomic.Value
)

func init() {
	config.GlobalConfig.Register("traffic_control", &trafficControlSetting)
	UpdateAndSync()
}

func ValidateCountryHeader(value string) error {
	header := strings.TrimSpace(value)
	if !httpguts.ValidHeaderFieldName(header) {
		return errors.New("country header must be a valid HTTP header name")
	}
	return nil
}

func UpdateAndSync() {
	header := strings.TrimSpace(trafficControlSetting.CountryHeader)
	if ValidateCountryHeader(header) != nil {
		header = defaultCountryHeader
	}
	mainlandWebBlockEnabled.Store(trafficControlSetting.MainlandWebBlockEnabled)
	includeHongKongTaiwan.Store(trafficControlSetting.IncludeHongKongTaiwan)
	countryHeader.Store(textproto.CanonicalMIMEHeaderKey(header))
	warningTitle.Store(trafficControlSetting.WarningTitle)
	warningContent.Store(trafficControlSetting.WarningContent)
	warningAnnotation.Store(trafficControlSetting.WarningAnnotation)
}

func MainlandWebBlockEnabled() bool {
	return mainlandWebBlockEnabled.Load()
}

func IncludeHongKongTaiwan() bool {
	return includeHongKongTaiwan.Load()
}

func CountryHeader() string {
	return countryHeader.Load().(string)
}

func WarningTitle() string {
	return warningTitle.Load().(string)
}

func WarningContent() string {
	return warningContent.Load().(string)
}

func WarningAnnotation() string {
	return warningAnnotation.Load().(string)
}
