package service

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/tiktoken-go/tokenizer"
	"github.com/tiktoken-go/tokenizer/codec"
)

// defaultTokenEncoder is created once on first use, so token counting never
// sees a nil codec even when InitTokenEncoders has not run (tests, tools).
var (
	defaultTokenEncoder     tokenizer.Codec
	defaultTokenEncoderOnce sync.Once
)

func getDefaultTokenEncoder() tokenizer.Codec {
	defaultTokenEncoderOnce.Do(func() {
		defaultTokenEncoder = codec.NewCl100kBase()
	})
	return defaultTokenEncoder
}

// tokenEncoderMap is used to store token encoders for different models
var tokenEncoderMap = make(map[string]tokenizer.Codec)

// tokenEncoderMutex protects tokenEncoderMap for concurrent access
var tokenEncoderMutex sync.RWMutex

func InitTokenEncoders() {
	common.SysLog("initializing token encoders")
	getDefaultTokenEncoder()
	common.SysLog("token encoders initialized")
}

func getTokenEncoder(model string) tokenizer.Codec {
	// First, try to get the encoder from cache with read lock
	tokenEncoderMutex.RLock()
	if encoder, exists := tokenEncoderMap[model]; exists {
		tokenEncoderMutex.RUnlock()
		return encoder
	}
	tokenEncoderMutex.RUnlock()

	// If not in cache, create new encoder with write lock
	tokenEncoderMutex.Lock()
	defer tokenEncoderMutex.Unlock()

	// Double-check if another goroutine already created the encoder
	if encoder, exists := tokenEncoderMap[model]; exists {
		return encoder
	}

	// Create new encoder
	modelCodec, err := tokenizer.ForModel(tokenizer.Model(model))
	if err != nil {
		// Cache the default encoder for this model to avoid repeated failures
		encoder := getDefaultTokenEncoder()
		tokenEncoderMap[model] = encoder
		return encoder
	}

	// Cache the new encoder
	tokenEncoderMap[model] = modelCodec
	return modelCodec
}

func getTokenNum(tokenEncoder tokenizer.Codec, text string) int {
	if text == "" {
		return 0
	}
	tkm, _ := tokenEncoder.Count(text)
	return tkm
}
