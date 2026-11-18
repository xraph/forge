package transport

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// DefaultMaxMessageSize is the largest message a network transport accepts
// when no limit is configured. It matches internal.Config's default.
const DefaultMaxMessageSize = 4 << 20

// DefaultBindAddress is used when a transport is given no address, so a node
// only listens on loopback unless it is told otherwise.
const DefaultBindAddress = "127.0.0.1"

// WireProtocolVersion is the version of the wire envelope this build sends
// and accepts. Bump it whenever the envelope changes incompatibly.
const WireProtocolVersion = 1

// ErrMessageTooLarge is returned when a message exceeds the size limit.
var ErrMessageTooLarge = errors.New("message exceeds the maximum message size")

// ErrProtocolVersion is returned for a message in another wire protocol
// version.
var ErrProtocolVersion = errors.New("unsupported wire protocol version")

// wireMessage is the network form of internal.Message.
//
// internal.Message carries its payload as an interface, which neither JSON nor
// gob can decode back into the concrete Raft types the node expects: JSON
// produced map[string]any and gob refused unregistered types. The envelope
// instead has one typed, optional field per RPC, and exactly the one matching
// Type is set.
type wireMessage struct {
	Version   int                  `json:"v"`
	Type      internal.MessageType `json:"type"`
	From      string               `json:"from"`
	To        string               `json:"to"`
	Timestamp int64                `json:"timestamp,omitempty"`
	RequestID uint64               `json:"request_id,omitempty"`

	AppendEntries           *internal.AppendEntriesRequest    `json:"append_entries,omitempty"`
	AppendEntriesResponse   *internal.AppendEntriesResponse   `json:"append_entries_response,omitempty"`
	RequestVote             *internal.RequestVoteRequest      `json:"request_vote,omitempty"`
	RequestVoteResponse     *internal.RequestVoteResponse     `json:"request_vote_response,omitempty"`
	InstallSnapshot         *internal.InstallSnapshotRequest  `json:"install_snapshot,omitempty"`
	InstallSnapshotResponse *internal.InstallSnapshotResponse `json:"install_snapshot_response,omitempty"`
	TimeoutNow              *internal.TimeoutNowRequest       `json:"timeout_now,omitempty"`
	TimeoutNowResponse      *internal.TimeoutNowResponse      `json:"timeout_now_response,omitempty"`
}

// toMessage reports an error unless exactly the payload matching Type is set.
func asPointer[T any](payload any) (*T, error) {
	switch p := payload.(type) {
	case *T:
		if p == nil {
			return nil, fmt.Errorf("nil %T payload", p)
		}

		return p, nil
	case T:
		return &p, nil
	default:
		return nil, fmt.Errorf("payload has type %T, want %T", payload, new(T))
	}
}

// encodeMessage turns an internal.Message (or a pointer to one) into its wire
// encoding.
func encodeMessage(message any) ([]byte, internal.Message, error) {
	var msg internal.Message

	switch m := message.(type) {
	case internal.Message:
		msg = m
	case *internal.Message:
		if m == nil {
			return nil, msg, errors.New("nil message")
		}

		msg = *m
	default:
		return nil, msg, fmt.Errorf("unsupported message type %T", message)
	}

	w := wireMessage{
		Version:   WireProtocolVersion,
		Type:      msg.Type,
		From:      msg.From,
		To:        msg.To,
		Timestamp: msg.Timestamp,
		RequestID: msg.RequestID,
	}

	var err error

	switch msg.Type {
	case internal.MessageTypeAppendEntries:
		w.AppendEntries, err = asPointer[internal.AppendEntriesRequest](msg.Payload)
	case internal.MessageTypeAppendEntriesResponse:
		w.AppendEntriesResponse, err = asPointer[internal.AppendEntriesResponse](msg.Payload)
	case internal.MessageTypeRequestVote:
		w.RequestVote, err = asPointer[internal.RequestVoteRequest](msg.Payload)
	case internal.MessageTypeRequestVoteResponse:
		w.RequestVoteResponse, err = asPointer[internal.RequestVoteResponse](msg.Payload)
	case internal.MessageTypeInstallSnapshot:
		w.InstallSnapshot, err = asPointer[internal.InstallSnapshotRequest](msg.Payload)
	case internal.MessageTypeInstallSnapshotResponse:
		w.InstallSnapshotResponse, err = asPointer[internal.InstallSnapshotResponse](msg.Payload)
	case internal.MessageTypeTimeoutNow:
		w.TimeoutNow, err = asPointer[internal.TimeoutNowRequest](msg.Payload)
	case internal.MessageTypeTimeoutNowResponse:
		w.TimeoutNowResponse, err = asPointer[internal.TimeoutNowResponse](msg.Payload)
	case internal.MessageTypeHeartbeat:
		if msg.Payload != nil {
			err = fmt.Errorf("heartbeat carries a %T payload", msg.Payload)
		}
	default:
		err = fmt.Errorf("unknown message type %q", msg.Type)
	}

	if err != nil {
		return nil, msg, fmt.Errorf("cannot encode %s message: %w", msg.Type, err)
	}

	data, err := json.Marshal(&w)
	if err != nil {
		return nil, msg, fmt.Errorf("failed to marshal message: %w", err)
	}

	return data, msg, nil
}

