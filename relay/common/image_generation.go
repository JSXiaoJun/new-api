package common

import (
	"strings"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

// imageModelNameMarkers identifies upstream models that return generated images.
// Relay modes alone are not enough: Gemini and xAI relay image generation
// through their regular chat paths, so the model name is what distinguishes an
// image model from a text model on those channels.
var imageModelNameMarkers = []string{
	"imagen",
	"gpt-image",
	"dall-e",
	"nano-banana",
	"grok-imagine",
	"-image",
}

// IsImageGenerationModel reports whether an upstream or origin model name
// identifies an image generation model.
func IsImageGenerationModel(modelName string) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return false
	}
	for _, marker := range imageModelNameMarkers {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// IsImageGenerationRequest reports whether this request generates images, so
// callers can opt into image-specific behaviour (asset links, log markers)
// without inspecting the resolved channel or response body.
func IsImageGenerationRequest(info *RelayInfo) bool {
	if info == nil {
		return false
	}
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		return true
	case relayconstant.RelayModeGemini:
		return IsImageGenerationModel(info.UpstreamModelName) || IsImageGenerationModel(info.OriginModelName)
	default:
		return false
	}
}
