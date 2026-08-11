package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTaskProxyContentURL(t *testing.T) {
	testCases := []struct {
		name string
		url  string
		want bool
	}{
		{name: "legacy authenticated path", url: "https://api.example/v1/videos/task_public/content", want: true},
		{name: "public path", url: "https://api.example/public/videos/task_public/content", want: true},
		{name: "upstream content", url: "https://upstream.example/videos/task_public/content", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, isTaskProxyContentURL(testCase.url, "task_public"))
		})
	}
}

func TestWriteVideoDataURLRejectsNonVideoContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	err := writeVideoDataURL(c, "data:text/html;base64,PGgxPm5vdCBhIHZpZGVvPC9oMT4=")

	require.Error(t, err)
	assert.Empty(t, recorder.Body.String())
}
