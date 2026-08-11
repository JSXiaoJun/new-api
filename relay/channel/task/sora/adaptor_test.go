package sora

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoUsesPublicContentURL(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://video.example/"
	t.Cleanup(func() { system_setting.ServerAddress = originalServerAddress })

	task := &model.Task{
		TaskID: "task_public_video",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"id":"upstream-task","video_url":"https://upstream.example/video.mp4"}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	require.Equal(t, "task_public_video", response["id"])
	require.Equal(t, "https://video.example/public/videos/task_public_video/content", response["video_url"])
}

func TestBuildRequestHeaderForwardsPublicTaskID(t *testing.T) {
	adaptor := &TaskAdaptor{apiKey: "adapter-key"}
	req := httptest.NewRequest(http.MethodPost, "https://media.example/v1/videos", nil)
	req.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req
	outbound := httptest.NewRequest(http.MethodPost, "https://media.example/v1/videos", nil)

	err := adaptor.BuildRequestHeader(ctx, outbound, &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_video"},
	})
	require.NoError(t, err)
	require.Equal(t, "Bearer adapter-key", outbound.Header.Get("Authorization"))
	require.Equal(t, "task_public_video", outbound.Header.Get("X-Public-Task-ID"))
}

func TestParseAndConvertUseTrustedMiddlewarePublicURL(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://media.yyapi.cloud"}
	publicURL := "https://media.yyapi.cloud/public/videos/task_public_video/content"

	result, err := adaptor.ParseTaskResult([]byte(`{"status":"completed","video_url":"` + publicURL + `"}`))
	require.NoError(t, err)
	require.Equal(t, publicURL, result.Url)

	task := &model.Task{
		TaskID: "task_public_video",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"status":"completed"}`),
		PrivateData: model.TaskPrivateData{
			ResultURL: publicURL,
		},
	}
	data, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	require.Equal(t, publicURL, response["video_url"])
}

func TestParseTaskResultRejectsUntrustedPublicURL(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://media.yyapi.cloud"}
	result, err := adaptor.ParseTaskResult(
		[]byte(`{"status":"completed","video_url":"https://attacker.example/public/videos/task_public_video/content"}`),
	)
	require.NoError(t, err)
	require.Empty(t, result.Url)
}
