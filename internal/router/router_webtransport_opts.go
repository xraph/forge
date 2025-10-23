package router

// WithWebTransport enables WebTransport support with the given configuration
func WithWebTransport(config WebTransportConfig) RouteOption {
	return &webTransportOpt{config: config}
}

type webTransportOpt struct {
	config WebTransportConfig
}

func (o *webTransportOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}
	cfg.Metadata["webtransport.enabled"] = true
	cfg.Metadata["webtransport.config"] = o.config
}

// WithWebTransportDatagrams enables datagram support for WebTransport
func WithWebTransportDatagrams(enabled bool) RouteOption {
	return &webTransportDatagramsOpt{enabled: enabled}
}

type webTransportDatagramsOpt struct {
	enabled bool
}

func (o *webTransportDatagramsOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}
	cfg.Metadata["webtransport.datagrams"] = o.enabled
}

// WithWebTransportStreams sets the maximum number of streams
func WithWebTransportStreams(maxBidi, maxUni int64) RouteOption {
	return &webTransportStreamsOpt{maxBidi: maxBidi, maxUni: maxUni}
}

type webTransportStreamsOpt struct {
	maxBidi int64
	maxUni  int64
}

func (o *webTransportStreamsOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}
	cfg.Metadata["webtransport.max_bidi_streams"] = o.maxBidi
	cfg.Metadata["webtransport.max_uni_streams"] = o.maxUni
}
