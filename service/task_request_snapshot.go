package service

import (
	"maps"
	"mime"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	maxTaskRequestPromptRunes = 20000
	maxTaskRequestMediaURLs   = 16
	maxTaskRequestURLBytes    = 2048
	maxTaskRequestBodyBytes   = 64 << 20
	maxTaskRequestWalkDepth   = 8
)

type taskRequestMediaKind int

const (
	taskRequestImage taskRequestMediaKind = iota
	taskRequestVideo
	taskRequestAudio
	taskRequestMediaKinds
)

// Field names are compared after lowercasing and dropping everything but
// letters and digits, so image_urls, imageUrls and image-urls all match. The
// sets follow the aliases the video channels accept.
var (
	taskRequestMediaNames = [taskRequestMediaKinds]map[string]struct{}{
		taskRequestImage: setOf("image", "images", "imageurl", "imageurls", "imageref", "imagerefs",
			"inputimage", "inputimages", "inputreference", "initimage", "initimages",
			"referenceimage", "referenceimages", "referenceimageurl", "referenceimageurls",
			"firstimage", "lastimage", "startimageurl", "endimageurl", "startframeurl", "endframeurl",
			"firstframe", "lastframe", "firstframeimage", "lastframeimage", "firstframeurl", "lastframeurl",
			"refimage", "refimages"),
		taskRequestVideo: setOf("video", "videos", "videourl", "videourls", "videoref", "videorefs",
			"referencevideo", "referencevideos", "referencevideourls", "refvideo", "refvideos"),
		taskRequestAudio: setOf("audio", "audios", "audiourl", "audiourls", "audioref", "audiorefs",
			"referenceaudio", "referenceaudios", "audioreference"),
	}
	taskRequestMediaPrefixes = [taskRequestMediaKinds][]string{
		taskRequestImage: {"imageurl", "imageref", "referenceimage", "refimage"},
		taskRequestVideo: {"videourl", "videoref", "referencevideo", "refvideo"},
		taskRequestAudio: {"audiourl", "audioref", "referenceaudio"},
	}
	taskRequestURLKeys = setOf("url", "uri", "href")
)

func setOf(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func normalizeTaskRequestField(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func taskRequestMediaKindOf(normalized string) (taskRequestMediaKind, bool) {
	for kind := range taskRequestMediaKinds {
		if _, ok := taskRequestMediaNames[kind][normalized]; ok {
			return kind, true
		}
		for _, prefix := range taskRequestMediaPrefixes[kind] {
			if strings.HasPrefix(normalized, prefix) {
				return kind, true
			}
		}
	}
	return 0, false
}

// TaskRequestSnapshotFromContext records the prompt and public reference media
// URLs of a task submit body. Inline data (base64, data: URLs, uploaded files)
// is skipped so the snapshot stays small and never copies user files.
func TaskRequestSnapshotFromContext(c *gin.Context) *model.TaskRequestSnapshot {
	if c == nil || c.Request == nil {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage.Size() == 0 || storage.Size() > maxTaskRequestBodyBytes {
		return nil
	}
	var body any
	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaType == "multipart/form-data" {
		form, parseErr := common.ParseMultipartFormReusable(c)
		if parseErr != nil {
			return nil
		}
		defer form.RemoveAll()
		fields := make(map[string]any, len(form.Value))
		for name, values := range form.Value {
			items := make([]any, len(values))
			for i, value := range values {
				items[i] = value
			}
			fields[name] = items
		}
		body = fields
	} else {
		raw, readErr := storage.Bytes()
		if readErr != nil || common.Unmarshal(raw, &body) != nil {
			return nil
		}
	}

	collector := newTaskRequestMediaCollector()
	collector.walk(body, 0)
	snapshot := &model.TaskRequestSnapshot{
		Prompt: taskRequestPrompt(body),
		Images: collector.urls[taskRequestImage],
		Videos: collector.urls[taskRequestVideo],
		Audios: collector.urls[taskRequestAudio],
	}
	// A protocol decoder (for example the Responses protocol) may assemble the
	// prompt from structured input; the decoded task body carries the result.
	if snapshot.Prompt == "" {
		if decoded, exists := c.Get("task_request"); exists {
			snapshot.Prompt = taskRequestPrompt(decoded)
		}
	}
	if snapshot.Prompt == "" && len(snapshot.Images) == 0 && len(snapshot.Videos) == 0 && len(snapshot.Audios) == 0 {
		return nil
	}
	return snapshot
}

func taskRequestPrompt(body any) string {
	fields, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	var prompt string
	switch value := fields["prompt"].(type) {
	case string:
		prompt = value
	case []any:
		if len(value) > 0 {
			prompt, _ = value[0].(string)
		}
	}
	prompt = strings.TrimSpace(prompt)
	if utf8.RuneCountInString(prompt) > maxTaskRequestPromptRunes {
		prompt = string([]rune(prompt)[:maxTaskRequestPromptRunes])
	}
	return prompt
}

type taskRequestMediaCollector struct {
	urls [taskRequestMediaKinds][]string
	seen [taskRequestMediaKinds]map[string]struct{}
}

func newTaskRequestMediaCollector() *taskRequestMediaCollector {
	collector := &taskRequestMediaCollector{}
	for kind := range taskRequestMediaKinds {
		collector.seen[kind] = make(map[string]struct{})
	}
	return collector
}

// walk visits the body looking for media-named fields; nested objects and
// arrays such as input[].content[] are searched as well.
func (m *taskRequestMediaCollector) walk(value any, depth int) {
	if depth > maxTaskRequestWalkDepth {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, name := range slices.Sorted(maps.Keys(typed)) {
			if kind, ok := taskRequestMediaKindOf(normalizeTaskRequestField(name)); ok {
				m.collect(kind, typed[name], depth+1)
				continue
			}
			m.walk(typed[name], depth+1)
		}
	case []any:
		for _, child := range typed {
			m.walk(child, depth+1)
		}
	}
}

// collect reads the URLs under a media field: a string, an array, or an
// OpenAI-style object holding the address below url / image_url.
func (m *taskRequestMediaCollector) collect(kind taskRequestMediaKind, value any, depth int) {
	if depth > maxTaskRequestWalkDepth {
		return
	}
	switch typed := value.(type) {
	case string:
		m.add(kind, typed)
	case []any:
		for _, child := range typed {
			m.collect(kind, child, depth+1)
		}
	case map[string]any:
		for _, name := range slices.Sorted(maps.Keys(typed)) {
			child := typed[name]
			normalized := normalizeTaskRequestField(name)
			if _, ok := taskRequestURLKeys[normalized]; ok {
				m.collect(kind, child, depth+1)
			} else if _, ok := taskRequestMediaKindOf(normalized); ok {
				m.collect(kind, child, depth+1)
			}
		}
	}
}

func (m *taskRequestMediaCollector) add(kind taskRequestMediaKind, value string) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > maxTaskRequestURLBytes || len(m.urls[kind]) >= maxTaskRequestMediaURLs {
		return
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return
	}
	if _, exists := m.seen[kind][value]; exists {
		return
	}
	m.seen[kind][value] = struct{}{}
	m.urls[kind] = append(m.urls[kind], value)
}
