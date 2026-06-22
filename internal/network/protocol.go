package network

const Magic = "ATOMESH.1"

const (
	OpHello       byte = 0x00
	OpRestore     byte = 0x04
	OpChallenge   byte = 0x05
	OpJoinRequest byte = 0x01
	OpJoinAccept  byte = 0x02
	OpKey         byte = 0x03
)

type Message struct {
	Opcode  byte
	Payload []byte
}

type CommandKind uint8

const (
	CmdCreateJoinToken CommandKind = iota
)

type Command struct {
	Opcode  CommandKind
	Payload any
}
