// Package murcott is a decentralized instant messaging framework.
package client

import (
	"errors"
	"time"

	"github.com/h2so5/murcott/log"
	"github.com/h2so5/murcott/router"
	"github.com/h2so5/murcott/utils"
	"gopkg.in/vmihailenco/msgpack.v2"
)

type Client struct {
	router     *router.Router
	readch     chan router.Message
	msgHandler messageHandler
	profile    UserProfile
	id         utils.NodeID
	config     utils.Config
	Roster     *Roster
	Logger     *log.Logger
}

type messageHandler func(src utils.NodeID, msg ChatMessage)
type Message interface{}

// NewClient generates a Client with the given PrivateKey.
func NewClient(key *utils.PrivateKey, config utils.Config) (*Client, error) {
	logger := log.NewLogger()

	r, err := router.NewRouter(key, logger, config)
	if err != nil {
		return nil, err
	}

	c := &Client{
		router: r,
		readch: make(chan router.Message),
		id:     utils.NewNodeID([4]byte{1, 1, 1, 1}, key.Digest()),
		config: config,
		Roster: &Roster{},
		Logger: logger,
	}

	return c, nil
}

func (c *Client) Read() (Message, utils.NodeID, error) {
	m := <-c.readch

	var t struct {
		Type string `msgpack:"type"`
	}
	err := msgpack.Unmarshal(m.Payload, &t)
	if err != nil {
		return nil, utils.NodeID{}, err
	}

	switch t.Type {
	case "chat":
		u := struct {
			Content ChatMessage `msgpack:"content"`
			ID      string      `msgpack:"id"`
		}{}
		err := msgpack.Unmarshal(m.Payload, &u)
		if err != nil {
			return nil, utils.NodeID{}, err
		}
		return u.Content, m.ID, nil

	default:
		return nil, utils.NodeID{}, errors.New("Unknown message type: " + t.Type)
	}
}

// Starts a mainloop in the current goroutine.
func (c *Client) Run() {

	// Discover bootstrap nodes
	c.router.Discover(c.config.Bootstrap())

	exit := make(chan int)

	go func() {
		defer func() {
			close(exit)
		}()

		for {
			m, err := c.router.RecvMessage()
			if err != nil {
				return
			}
			c.readch <- m
		}
	}()

	<-exit
}

// Stops the current mainloop.
func (c *Client) Close() {
	time.Sleep(100 * time.Millisecond)
	c.router.Close()
}

// Sends the given message to the destination node.
func (c *Client) SendMessage(dst utils.NodeID, msg ChatMessage) error {
	t := struct {
		Type    string      `msgpack:"type"`
		Content interface{} `msgpack:"content"`
		ID      string      `msgpack:"id"`
	}{Type: "chat", Content: msg}

	data, err := msgpack.Marshal(t)
	if err != nil {
		return err
	}

	c.router.SendMessage(dst, data)
	return nil
}

func (c *Client) ID() utils.NodeID {
	return c.id
}

func (c *Client) Nodes() int {
	return len(c.router.KnownNodes())
}

func (c *Client) MarshalCache() (data []byte, err error) {
	return msgpack.Marshal(c.router.KnownNodes())
}

func (c *Client) UnmarshalCache(data []byte) error {
	var nodes []utils.NodeInfo
	err := msgpack.Unmarshal(data, &nodes)
	for _, n := range nodes {
		c.router.AddNode(n)
	}
	return err
}
