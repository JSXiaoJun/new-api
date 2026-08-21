package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureResponseBodyPreservesResponseAndMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	CaptureResponseBody(c)
	c.Header("Content-Type", "application/json")
	c.Status(201)
	_, err := c.Writer.WriteString(`{"ok":true}`)
	require.NoError(t, err)

	body, encoding, contentType, status, ok := GetCapturedResponseBody(c)
	require.True(t, ok)
	assert.Equal(t, `{"ok":true}`, body)
	assert.Equal(t, "utf-8", encoding)
	assert.Equal(t, "application/json", contentType)
	assert.Equal(t, 201, status)
	assert.Equal(t, `{"ok":true}`, recorder.Body.String())
}
