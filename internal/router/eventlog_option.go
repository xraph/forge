package router

// eventLogOpt carries the log and its channel resolver onto a route.
type eventLogOpt struct {
	log     EventLog
	channel func(Context) string
}

func (o *eventLogOpt) Apply(config *RouteConfig) {
	config.EventLog = o.log
	config.EventLogChannel = o.channel
}

// WithEventLog makes an SSE route resumable.
//
// Events the handler sends are recorded in log, and a client reconnecting with
// a Last-Event-ID is replayed what it missed — or told the gap cannot be filled,
// so it can resync rather than silently continue with stale data.
//
// channel partitions the log by request. A route serving one global stream
// returns a constant; a route serving per-tenant streams returns the tenant, so
// one client's events are never replayed to another's reconnect.
func WithEventLog(log EventLog, channel func(Context) string) RouteOption {
	return &eventLogOpt{log: log, channel: channel}
}
