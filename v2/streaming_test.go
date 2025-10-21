package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultStreamConfig(t *testing.T) {
	config := DefaultStreamConfig()

	assert.Equal(t, 4096, config.ReadBufferSize)
	assert.Equal(t, 4096, config.WriteBufferSize)
	assert.False(t, config.EnableCompression)
	assert.Equal(t, 3000, config.RetryInterval)
	assert.True(t, config.KeepAlive)
}

func TestStreamConfig_Custom(t *testing.T) {
	config := StreamConfig{
		ReadBufferSize:    8192,
		WriteBufferSize:   8192,
		EnableCompression: true,
		RetryInterval:     5000,
		KeepAlive:         false,
	}

	assert.Equal(t, 8192, config.ReadBufferSize)
	assert.Equal(t, 8192, config.WriteBufferSize)
	assert.True(t, config.EnableCompression)
	assert.Equal(t, 5000, config.RetryInterval)
	assert.False(t, config.KeepAlive)
}
