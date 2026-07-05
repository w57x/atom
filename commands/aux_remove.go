package commands

import (
	"atom/internal/api"
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"
)

func RemoveAuxiliaryDevice(socketPath string, idPrefix string, asJson bool) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	args := &api.AuxRemoveArgs{IdPrefix: idPrefix}
	var reply api.AuxRemoveReply

	err = client.Call("Atom.RemoveAuxiliaryDevice", args, &reply)
	if err != nil {
		log.Fatalf("RPC communication failed %v", err)
	}

	if reply.Error != nil {
		log.Fatalf("An error occured: %s", reply.Error.Error())
	}

	if asJson {
		data, err := json.Marshal(reply)
		if err != nil {
			panic(err)
		}

		fmt.Printf("%s", string(data))
		return
	}

	fmt.Println(reply.Message)
}