// decodeMessage parses a wire message and checks that it carries exactly the
// payload its type announces.
func decodeMessage(data []byte) (internal.Message, error) {
	var w wireMessage
	if err := json.Unmarshal(data, &w); err != nil {
		return internal.Message{}, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	if w.Version != WireProtocolVersion {
		return internal.Message{}, fmt.Errorf("%w: peer %q sent version %d, this node speaks version %d",
			ErrProtocolVersion, w.From, w.Version, WireProtocolVersion)
	}

	msg := internal.Message{
		Type:      w.Type,
		From:      w.From,
		To:        w.To,
		Timestamp: w.Timestamp,
		RequestID: w.RequestID,
	}

	set := 0

	for _, present := range []bool{
		w.AppendEntries != nil, w.AppendEntriesResponse != nil,
		w.RequestVote != nil, w.RequestVoteResponse != nil,
		w.InstallSnapshot != nil, w.InstallSnapshotResponse != nil,
		w.TimeoutNow != nil, w.TimeoutNowResponse != nil,
	} {
		if present {
			set++
		}
	}

	switch w.Type {
	case internal.MessageTypeAppendEntries:
		msg.Payload, set = w.AppendEntries, set-boolToInt(w.AppendEntries != nil)
	case internal.MessageTypeAppendEntriesResponse:
		msg.Payload, set = w.AppendEntriesResponse, set-boolToInt(w.AppendEntriesResponse != nil)
	case internal.MessageTypeRequestVote:
		msg.Payload, set = w.RequestVote, set-boolToInt(w.RequestVote != nil)
	case internal.MessageTypeRequestVoteResponse:
		msg.Payload, set = w.RequestVoteResponse, set-boolToInt(w.RequestVoteResponse != nil)
	case internal.MessageTypeInstallSnapshot:
		msg.Payload, set = w.InstallSnapshot, set-boolToInt(w.InstallSnapshot != nil)
	case internal.MessageTypeInstallSnapshotResponse:
		msg.Payload, set = w.InstallSnapshotResponse, set-boolToInt(w.InstallSnapshotResponse != nil)
	case internal.MessageTypeTimeoutNow:
		msg.Payload, set = w.TimeoutNow, set-boolToInt(w.TimeoutNow != nil)
	case internal.MessageTypeTimeoutNowResponse:
		msg.Payload, set = w.TimeoutNowResponse, set-boolToInt(w.TimeoutNowResponse != nil)
	case internal.MessageTypeHeartbeat:
		if set != 0 {
			return internal.Message{}, errors.New("heartbeat message carries a payload")
		}

		return msg, nil
	default:
		return internal.Message{}, fmt.Errorf("unknown message type %q", w.Type)
	}

	if msg.Payload == nil || isNilPayload(msg.Payload) {
		return internal.Message{}, fmt.Errorf("%s message has no %s payload", w.Type, w.Type)
	}

	if set != 0 {
		return internal.Message{}, fmt.Errorf("%s message carries payloads for other message types", w.Type)
	}

	return msg, nil
}

func isNilPayload(p any) bool {
	switch v := p.(type) {
	case *internal.AppendEntriesRequest:
		return v == nil
	case *internal.AppendEntriesResponse:
		return v == nil
	case *internal.RequestVoteRequest:
		return v == nil
	case *internal.RequestVoteResponse:
		return v == nil
	case *internal.InstallSnapshotRequest:
		return v == nil
	case *internal.InstallSnapshotResponse:
		return v == nil
	case *internal.TimeoutNowRequest:
		return v == nil
	case *internal.TimeoutNowResponse:
		return v == nil
	default:
		return false
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// certificateNamesNode reports whether a verified peer certificate names the
// node in one of its DNS subject alternative names. With peer identity
// checking on, every node's certificate must carry its node ID as a DNS SAN.
func certificateNamesNode(cert *x509.Certificate, nodeID string) bool {
	return cert != nil && nodeID != "" && slices.Contains(cert.DNSNames, nodeID)
}

// verifySender checks a message's From against the client certificate of the
// connection it arrived on.
func verifySender(state *tls.ConnectionState, from string) error {
	if state == nil || len(state.PeerCertificates) == 0 {
		return errors.New("peer identity check requires a client certificate, none was presented")
	}

	if !certificateNamesNode(state.PeerCertificates[0], from) {
		return fmt.Errorf("message from %q does not match the peer certificate", from)
	}

	return nil
}

// checkIdentityTLS validates the TLS settings needed for peer identity checks,
// so a misconfiguration fails at startup instead of being ignored.
func checkIdentityTLS(cfg *tls.Config, requireIdentity bool) error {
	if !requireIdentity {
		return nil
	}

	if cfg == nil {
		return errors.New("peer identity checks need TLS: set TLSConfig")
	}

	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		return errors.New("peer identity checks need mutual TLS: TLSConfig.ClientAuth must be tls.RequireAndVerifyClientCert")
	}

	if cfg.InsecureSkipVerify {
		return errors.New("peer identity checks cannot be combined with InsecureSkipVerify")
	}

	return nil
}

// clientTLSFor returns the client TLS config for dialing a peer. With identity
// checks on, the peer's node ID becomes the server name, so the standard
// certificate verification also proves the server is the node we meant to
// reach.
func clientTLSFor(cfg *tls.Config, peerID string, requireIdentity bool) *tls.Config {
	if cfg == nil {
		return nil
	}

	c := cfg.Clone()
	if requireIdentity {
		c.ServerName = peerID
	}

	return c
}
