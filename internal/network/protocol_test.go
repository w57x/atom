package network

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestWireOversizedPayloadRejection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	wire := NewWire(serverConn)

	go func() {
		// Send valid Magic
		_, _ = clientConn.Write([]byte(Magic))
		// Send length exceeding MaxPayloadSize
		lenBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(lenBuf, uint32(MaxPayloadSize+1))
		_, _ = clientConn.Write(lenBuf)
	}()

	_, err := wire.Read()
	if err == nil {
		t.Fatal("expected Read to reject payload exceeding MaxPayloadSize, but got nil")
	}
}

func TestWireInvalidMagicRejection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	wire := NewWire(serverConn)

	go func() {
		_, _ = clientConn.Write([]byte("BAD_MAGIC"))
	}()

	_, err := wire.Read()
	if err == nil {
		t.Fatal("expected Read to reject invalid magic header, but got nil")
	}
}

func TestWireRoundtrip(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	wireClient := NewWire(clientConn)
	wireServer := NewWire(serverConn)

	msgToSend := Message{
		Opcode:  OpHello,
		Payload: "test-token-id",
	}

	go func() {
		if err := wireClient.Write(msgToSend); err != nil {
			t.Errorf("wireClient.Write failed: %v", err)
		}
	}()

	received, err := wireServer.Read()
	if err != nil {
		t.Fatalf("wireServer.Read failed: %v", err)
	}

	if received.Opcode != OpHello {
		t.Errorf("expected opcode %d, got %d", OpHello, received.Opcode)
	}
	if received.Payload.(string) != "test-token-id" {
		t.Errorf("expected payload 'test-token-id', got '%v'", received.Payload)
	}
}
