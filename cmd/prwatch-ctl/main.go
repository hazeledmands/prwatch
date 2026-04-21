// prwatch-ctl sends commands to a running prwatch instance via IPC.
//
// Usage:
//
//	prwatch-ctl <socket> <keys>     Send keys and print rendered screen
//	prwatch-ctl <socket> --render   Print current screen without sending keys
//	prwatch-ctl <socket> --quit     Tell prwatch to quit
//
// Examples:
//
//	prwatch-ctl /tmp/prwatch.sock "j,j,j"
//	prwatch-ctl /tmp/prwatch.sock "c"
//	prwatch-ctl /tmp/prwatch.sock "v,down,down,tab"
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
)

type request struct {
	Keys   string `json:"keys,omitempty"`
	Action string `json:"action,omitempty"`
}

type response struct {
	Screen string `json:"screen"`
	Error  string `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: prwatch-ctl <socket> <keys|--render|--quit>\n")
		os.Exit(1)
	}

	socketPath := os.Args[1]
	arg := os.Args[2]

	var req request
	switch arg {
	case "--render":
		req = request{Action: "render"}
	case "--quit":
		req = request{Action: "quit"}
	default:
		req = request{Keys: arg}
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to %s: %v\n", socketPath, err)
		os.Exit(1)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending request: %v\n", err)
		os.Exit(1)
	}

	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		os.Exit(1)
	}

	if resp.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	fmt.Print(resp.Screen)
}
