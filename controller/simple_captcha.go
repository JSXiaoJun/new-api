package controller

import (
	"encoding/base64"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetSimpleCaptcha issues a new single-use image captcha for sign-in and
// sign-up. The answer never leaves the server; only the id and image do.
func GetSimpleCaptcha(c *gin.Context) {
	if !common.SimpleCaptchaEnabled {
		common.ApiErrorT(c, "Captcha is not enabled")
		return
	}
	id, code, err := service.NewSimpleCaptcha()
	if err != nil {
		common.SysError(common.LogText("failed to issue simple captcha: %s", err.Error()))
		common.ApiErrorT(c, "Failed to generate captcha, please try again")
		return
	}
	image, err := service.RenderSimpleCaptchaPNG(code)
	if err != nil {
		common.SysError(common.LogText("failed to render simple captcha: %s", err.Error()))
		common.ApiErrorT(c, "Failed to generate captcha, please try again")
		return
	}
	common.ApiSuccess(c, gin.H{
		"captcha_id": id,
		"image":      "data:image/png;base64," + base64.StdEncoding.EncodeToString(image),
	})
}
