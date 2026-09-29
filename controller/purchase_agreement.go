package controller

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const purchaseAgreementStatementMaxLength = 64

type PurchaseAgreementRequest struct {
	ConfirmNotMainlandCitizen bool   `json:"confirm_not_mainland_citizen"`
	ConfirmNotInMainland      bool   `json:"confirm_not_in_mainland"`
	ConfirmNoInvoice          bool   `json:"confirm_no_invoice"`
	Statement                 string `json:"statement"`
}

// ConfirmPurchaseAgreement records the current user's one-time pre-purchase
// confirmation. The exact localized statement is validated by the dashboard;
// the backend requires every item to be checked and a non-empty statement.
func ConfirmPurchaseAgreement(c *gin.Context) {
	var req PurchaseAgreementRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	statement := strings.TrimSpace(req.Statement)
	if !req.ConfirmNotMainlandCitizen ||
		!req.ConfirmNotInMainland ||
		!req.ConfirmNoInvoice ||
		statement == "" ||
		utf8.RuneCountInString(statement) > purchaseAgreementStatementMaxLength {
		common.ApiErrorI18n(c, i18n.MsgPurchaseAgreementInvalid)
		return
	}

	userId := c.GetInt("id")
	signedAt, err := model.ConfirmUserPurchaseAgreement(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf(
		"purchase agreement confirmed user_id=%d ip=%s signed_at=%d statement=%q",
		userId,
		c.ClientIP(),
		signedAt,
		statement,
	))

	common.ApiSuccess(c, gin.H{
		"purchase_agreement_at": signedAt,
	})
}
