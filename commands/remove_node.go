package commands

import (
	"atom/internal/api"
	"fmt"
	"log"
	"net/rpc"
)

func RemoveNodeCommand(socketPath string, nodeName string) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	args := &api.NodeRemoveArgs{NodeName: nodeName}
	var reply api.NodeRemoveReply

	err = client.Call("Atom.RemoveNode", args, &reply)
	if err != nil {
		log.Fatalf("RPC communication failed: %v", err)
	}

	if reply.Error != "" {
		log.Fatalf("Daemon failed to remove node: %s", reply.Error)
	}

	fmt.Printf("Node %s removed successfully.\n", nodeName)
}
