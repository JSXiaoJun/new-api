package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// PurchaseAgreementRequired blocks top-up, redemption and subscription purchase
// requests until the authenticated user has signed the pre-purchase
// confirmation. It must run after UserAuth so the user id is available.
func PurchaseAgreementRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		signed, err := model.HasUserSignedPurchaseAgreement(c.GetInt("id"))
		if err != nil {
			common.ApiError(c, err)
			c.Abort()
			return
		}
		if !signed {
			common.ApiErrorI18n(c, i18n.MsgPurchaseAgreementRequired)
			c.Abort()
			return
		}
		c.Next()
	}
}
