package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSyncableChannelsOmitsSecretsAndLargeFields(t *testing.T) {
	truncateTables(t)

	baseURL := "https://upstream.example"
	channel := &Channel{
		Id:      981,
		Type:    1,
		Key:     "secret-key-that-must-not-be-loaded",
		Name:    "sync-channel",
		Status:  1,
		Models:  "model-a,model-b",
		BaseURL: &baseURL,
		Other:   "large channel configuration",
	}
	require.NoError(t, DB.Create(channel).Error)

	channels, err := GetSyncableChannels()
	require.NoError(t, err)
	require.Len(t, channels, 1)

	assert.Equal(t, channel.Id, channels[0].Id)
	assert.Equal(t, channel.Name, channels[0].Name)
	assert.Equal(t, channel.BaseURL, channels[0].BaseURL)
	assert.Empty(t, channels[0].Key)
	assert.Empty(t, channels[0].Models)
	assert.Empty(t, channels[0].Other)
}
