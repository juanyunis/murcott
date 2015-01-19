murcott
=======

Decentralized instant messaging framework

[![Build Status](https://travis-ci.org/h2so5/murcott.svg)](https://travis-ci.org/h2so5/murcott)
[![GoDoc](https://godoc.org/github.com/h2so5/murcott?status.svg)](http://godoc.org/github.com/h2so5/murcott)

## Installation

```
go get github.com/h2so5/murcott
```

## Example

```go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/h2so5/murcott"
	"github.com/h2so5/murcott/utils"
	)

func main() {

	// Private key identifies the ownership of your node.
	key := utils.GeneratePrivateKey()
	fmt.Println("Your node id: " + key.Digest().String())

	// Create a client with the private key and the storage.
	client, _ := murcott.NewClient(key, utils.DefaultConfig)

	// Handle incoming messages.
	go func() {
		for {
			m, src, err := client.Read()
			if err != nil {
				return
			}
			if msg, ok := m.(murcott.ChatMessage); ok {
				fmt.Println(msg.Text() + " from " + src.String())
			}
		}
	}()

	// Start client's mainloop.
	go client.Run()

	// Parse a base58-encoded node identifier of your friend.
	dst, _ := utils.NewNodeIDFromString("3CjjdZLV4DqXkc3KtPZPTfBU1AAY")

	for {
		b := make([]byte, 1024)
		len, err := os.Stdin.Read(b)
		if err != nil {
			break
		}
		str := strings.TrimSpace(string(b[:len]))
		if str == "quit" {
			break
		}

		// Send message to the destination node.
		client.SendMessage(dst, murcott.NewPlainChatMessage(str))
	}

	// Stop client's mainloop.
	client.Close()
}
```

## License

MIT License
