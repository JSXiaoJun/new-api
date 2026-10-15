package service

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskRequestSnapshotContext(t *testing.T, contentType string, body []byte) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	return c
}

func TestTaskRequestSnapshotFromContext(t *testing.T) {
	testCases := []struct {
		name string
		body string
		want *model.TaskRequestSnapshot
	}{
		{
			name: "json prompt with public reference media",
			body: `{"model":"wan","prompt":"  two people  ","image_urls":["https://cdn.example/1.png","https://cdn.example/1.png",{"url":"https://cdn.example/2.png"}],"reference_video":"https://cdn.example/r.mp4","audio_urls":["http://cdn.example/a.mp3"]}`,
			want: &model.TaskRequestSnapshot{
				Prompt: "two people",
				Images: []string{"https://cdn.example/1.png", "https://cdn.example/2.png"},
				Videos: []string{"https://cdn.example/r.mp4"},
				Audios: []string{"http://cdn.example/a.mp3"},
			},
		},
		{
			name: "inline and credentialed media are skipped",
			body: `{"prompt":"p","image":"data:image/png;base64,AAAA","images":["aGVsbG8=","https://user:pw@cdn.example/x.png"]}`,
			want: &model.TaskRequestSnapshot{Prompt: "p"},
		},
		{
			name: "aliases and nested responses input are found",
			body: `{"prompt":"p","imageUrls":["https://cdn.example/a.png"],"first_frame_url":"https://cdn.example/f.png","referenceVideos":["https://cdn.example/v.mp4"],"input":[{"role":"user","content":[{"type":"input_image","image_url":{"url":"https://cdn.example/n.png"}}]}]}`,
			want: &model.TaskRequestSnapshot{
				Prompt: "p",
				Images: []string{"https://cdn.example/f.png", "https://cdn.example/a.png", "https://cdn.example/n.png"},
				Videos: []string{"https://cdn.example/v.mp4"},
			},
		},
		{
			name: "body without prompt or media records nothing",
			body: `{"model":"wan","duration":5}`,
			want: nil,
		},
		{
			name: "invalid json records nothing",
			body: `{"prompt":`,
			want: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := taskRequestSnapshotContext(t, "application/json", []byte(testCase.body))
			assert.Equal(t, testCase.want, TaskRequestSnapshotFromContext(c))
		})
	}
}

func TestTaskRequestSnapshotFromMultipartKeepsTextFieldsOnly(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "a cat"))
	require.NoError(t, writer.WriteField("input_reference", "https://cdn.example/ref.png"))
	part, err := writer.CreateFormFile("input_reference", "upload.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("binary"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c := taskRequestSnapshotContext(t, writer.FormDataContentType(), body.Bytes())

	assert.Equal(t, &model.TaskRequestSnapshot{
		Prompt: "a cat",
		Images: []string{"https://cdn.example/ref.png"},
	}, TaskRequestSnapshotFromContext(c))
}

func TestTaskRequestSnapshotUsesDecodedPromptWhenBodyHasNone(t *testing.T) {
	c := taskRequestSnapshotContext(t, "application/json", []byte(`{"model":"m","input":"draw a cat"}`))
	c.Set("task_request", map[string]any{"model": "m", "prompt": "draw a cat"})

	assert.Equal(t, &model.TaskRequestSnapshot{Prompt: "draw a cat"}, TaskRequestSnapshotFromContext(c))
}

func TestTaskRequestSnapshotTruncatesLongPrompt(t *testing.T) {
	prompt := strings.Repeat("字", maxTaskRequestPromptRunes+10)
	c := taskRequestSnapshotContext(t, "application/json", []byte(`{"prompt":"`+prompt+`"}`))

	snapshot := TaskRequestSnapshotFromContext(c)

	require.NotNil(t, snapshot)
	assert.Equal(t, maxTaskRequestPromptRunes, len([]rune(snapshot.Prompt)))
}
