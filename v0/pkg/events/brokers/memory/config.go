package memory

import (
	"time"
)

// MemoryBrokerConfig defines configuration for the memory broker
type MemoryBrokerConfig struct {
	BufferSize       int           `yaml:"buffer_size" json:"buffer_size"`
	MaxTopics        int           `yaml:"max_topics" json:"max_topics"`
	EnableMetrics    bool          `yaml:"enable_metrics" json:"enable_metrics"`
	ProcessTimeout   time.Duration `yaml:"process_timeout" json:"process_timeout"`
	MaxSubscribers   int           `yaml:"max_subscribers" json:"max_subscribers"`
	EnableDeadLetter bool          `yaml:"enable_dead_letter" json:"enable_dead_letter"`
}

// DefaultMemoryBrokerConfig returns default configuration
func DefaultMemoryBrokerConfig() MemoryBrokerConfig {
	return MemoryBrokerConfig{
		BufferSize:       1000,
		MaxTopics:        100,
		EnableMetrics:    true,
		ProcessTimeout:   time.Second * 30,
		MaxSubscribers:   1000,
		EnableDeadLetter: false,
	}
}
