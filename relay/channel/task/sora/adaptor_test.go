package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
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
