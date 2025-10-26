package webrtc

import (
	"fmt"

	"github.com/pion/webrtc/v3"
	"github.com/xraph/forge"
)

// dataChannel implements DataChannel using pion/webrtc
type dataChannel struct {
	dc     *webrtc.DataChannel
	logger forge.Logger

	// Event handlers
	messageHandler DataChannelMessageHandler
	openHandler    DataChannelStateHandler
	closeHandler   DataChannelStateHandler
	
	// State tracking
	closed bool
}

// ID returns channel ID
func (d *dataChannel) ID() string {
	if id := d.dc.ID(); id != nil {
		return fmt.Sprintf("%d", *id)
	}
	return ""
}

// Label returns channel label
func (d *dataChannel) Label() string {
	return d.dc.Label()
}

// State returns channel state
func (d *dataChannel) State() DataChannelState {
	switch d.dc.ReadyState() {
	case webrtc.DataChannelStateConnecting:
		return DataChannelStateConnecting
	case webrtc.DataChannelStateOpen:
		return DataChannelStateOpen
	case webrtc.DataChannelStateClosing:
		return DataChannelStateClosing
	case webrtc.DataChannelStateClosed:
		return DataChannelStateClosed
	default:
		return DataChannelStateClosed
	}
}

// Send sends binary data
func (d *dataChannel) Send(data []byte) error {
	// Check if channel is closed
	if d.closed {
		return fmt.Errorf("data channel is closed: %w", ErrConnectionClosed)
	}
	
	// Check channel state
	state := d.dc.ReadyState()
	if state != webrtc.DataChannelStateOpen {
		return fmt.Errorf("data channel not open (state: %s): %w", state, ErrConnectionClosed)
	}
	
	if err := d.dc.Send(data); err != nil {
		d.logger.Error("failed to send data on data channel",
			forge.F("label", d.Label()),
			forge.F("error", err))
		return fmt.Errorf("failed to send data: %w", err)
	}
	return nil
}

// SendText sends text data
func (d *dataChannel) SendText(text string) error {
	// Check if channel is closed
	if d.closed {
		return fmt.Errorf("data channel is closed: %w", ErrConnectionClosed)
	}
	
	// Check channel state
	state := d.dc.ReadyState()
	if state != webrtc.DataChannelStateOpen {
		return fmt.Errorf("data channel not open (state: %s): %w", state, ErrConnectionClosed)
	}
	
	if err := d.dc.SendText(text); err != nil {
		d.logger.Error("failed to send text on data channel",
			forge.F("label", d.Label()),
			forge.F("error", err))
		return fmt.Errorf("failed to send text: %w", err)
	}
	return nil
}

// OnMessage sets message callback
func (d *dataChannel) OnMessage(handler DataChannelMessageHandler) {
	d.messageHandler = handler

	d.dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if handler != nil {
			handler(msg.Data)
		}
	})
}

// OnOpen sets open callback
func (d *dataChannel) OnOpen(handler DataChannelStateHandler) {
	d.openHandler = handler

	d.dc.OnOpen(func() {
		d.logger.Debug("data channel opened", forge.F("label", d.Label()))
		if handler != nil {
			handler()
		}
	})
}

// OnClose sets close callback
func (d *dataChannel) OnClose(handler DataChannelStateHandler) {
	d.closeHandler = handler

	d.dc.OnClose(func() {
		d.closed = true
		d.logger.Debug("data channel closed event", forge.F("label", d.Label()))
		if handler != nil {
			handler()
		}
	})
}

// Close closes the channel
func (d *dataChannel) Close() error {
	// Check if already closed
	if d.closed {
		return nil // Idempotent close
	}
	
	d.closed = true
	
	if err := d.dc.Close(); err != nil {
		// Log the error but don't return it if the channel is already closed
		d.logger.Debug("error closing data channel (might already be closed)",
			forge.F("label", d.Label()),
			forge.F("error", err))
		return nil // Make close idempotent
	}
	
	d.logger.Debug("data channel closed", forge.F("label", d.Label()))
	return nil
}

// NewDataChannel creates a new data channel on a peer connection
func NewDataChannel(pc *webrtc.PeerConnection, label string, ordered bool, logger forge.Logger) (DataChannel, error) {
	init := &webrtc.DataChannelInit{
		Ordered: &ordered,
	}

	dc, err := pc.CreateDataChannel(label, init)
	if err != nil {
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}

	return &dataChannel{
		dc:     dc,
		logger: logger,
	}, nil
}
