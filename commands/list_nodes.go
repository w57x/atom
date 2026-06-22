package commands

import (
	"atom/internal/api"
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"
	"os"

	"github.com/logrusorgru/aurora/v4"
	"github.com/olekukonko/tablewriter"
)

func ListNodesCommand(socketPath string, asJson bool) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	args := &api.NodeListArgs{}
	var reply api.NodeListReply

	err = client.Call("Atom.ListNodes", args, &reply)
	if err != nil {
		log.Fatalf("RPC communication failed: %v", err)
	}

	if reply.Error != "" {
		log.Fatalf("Daemon failed to list nodes: %s", reply.Error)
	}

	if asJson {
		b, err := json.MarshalIndent(reply.Nodes, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal JSON: %v", err)
		}
		fmt.Println(string(b))
		return
	}

	if len(reply.Nodes) == 0 {
		fmt.Println("No nodes found in the mesh.")
		return
	}

	var data [][]any
	for _, node := range reply.Nodes {
		nameStr := node.Name
		if node.IsSelf {
			nameStr += " (Self)"
		}
		if node.IsLeader {
			nameStr += " (Leader)"
		}

		var name any = nameStr
		if node.IsLeader {
			name = aurora.Green(nameStr).Bold()
		} else if node.IsSelf {
			name = aurora.Blue(nameStr)
		}

		endpointStr := node.PublicEndpoint
		if endpointStr == "" {
			endpointStr = "N/A"
		}

		pubKeyStr := node.PubKey
		if len(pubKeyStr) > 16 {
			pubKeyStr = pubKeyStr[:16] + "..."
		}

		statusStr := "Offline"
		statusColor := aurora.Red(statusStr)
		if node.IsOnline {
			statusStr = "Online"
			statusColor = aurora.Green(statusStr)
		}

		data = append(data, []any{
			fmt.Sprint(name),
			fmt.Sprint(aurora.Cyan(node.VPNIP)),
			fmt.Sprint(aurora.Gray(12, endpointStr)),
			fmt.Sprint(statusColor),
			fmt.Sprint(aurora.Gray(10, pubKeyStr)),
		})
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "VPN IP", "ENDPOINT", "STATUS", "PUBKEY")
	table.Bulk(data)
	if err := table.Render(); err != nil {
		log.Fatalf("Failed to render table: %v", err)
	}
	fmt.Println()
}
