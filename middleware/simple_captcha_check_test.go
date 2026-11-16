package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type simpleCaptchaStore struct {
	name string
	// setup selects the store and returns a function that issues a captcha
	// which has already expired.
	setup func(t *testing.T) (issueExpired func() (string, string, error))
}

var simpleCaptchaStores = []simpleCaptchaStore{
	{
		name: "memory",
		setup: func(t *testing.T) func() (string, string, error) {
			previousRedisEnabled := common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
			return func() (string, string, error) {
				previousTTL := service.SimpleCaptchaTTL
				service.SimpleCaptchaTTL = -time.Second
				defer func() { service.SimpleCaptchaTTL = previousTTL }()
				return service.NewSimpleCaptcha()
			}
		},
	},
	{
		name: "redis",
		setup: func(t *testing.T) func() (string, string, error) {
			previousRedisEnabled := common.RedisEnabled
			previousRedisClient := common.RDB
			redisServer := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			common.RedisEnabled = true
			common.RDB = redisClient
			t.Cleanup(func() {
				common.RedisEnabled = previousRedisEnabled
				common.RDB = previousRedisClient
				_ = redisClient.Close()
			})
			return func() (string, string, error) {
				id, code, err := service.NewSimpleCaptcha()
				redisServer.FastForward(service.SimpleCaptchaTTL + time.Second)
				return id, code, err
			}
		},
	},
}

func runSimpleCaptchaGate(t *testing.T, captchaID string, captchaCode string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handlerReached := false
	router := gin.New()
	router.POST("/api/user/login", SimpleCaptchaCheck(), func(c *gin.Context) {
		handlerReached = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	query := url.Values{}
	if captchaID != "" {
		query.Set("captcha_id", captchaID)
	}
	if captchaCode != "" {
		query.Set("captcha_code", captchaCode)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/user/login?"+query.Encode(), nil)
	router.ServeHTTP(recorder, request)
	return recorder, handlerReached
}

func enableSimpleCaptcha(t *testing.T, enabled bool) {
	t.Helper()
	previous := common.SimpleCaptchaEnabled
	common.SimpleCaptchaEnabled = enabled
	t.Cleanup(func() { common.SimpleCaptchaEnabled = previous })
}

func TestSimpleCaptchaCheckPassesThroughWhenDisabled(t *testing.T) {
	enableSimpleCaptcha(t, false)

	_, reached := runSimpleCaptchaGate(t, "", "")

	assert.True(t, reached)
}

func TestSimpleCaptchaCheck(t *testing.T) {
	for _, store := range simpleCaptchaStores {
		t.Run(store.name, func(t *testing.T) {
			t.Run("missing answer is rejected", func(t *testing.T) {
				store.setup(t)
				enableSimpleCaptcha(t, true)
				id, _, err := service.NewSimpleCaptcha()
				require.NoError(t, err)

				recorder, reached := runSimpleCaptchaGate(t, id, "")

				assert.False(t, reached)
				assert.Contains(t, recorder.Body.String(), "Please enter the captcha")
			})

			t.Run("correct answer passes once, ignoring case and spaces", func(t *testing.T) {
				store.setup(t)
				enableSimpleCaptcha(t, true)
				id, code, err := service.NewSimpleCaptcha()
				require.NoError(t, err)

				_, reached := runSimpleCaptchaGate(t, id, " "+strings.ToLower(code)+" ")
				assert.True(t, reached)

				recorder, replayReached := runSimpleCaptchaGate(t, id, code)
				assert.False(t, replayReached)
				assert.Contains(t, recorder.Body.String(), "Captcha is incorrect or expired")
			})

			t.Run("wrong answer consumes the captcha", func(t *testing.T) {
				store.setup(t)
				enableSimpleCaptcha(t, true)
				id, code, err := service.NewSimpleCaptcha()
				require.NoError(t, err)
				wrong := "AAAA"
				if code == wrong {
					wrong = "CCCC"
				}

				_, wrongReached := runSimpleCaptchaGate(t, id, wrong)
				_, retryReached := runSimpleCaptchaGate(t, id, code)

				assert.False(t, wrongReached)
				assert.False(t, retryReached)
			})

			t.Run("unknown id is rejected", func(t *testing.T) {
				store.setup(t)
				enableSimpleCaptcha(t, true)

				_, reached := runSimpleCaptchaGate(t, common.GetUUID(), "ACDE")

				assert.False(t, reached)
			})

			t.Run("expired captcha is rejected", func(t *testing.T) {
				issueExpired := store.setup(t)
				enableSimpleCaptcha(t, true)
				id, code, err := issueExpired()
				require.NoError(t, err)

				_, reached := runSimpleCaptchaGate(t, id, code)

				assert.False(t, reached)
			})
		})
	}
}
