package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPurchaseAgreementMiddlewareTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "unsigned_user",
		AffCode:  "unsigned_aff",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id:               2,
		Username:         "signed_user",
		AffCode:          "signed_aff",
		Status:           common.UserStatusEnabled,
		PurchaseAgreedAt: 1_700_000_000,
	}).Error)
}

func runPurchaseAgreementGate(t *testing.T, userId int) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handlerReached := false
	router := gin.New()
	router.POST("/api/user/topup", func(c *gin.Context) {
		c.Set("id", userId)
		c.Next()
	}, PurchaseAgreementRequired(), func(c *gin.Context) {
		handlerReached = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/user/topup", nil))
	return recorder, handlerReached
}

func TestPurchaseAgreementRequiredBlocksUnsignedUser(t *testing.T) {
	setupPurchaseAgreementMiddlewareTest(t)

	recorder, handlerReached := runPurchaseAgreementGate(t, 1)

	assert.False(t, handlerReached)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
}

func TestPurchaseAgreementRequiredAllowsSignedUser(t *testing.T) {
	setupPurchaseAgreementMiddlewareTest(t)

	recorder, handlerReached := runPurchaseAgreementGate(t, 2)

	assert.True(t, handlerReached)
	assert.JSONEq(t, `{"success":true}`, recorder.Body.String())
}

func TestPurchaseAgreementRequiredStillAllowsSignedUserAfterProfileUpdate(t *testing.T) {
	setupPurchaseAgreementMiddlewareTest(t)

	// Admin/self profile updates bind client JSON into User; the signature must
	// survive because it is never bound from JSON.
	edited := model.User{Id: 2, DisplayName: "renamed"}
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return edited.UpdateWithTx(tx, false)
	}))

	_, handlerReached := runPurchaseAgreementGate(t, 2)

	assert.True(t, handlerReached)
}
