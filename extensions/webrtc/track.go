package webrtc

import (
	"context"
	"fmt"

	"github.com/pion/webrtc/v3"
	"github.com/xraph/forge"
)

// localMediaTrack implements MediaTrack for local tracks
type localMediaTrack struct {
	track        webrtc.TrackLocal
	sender       *webrtc.RTPSender
	kind         TrackKind
	enabled      bool
	muted        bool // Local mute state
	originalTrack webrtc.TrackLocal // Store original track for unmute
	logger       forge.Logger
}

// NewLocalTrack creates a new local media track
func NewLocalTrack(kind TrackKind, id, label string, logger forge.Logger) (MediaTrack, error) {
	var track webrtc.TrackLocal
	var err error

	if kind == TrackKindAudio {
		track, err = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
			id,
			label,
		)
	} else if kind == TrackKindVideo {
		track, err = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
			id,
			label,
		)
	} else {
		return nil, fmt.Errorf("invalid track kind: %s", kind)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create local track: %w", err)
	}

	return &localMediaTrack{
		track:   track,
		kind:    kind,
		enabled: true,
		logger:  logger,
	}, nil
}

func (t *localMediaTrack) ID() string {
	return t.track.ID()
}

func (t *localMediaTrack) Kind() TrackKind {
	return t.kind
}

func (t *localMediaTrack) Label() string {
	if staticTrack, ok := t.track.(*webrtc.TrackLocalStaticSample); ok {
		return staticTrack.StreamID()
	}
	return ""
}

func (t *localMediaTrack) Enabled() bool {
	return t.enabled
}

func (t *localMediaTrack) SetEnabled(enabled bool) {
	if t.enabled == enabled {
		return // No-op if already in desired state
	}
	
	t.enabled = enabled
	
	// If we have a sender, control the actual media transmission
	if t.sender != nil {
		if enabled {
			// Unmute: restore the original track
			if t.originalTrack != nil && t.muted {
				if err := t.sender.ReplaceTrack(t.originalTrack); err != nil {
					t.logger.Error("failed to restore track on unmute",
						forge.F("track_id", t.ID()),
						forge.F("error", err))
					return
				}
				t.track = t.originalTrack
				t.muted = false
				t.logger.Debug("unmuted track", forge.F("track_id", t.ID()))
			}
		} else {
			// Mute: replace with a silent/null track or disable the sender
			// Store the original track if not already stored
			if !t.muted {
				t.originalTrack = t.track
				t.muted = true
			}
			
			// For muting, we set the track to nil (stops transmission)
			// In a production environment, you might want to send silence instead
			if err := t.sender.ReplaceTrack(nil); err != nil {
				t.logger.Error("failed to mute track",
					forge.F("track_id", t.ID()),
					forge.F("error", err))
				return
			}
			t.logger.Debug("muted track", forge.F("track_id", t.ID()))
		}
	} else {
		// No sender attached, just track the state
		t.logger.Debug("track enabled state changed (no sender)",
			forge.F("track_id", t.ID()),
			forge.F("enabled", enabled))
	}
}

func (t *localMediaTrack) GetSettings() TrackSettings {
	// For local tracks, settings would come from media constraints
	return TrackSettings{
		// Default settings
		Width:      640,
		Height:     480,
		FrameRate:  30,
		Bitrate:    1000,
		SampleRate: 48000,
		Channels:   2,
	}
}

func (t *localMediaTrack) GetStats(ctx context.Context) (*TrackStats, error) {
	stats := &TrackStats{
		TrackID: t.ID(),
		Kind:    t.kind,
	}

	// Get stats from sender if available
	if t.sender != nil {
		// Stats would be retrieved from RTP sender
		// This requires iterating through sender.GetStats()
	}

	return stats, nil
}

func (t *localMediaTrack) Close() error {
	// Local tracks don't need explicit closing
	t.logger.Debug("closed local track", forge.F("track_id", t.ID()))
	return nil
}

// remoteMediaTrack implements MediaTrack for remote tracks
type remoteMediaTrack struct {
	track    *webrtc.TrackRemote
	receiver *webrtc.RTPReceiver // Store receiver for stats
	enabled  bool                 // Controls local playback
	logger   forge.Logger
}

func (t *remoteMediaTrack) ID() string {
	if t.track == nil {
		return "remote-track-nil"
	}
	return t.track.ID()
}

func (t *remoteMediaTrack) Kind() TrackKind {
	if t.track == nil {
		return TrackKindAudio // Default to audio
	}
	return TrackKind(t.track.Kind().String())
}

func (t *remoteMediaTrack) Label() string {
	if t.track == nil {
		return "remote-track-nil"
	}
	return t.track.StreamID()
}

func (t *remoteMediaTrack) Enabled() bool {
	return t.enabled
}

func (t *remoteMediaTrack) SetEnabled(enabled bool) {
	if t.enabled == enabled {
		return // No-op if already in desired state
	}
	
	t.enabled = enabled
	
	// For remote tracks, "enabled" controls local playback
	// The actual media is controlled by the sender
	// This flag is used by the application to decide whether to play the media
	t.logger.Debug("remote track playback state changed",
		forge.F("track_id", t.ID()),
		forge.F("enabled", enabled))
}

func (t *remoteMediaTrack) GetSettings() TrackSettings {
	// Get settings from codec parameters
	return TrackSettings{
		// Would be populated from track's RTP parameters
	}
}

func (t *remoteMediaTrack) GetStats(ctx context.Context) (*TrackStats, error) {
	stats := &TrackStats{
		TrackID: t.ID(),
		Kind:    t.Kind(),
	}

	// Remote track stats would come from receiver
	return stats, nil
}

func (t *remoteMediaTrack) Close() error {
	t.logger.Debug("closed remote track", forge.F("track_id", t.ID()))
	return nil
}
