package network

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"net"

	"atom/internal/utils"
)

const Magic = "ATOMESH.1"

const (
	OpHello           byte = 0x00
	OpRestore         byte = 0x04
	OpChallengeAccept byte = 0x05
	OpChallenge       byte = 0x06
	OpJoinRequest     byte = 0x01
	OpJoinAccept      byte = 0x02
	__reserved        byte = 0x03
)

type Message struct {
	Opcode  byte
	Payload any
}

type JoinRequestPayload struct {
	Name     string
	PubKey   string
	WGPort   int
	RaftPort int
}

type JoinAcceptPayload struct {
	AssignedIP   string
	ServerPubKey string
	ServerWGPort int
	ServerVPNIP  string
}

// Wire represents a stateful connection that handles framing, serialization, and optional encryption.
type Wire struct {
	conn   net.Conn
	secret string
}

func NewWire(conn net.Conn) *Wire {
	return &Wire{conn: conn}
}

// SetSecret upgrades the Wire to automatically encrypt and decrypt all subsequent messages.
func (w *Wire) SetSecret(secret string) {
	w.secret = secret
}

func (w *Wire) Write(msg Message) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(msg); err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	payload := buf.Bytes()

	if len(w.secret) != 0 {
		var err error
		payload, err = utils.EncryptPayload(w.secret, payload)
		if err != nil {
			return fmt.Errorf("encryption failed: %w", err)
		}
	}

	if _, err := w.conn.Write([]byte(Magic)); err != nil {
		return err
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(payload)))
	if _, err := w.conn.Write(lenBuf); err != nil {
		return err
	}

	if _, err := w.conn.Write(payload); err != nil {
		return err
	}

	return nil
}

func (w *Wire) Read() (Message, error) {
	var msg Message

	magicBuf := make([]byte, len(Magic))
	if _, err := io.ReadFull(w.conn, magicBuf); err != nil {
		return msg, fmt.Errorf("failed to read magic: %w", err)
	}

	if string(magicBuf) != Magic {
		return msg, fmt.Errorf("invalid magic header")
	}

	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(w.conn, lenBuf); err != nil {
		return msg, fmt.Errorf("failed to read length: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBuf)

	if length > 1024*1024*10 {
		return msg, fmt.Errorf("payload too large")
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(w.conn, payload); err != nil {
		return msg, fmt.Errorf("failed to read payload: %w", err)
	}

	if len(w.secret) != 0 {
		var err error
		payload, err = utils.DecryptPayload(w.secret, payload)
		if err != nil {
			return msg, fmt.Errorf("decryption failed: %w", err)
		}
	}

	buf := bytes.NewReader(payload)
	if err := gob.NewDecoder(buf).Decode(&msg); err != nil {
		return msg, fmt.Errorf("failed to decode message: %w", err)
	}

	return msg, nil
}

func (w *Wire) Expect(opcode byte) (Message, error) {
	msg, err := w.Read()
	if err != nil {
		return msg, err
	}
	if msg.Opcode != opcode {
		return msg, fmt.Errorf("expected opcode %d, got %d", opcode, msg.Opcode)
	}
	return msg, nil
}

func init() {
	gob.Register(JoinRequestPayload{})
	gob.Register(JoinAcceptPayload{})
}

type CommandKind uint8

const (
	CmdCreateJoinToken CommandKind = iota
	CmdAddNode
	CmdConsumeToken
	CmdRevokeToken
	CmdRemoveNode
	CmdSetNetworkCIDR
)

type Command struct {
	Opcode  CommandKind
	Payload any
}
