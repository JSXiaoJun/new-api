package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// SimpleCaptchaCheck requires a valid image captcha answer when the simple
// captcha switch is on. The answer is read from the captcha_id and
// captcha_code query parameters and each captcha can be tried only once.
func SimpleCaptchaCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.SimpleCaptchaEnabled {
			c.Next()
			return
		}
		captchaID := c.Query("captcha_id")
		captchaCode := c.Query("captcha_code")
		if captchaID == "" || captchaCode == "" {
			common.ApiErrorT(c, "Please enter the captcha")
			c.Abort()
			return
		}
		if !service.VerifySimpleCaptcha(captchaID, captchaCode) {
			common.ApiErrorT(c, "Captcha is incorrect or expired, please try again")
			c.Abort()
			return
		}
		c.Next()
	}
}

// CaptchaRateLimit bounds how many captcha images one client IP can request.
// It uses its own counter so refreshing images does not use up the login
// and registration budget.
func CaptchaRateLimit() func(c *gin.Context) {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	return rateLimitFactory(60, 10*60, "CP")
}
