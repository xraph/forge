package router

// eventLogOpt carries the log, its channel resolver, and who writes it onto a
// route.
type eventLogOpt struct {
	log           EventLog
	channel       func(Context) string
	authoritative bool
}

func (o *eventLogOpt) Apply(config *RouteConfig) {
	config.EventLog = o.log
	config.EventLogChannel = o.channel
	config.EventLogAuthoritative = o.authoritative
}

// WithEventLog makes an SSE route resumable on a best-effort basis.
//
// Events the handler sends are recorded in log, and a client reconnecting with
// a Last-Event-ID is replayed what it missed — or told the gap cannot be filled,
// so it can resync rather than silently continue with stale data.
//
// Best-effort because only connections write this log. Nothing is recorded on a
// channel while nobody is connected to it, so a reconnect that finds nothing
// after its position is reported as a gap rather than as a completed resume:
// the router cannot tell "you missed nothing" from "nothing was listening", and
// a client wrongly told it is caught up serves stale data for the life of the
// session. Applications whose producer appends to the log itself, independently
// of connections, should use WithProducerEventLog, which can report that case
// honestly.
//
// channel partitions the log by request. A route serving one global stream
// returns a constant; a route serving per-tenant streams returns the tenant, so
// one client's events are never replayed to another's reconnect.
func WithEventLog(log EventLog, channel func(Context) string) RouteOption {
	return &eventLogOpt{log: log, channel: channel}
}

// WithProducerEventLog marks the log as written by the application's own
// producer rather than by the connections on this route.
//
// The distinction decides what a zero-event resume means. A log written only
// by connections cannot record anything while nobody is connected, so
// "nothing after your position" is indistinguishable from "nothing was
// listening" — and a client told it is caught up when it is not will serve
// stale data for the life of the session. A producer-written log is fed
// independently of connections, so an empty result genuinely means nothing
// was missed and may be reported as such.
func WithProducerEventLog(log EventLog, channel func(Context) string) RouteOption {
	return &eventLogOpt{log: log, channel: channel, authoritative: true}
}
