package commands

import (
	"atom/internal/api"
	"fmt"
	"log"
	"net/rpc"
)

func CreateTokenCommand(socketPath string, uses int, asJson bool) {
	// Dial the local daemon using a raw Unix socket
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	args := &api.TokenCreateArgs{Uses: uses}
	var reply api.TokenCreateReply

	// Execute the binary RPC call
	err = client.Call("Atom.CreateToken", args, &reply)
	if err != nil {
		log.Fatalf("RPC communication failed: %v", err)
	}

	if reply.Error != "" {
		log.Fatalf("Daemon failed to create token: %s", reply.Error)
	}

	if asJson {
		fmt.Printf(`{ "token": "%s" }`, reply.TokenString)
		return
	}

	fmt.Printf("Token generated: %s\n", reply.TokenString)
}
