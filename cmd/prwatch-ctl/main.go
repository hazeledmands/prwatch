// prwatch-ctl sends commands to a running prwatch instance via IPC.
//
// Usage:
//
//	prwatch-ctl <socket> <keys>     Send keys and print rendered screen
//	prwatch-ctl <socket> --render   Print current screen without sending keys
//	prwatch-ctl <socket> --quit     Tell prwatch to quit
//
// With no <socket>, the per-user default is used — the same path prwatch
// itself picks (under XDG_RUNTIME_DIR, else the user cache dir).
//
// Examples:
//
//	prwatch-ctl "j,j,j"
//	prwatch-ctl --render
//	prwatch-ctl /custom/path.sock "v,down,down,tab"
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/hazeledmands/prwatch/internal/ui"
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
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: prwatch-ctl [socket] <keys|--render|--quit>\n")
		os.Exit(1)
	}

	var socketPath, arg string
	if len(os.Args) >= 3 {
		socketPath = os.Args[1]
		arg = os.Args[2]
	} else {
		// Same function the server uses, so both ends cannot drift apart.
		var err error
		socketPath, err = ui.DefaultIPCSocketPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		arg = os.Args[1]
	}

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

	// Print the screen even alongside an error: when a follow-up command
	// panicked, the half-updated screen is the evidence of what went wrong.
	// The non-zero exit is what says not to trust it.
	fmt.Print(resp.Screen)

	if resp.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}
}
