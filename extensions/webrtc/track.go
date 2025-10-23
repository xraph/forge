package webrtc

import (
	"context"
	"fmt"

	"github.com/pion/webrtc/v3"
	"github.com/xraph/forge"
)

// localMediaTrack implements MediaTrack for local tracks
type localMediaTrack struct {
	track   webrtc.TrackLocal
	sender  *webrtc.RTPSender
	kind    TrackKind
	enabled bool
	logger  forge.Logger
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
	t.enabled = enabled
	// Note: Actual muting would be done by stopping sample writes
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
	track   *webrtc.TrackRemote
	enabled bool
	logger  forge.Logger
}

func (t *remoteMediaTrack) ID() string {
	return t.track.ID()
}

func (t *remoteMediaTrack) Kind() TrackKind {
	return TrackKind(t.track.Kind().String())
}

func (t *remoteMediaTrack) Label() string {
	return t.track.StreamID()
}

func (t *remoteMediaTrack) Enabled() bool {
	return t.enabled
}

func (t *remoteMediaTrack) SetEnabled(enabled bool) {
	t.enabled = enabled
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
