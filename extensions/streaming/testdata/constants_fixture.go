package fixture

// A stand-in for the constant block in internal/streaming.go, with one kind the
// real file does not declare. If messageTypesIn stops noticing an added
// constant, the ack below stops appearing and the test that reads this fails --
// which is the proof the real assertion cannot give without editing a file this
// module shares with another workstream.

// Message types.
const (
	MessageTypeMessage = "message"
	MessageTypeAck     = "ack"
)

// Deliberately not a message type: the prefix filter must skip it.
const NotAMessageType = "ignored"
