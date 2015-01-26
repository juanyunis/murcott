// Package murcott is a decentralized instant messaging framework.
package murcott

import (
	"errors"
	"sync"

	"github.com/h2so5/murcott/log"
	"github.com/h2so5/murcott/router"
	"github.com/h2so5/murcott/utils"
	"gopkg.in/vmihailenco/msgpack.v2"
)

type Client struct {
	router *router.Router
	readch chan router.Message
	mbuf   *messageBuffer
	id     utils.NodeID
	config utils.Config

	profile UserProfile
	Roster  Roster

	Logger *log.Logger
}

type readPair struct {
	M  Message
	ID utils.NodeID
}

type messageBuffer struct {
	b           []readPair
	begin, size int
	ch, closed  chan int
	mutex       sync.Mutex
}

func newMessageBuffer(size int) *messageBuffer {
	return &messageBuffer{
		b:      make([]readPair, size),
		ch:     make(chan int),
		closed: make(chan int),
	}
}

func (b *messageBuffer) Push(m readPair) {
	b.mutex.Lock()
	index := (b.begin + b.size) % len(b.b)
	b.b[index] = m
	if b.size < len(b.b) {
		b.size++
	} else {
		b.begin = (b.begin + 1) % len(b.b)
	}
	b.mutex.Unlock()
	select {
	case <-b.closed:
	case b.ch <- 0:
	default:
	}
}

func (b *messageBuffer) Pop() (readPair, error) {
	b.mutex.Lock()
	l := len(b.b)
	b.mutex.Unlock()

	if l == 0 {
		select {
		case <-b.ch:
		case <-b.closed:
			return readPair{}, errors.New("closed")
		}
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()
	index := (b.begin + b.size) % len(b.b)
	m := b.b[index]
	b.begin = (b.begin + 1) % len(b.b)
	b.size--
	return m, nil
}

func (b *messageBuffer) Close() {
	select {
	case <-b.closed:
		close(b.closed)
	default:
	}
}

// Message represents an incoming message.
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
		mbuf:   newMessageBuffer(128),
		id:     utils.NewNodeID(utils.GlobalNamespace, key.Digest()),
		config: config,
		Logger: logger,
	}

	return c, nil
}

func (c *Client) parseMessage(rm router.Message) {
	var t struct {
		Type string `msgpack:"type"`
	}
	err := msgpack.Unmarshal(rm.Payload, &t)
	if err != nil {
		return
	}

	u := struct {
		ID string `msgpack:"id"`
	}{}
	err = msgpack.Unmarshal(rm.Payload, &u)
	if err != nil {
		return
	}

	id, err := utils.NewNodeIDFromString(u.ID)
	if err != nil {
		return
	}

	var m Message
	switch t.Type {
	case "chat":
		u := struct {
			Content ChatMessage `msgpack:"content"`
		}{}
		err := msgpack.Unmarshal(rm.Payload, &u)
		if err != nil {
			return
		}
		m = u.Content

	case "ack":
		u := struct {
			Content MessageAck `msgpack:"content"`
		}{}
		err := msgpack.Unmarshal(rm.Payload, &u)
		if err != nil {
			return
		}
		m = u.Content

	case "prof-res":
		u := struct {
			Content UserProfileResponse `msgpack:"content"`
		}{}
		err := msgpack.Unmarshal(rm.Payload, &u)
		if err != nil {
			return
		}
		m = u.Content
		c.Roster.Set(id, u.Content.Profile)

	case "prof-req":
		c.SendProfile(id)

	}
	if m != nil {
		c.mbuf.Push(readPair{M: m, ID: rm.Node})
		c.SendAck(rm.Node, rm.ID)
	}
}

func (c *Client) Read() (Message, utils.NodeID, error) {
	m, err := c.mbuf.Pop()
	return m.M, m.ID, err
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
	c.mbuf.Close()
	c.router.Close()
}

// Sends the given message to the destination node.
func (c *Client) SendMessage(dst utils.NodeID, msg ChatMessage) error {
	t := struct {
		Type    string      `msgpack:"type"`
		Content interface{} `msgpack:"content"`
	}{Type: "chat", Content: msg}

	data, err := msgpack.Marshal(t)
	if err != nil {
		return err
	}

	c.router.SendMessage(dst, data)
	return nil
}

func (c *Client) SendProfile(dst utils.NodeID) error {
	t := struct {
		Type    string      `msgpack:"type"`
		Content interface{} `msgpack:"content"`
	}{Type: "prof-res", Content: c.profile}

	data, err := msgpack.Marshal(t)
	if err != nil {
		return err
	}

	c.router.SendMessage(dst, data)
	return nil
}

func (c *Client) SendProfileRequest(dst utils.NodeID) error {
	t := struct {
		Type    string      `msgpack:"type"`
		Content interface{} `msgpack:"content"`
	}{Type: "prof-req", Content: UserProfileRequest{}}

	data, err := msgpack.Marshal(t)
	if err != nil {
		return err
	}

	c.router.SendMessage(dst, data)
	return nil
}

func (c *Client) SendAck(dst utils.NodeID, id []byte) error {
	t := struct {
		Type    string      `msgpack:"type"`
		Content interface{} `msgpack:"content"`
	}{Type: "ack", Content: MessageAck{ID: id}}

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

func (c *Client) KnownNodes() []utils.NodeInfo {
	return c.router.KnownNodes()
}

type serializable struct {
	Roster Roster           `msgpack:"roster"`
	Nodes  []utils.NodeInfo `msgpack:"nodes"`
}

func (c *Client) MarshalBinary() (data []byte, err error) {
	s := serializable{
		Roster: c.Roster,
		Nodes:  c.router.KnownNodes(),
	}
	return msgpack.Marshal(s)
}

func (c *Client) UnmarshalBinary(data []byte) error {
	var s serializable
	err := msgpack.Unmarshal(data, &s)
	if err != nil {
		return err
	}
	for _, n := range s.Nodes {
		c.router.AddNode(n)
	}
	c.Roster = s.Roster

	for _, id := range c.Roster.List() {
		c.SendProfileRequest(id)
	}

	return nil
}
