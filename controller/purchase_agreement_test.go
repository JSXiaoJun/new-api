package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPurchaseAgreementControllerTest(t *testing.T) {
	t.Helper()
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, db.Create(&model.User{
		Id:       7,
		Username: "agreement_user",
		Status:   common.UserStatusEnabled,
	}).Error)
}

func postPurchaseAgreement(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 7)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/purchase-agreement", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ConfirmPurchaseAgreement(ctx)
	return recorder
}

func loadPurchaseAgreementAt(t *testing.T) int64 {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, 7).Error)
	return user.PurchaseAgreedAt
}

func TestConfirmPurchaseAgreementRejectsIncompleteRequests(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "citizen item unchecked", body: `{"confirm_not_mainland_citizen":false,"confirm_not_in_mainland":true,"statement":"我已知晓并确认"}`},
		{name: "location item unchecked", body: `{"confirm_not_mainland_citizen":true,"confirm_not_in_mainland":false,"statement":"我已知晓并确认"}`},
		{name: "blank statement", body: `{"confirm_not_mainland_citizen":true,"confirm_not_in_mainland":true,"statement":"   "}`},
		{name: "oversized statement", body: `{"confirm_not_mainland_citizen":true,"confirm_not_in_mainland":true,"statement":"` + strings.Repeat("x", purchaseAgreementStatementMaxLength+1) + `"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupPurchaseAgreementControllerTest(t)

			recorder := postPurchaseAgreement(t, tc.body)

			assert.Contains(t, recorder.Body.String(), `"success":false`)
			assert.Zero(t, loadPurchaseAgreementAt(t))
		})
	}
}

func TestConfirmPurchaseAgreementKeepsFirstSignatureTime(t *testing.T) {
	setupPurchaseAgreementControllerTest(t)
	const body = `{"confirm_not_mainland_citizen":true,"confirm_not_in_mainland":true,"statement":"我已知晓并确认"}`

	first := postPurchaseAgreement(t, body)
	require.Contains(t, first.Body.String(), `"success":true`)
	firstSignedAt := loadPurchaseAgreementAt(t)
	require.Positive(t, firstSignedAt)

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).
		Update("purchase_agreement_at", firstSignedAt-100).Error)
	second := postPurchaseAgreement(t, body)

	require.Contains(t, second.Body.String(), `"success":true`)
	assert.Equal(t, firstSignedAt-100, loadPurchaseAgreementAt(t))
}
