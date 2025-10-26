package router

import (
	"context"
	"testing"
)

func TestWebTransportConfig(t *testing.T) {
	config := DefaultWebTransportConfig()

	if config.MaxBidiStreams != 100 {
		t.Errorf("expected max_bidi_streams 100, got %d", config.MaxBidiStreams)
	}

	if config.MaxUniStreams != 100 {
		t.Errorf("expected max_uni_streams 100, got %d", config.MaxUniStreams)
	}

	if config.MaxDatagramFrameSize != 65536 {
		t.Errorf("expected max_datagram_frame_size 65536, got %d", config.MaxDatagramFrameSize)
	}

	if !config.EnableDatagrams {
		t.Error("expected enable_datagrams to be true")
	}

	if config.StreamReceiveWindow != 6*1024*1024 {
		t.Errorf("expected stream_receive_window 6MB, got %d", config.StreamReceiveWindow)
	}

	if config.ConnectionReceiveWindow != 15*1024*1024 {
		t.Errorf("expected connection_receive_window 15MB, got %d", config.ConnectionReceiveWindow)
	}

	if config.KeepAliveInterval != 30000 {
		t.Errorf("expected keep_alive_interval 30000ms, got %d", config.KeepAliveInterval)
	}

	if config.MaxIdleTimeout != 60000 {
		t.Errorf("expected max_idle_timeout 60000ms, got %d", config.MaxIdleTimeout)
	}
}

func TestWebTransportSessionInterface(t *testing.T) {
	// Test that the interface is defined correctly
	var _ WebTransportSession = (*webTransportSession)(nil)
}

func TestWebTransportStreamInterface(t *testing.T) {
	// Test that the interface is defined correctly
	var _ WebTransportStream = (*webTransportStream)(nil)
}

func TestWebTransportSessionID(t *testing.T) {
	ctx := context.Background()
	session := &webTransportSession{
		id:  "test-session-id",
		ctx: ctx,
	}

	if session.ID() != "test-session-id" {
		t.Errorf("expected ID 'test-session-id', got %s", session.ID())
	}
}

func TestWebTransportSessionContext(t *testing.T) {
	ctx := context.Background()
	session := &webTransportSession{
		id:  "test-session-id",
		ctx: ctx,
	}

	if session.Context() != ctx {
		t.Error("expected context to match")
	}
}

func TestStreamConfig(t *testing.T) {
	config := DefaultStreamConfig()

	if config.EnableWebTransport {
		t.Error("expected EnableWebTransport to be false by default")
	}

	if config.MaxBidiStreams != 100 {
		t.Errorf("expected MaxBidiStreams 100, got %d", config.MaxBidiStreams)
	}

	if config.MaxUniStreams != 100 {
		t.Errorf("expected MaxUniStreams 100, got %d", config.MaxUniStreams)
	}

	if config.MaxDatagramFrameSize != 65536 {
		t.Errorf("expected MaxDatagramFrameSize 65536, got %d", config.MaxDatagramFrameSize)
	}

	if !config.EnableDatagrams {
		t.Error("expected EnableDatagrams to be true")
	}

	if config.StreamReceiveWindow != 6*1024*1024 {
		t.Errorf("expected StreamReceiveWindow 6MB, got %d", config.StreamReceiveWindow)
	}

	if config.ConnectionReceiveWindow != 15*1024*1024 {
		t.Errorf("expected ConnectionReceiveWindow 15MB, got %d", config.ConnectionReceiveWindow)
	}

	if config.WebTransportKeepAlive != 30000 {
		t.Errorf("expected WebTransportKeepAlive 30000ms, got %d", config.WebTransportKeepAlive)
	}

	if config.WebTransportMaxIdle != 60000 {
		t.Errorf("expected WebTransportMaxIdle 60000ms, got %d", config.WebTransportMaxIdle)
	}
}

func TestWebTransportOptions(t *testing.T) {
	config := &RouteConfig{}

	// Test WithWebTransport
	wtConfig := DefaultWebTransportConfig()
	WithWebTransport(wtConfig).Apply(config)

	if config.Metadata["webtransport.enabled"] != true {
		t.Error("expected webtransport.enabled to be true")
	}

	if config.Metadata["webtransport.config"] == nil {
		t.Error("expected webtransport.config to be set")
	}

	// Test WithWebTransportDatagrams
	WithWebTransportDatagrams(false).Apply(config)

	if config.Metadata["webtransport.datagrams"] != false {
		t.Error("expected webtransport.datagrams to be false")
	}

	// Test WithWebTransportStreams
	WithWebTransportStreams(200, 300).Apply(config)

	if config.Metadata["webtransport.max_bidi_streams"] != int64(200) {
		t.Error("expected webtransport.max_bidi_streams to be 200")
	}

	if config.Metadata["webtransport.max_uni_streams"] != int64(300) {
		t.Error("expected webtransport.max_uni_streams to be 300")
	}
}
