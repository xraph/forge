package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithEventLog_AppliesToConfig(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	config := &RouteConfig{}

	WithEventLog(log, func(Context) string { return "orders" }).Apply(config)

	require.NotNil(t, config.EventLog)
	require.NotNil(t, config.EventLogChannel)
	assert.Equal(t, "orders", config.EventLogChannel(nil))
}

// A route with no option applied must be indistinguishable from today's, which
// is what keeps replay opt-in.
func TestRouteConfig_EventLogUnsetByDefault(t *testing.T) {
	config := &RouteConfig{}

	assert.Nil(t, config.EventLog)
	assert.Nil(t, config.EventLogChannel)
}
