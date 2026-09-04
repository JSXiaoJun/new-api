package common

import (
	"bytes"
	"encoding/base64"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const responseBodyCaptureKey = "response_body_capture"
const responseBodyCaptureLimit = 1024

type responseBodyCaptureWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *responseBodyCaptureWriter) record(data []byte, n int) {
	if n > len(data) {
		n = len(data)
	}
	if n <= 0 || w.body.Len() >= responseBodyCaptureLimit {
		return
	}
	remaining := responseBodyCaptureLimit - w.body.Len()
	if n > remaining {
		n = remaining
	}
	_, _ = w.body.Write(data[:n])
}

func (w *responseBodyCaptureWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.record(data, n)
	return n, err
}

func (w *responseBodyCaptureWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	w.record([]byte(data), n)
	return n, err
}

// CaptureResponseBody records the exact HTTP response bytes written through
// Gin so a completed usage log can retain them for root-only diagnostics.
func CaptureResponseBody(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	if _, exists := c.Get(responseBodyCaptureKey); exists {
		return
	}
	writer := &responseBodyCaptureWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	c.Set(responseBodyCaptureKey, writer)
}

// GetCapturedResponseBody returns a UTF-8 body verbatim. Binary responses are
// base64 encoded so they can be stored safely in the log JSON.
func GetCapturedResponseBody(c *gin.Context) (body string, encoding string, contentType string, status int, ok bool) {
	if c == nil {
		return "", "", "", 0, false
	}
	value, exists := c.Get(responseBodyCaptureKey)
	if !exists {
		return "", "", "", 0, false
	}
	writer, ok := value.(*responseBodyCaptureWriter)
	if !ok || writer.body.Len() == 0 {
		return "", "", "", 0, false
	}
	data := writer.body.Bytes()
	encoding = "utf-8"
	if utf8.Valid(data) {
		body = string(data)
	} else {
		body = base64.StdEncoding.EncodeToString(data)
		encoding = "base64"
	}
	return body, encoding, writer.Header().Get("Content-Type"), writer.Status(), true
}
