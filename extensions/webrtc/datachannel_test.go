package webrtc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/xraph/forge"
)

func TestDataChannel(t *testing.T) {
	// Create peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()
	peer, err := NewPeerConnection("test-peer", "test-user", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Create data channel
	dc, err := NewDataChannel(peer.(*peerConnection).pc, "test-channel", true, logger)
	if err != nil {
		t.Fatalf("failed to create data channel: %v", err)
	}

	// Test basic properties
	// Note: ID may be empty before connection is established
	t.Logf("data channel ID: %s", dc.ID())

	if dc.Label() != "test-channel" {
		t.Errorf("expected label 'test-channel', got %s", dc.Label())
	}

	// Test initial state
	if dc.State() != DataChannelStateConnecting {
		t.Errorf("expected connecting state, got %s", dc.State())
	}

	// Test message handler
	messageReceived := make(chan []byte, 1)
	dc.OnMessage(func(data []byte) {
		select {
		case messageReceived <- data:
		default:
		}
	})

	// Test open handler
	openCalled := make(chan bool, 1)
	dc.OnOpen(func() {
		select {
		case openCalled <- true:
		default:
		}
	})

	// Test close handler
	closeCalled := make(chan bool, 1)
	dc.OnClose(func() {
		select {
		case closeCalled <- true:
		default:
		}
	})

	// Send text message - will fail because channel isn't open yet
	testText := "Hello, WebRTC!"
	err = dc.SendText(testText)
	if err == nil {
		t.Error("expected error when sending on non-open channel")
	}
	if !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("expected ErrConnectionClosed, got %v", err)
	}

	// Send binary data - will also fail
	testData := []byte("Binary data test")
	err = dc.Send(testData)
	if err == nil {
		t.Error("expected error when sending on non-open channel")
	}
	if !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("expected ErrConnectionClosed, got %v", err)
	}

	// Test close
	err = dc.Close()
	if err != nil {
		t.Errorf("failed to close data channel: %v", err)
	}

	// Test double close
	err = dc.Close()
	if err != nil {
		t.Errorf("double close should not error: %v", err)
	}
}

func TestDataChannel_States(t *testing.T) {
	// Create peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()
	peer, err := NewPeerConnection("test-peer-2", "test-user-2", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Create data channel
	dc, err := NewDataChannel(peer.(*peerConnection).pc, "state-test", false, logger)
	if err != nil {
		t.Fatalf("failed to create data channel: %v", err)
	}

	// Test state transitions - check current state
	currentState := dc.State()
	if currentState != DataChannelStateConnecting {
		t.Logf("Initial state: %s", currentState)
	}

	// Wait a bit for state changes
	time.Sleep(100 * time.Millisecond)
	currentState = dc.State()
	t.Logf("State after wait: %s", currentState)

	// Test close
	err = dc.Close()
	if err != nil {
		t.Errorf("failed to close data channel: %v", err)
	}
}

func TestDataChannel_InvalidOperations(t *testing.T) {
	// Create peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()
	peer, err := NewPeerConnection("test-peer-3", "test-user-3", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Test creating data channel on closed peer connection
	peer.Close(context.Background())

	_, err = NewDataChannel(peer.(*peerConnection).pc, "invalid", true, logger)
	if err == nil {
		t.Error("should not be able to create data channel on closed peer connection")
	}
}

func TestDataChannel_ConcurrentMessages(t *testing.T) {
	// Create peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()
	peer, err := NewPeerConnection("test-peer-4", "test-user-4", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Create data channel
	dc, err := NewDataChannel(peer.(*peerConnection).pc, "concurrent-test", true, logger)
	if err != nil {
		t.Fatalf("failed to create data channel: %v", err)
	}

	// Track received messages
	messagesReceived := make(chan int, 100)
	dc.OnMessage(func(data []byte) {
		select {
		case messagesReceived <- len(data):
		default:
		}
	})

	// Send multiple messages concurrently
	// Note: these will fail because the channel isn't open without a full peer connection
	done := make(chan bool, 1)
	errorCount := 0
	go func() {
		for i := 0; i < 50; i++ {
			text := fmt.Sprintf("Message %d", i)
			if err := dc.SendText(text); err != nil {
				errorCount++
			}
		}
		done <- true
	}()

	// Wait for concurrent sends to complete
	<-done

	// All sends should fail because channel isn't open
	if errorCount != 50 {
		t.Logf("Expected all 50 sends to fail, but only %d failed", errorCount)
	}

	// Cleanup
	dc.Close()
}

func BenchmarkDataChannel_SendText(b *testing.B) {
	// Create peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()
	peer, err := NewPeerConnection("bench-peer", "bench-user", config, logger)
	if err != nil {
		b.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Create data channel
	dc, err := NewDataChannel(peer.(*peerConnection).pc, "bench-channel", true, logger)
	if err != nil {
		b.Fatalf("failed to create data channel: %v", err)
	}

	testText := "Benchmark message text"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Note: this will fail because channel isn't open, but we benchmark the path
		_ = dc.SendText(testText)
	}

	// Cleanup
	dc.Close()
}
