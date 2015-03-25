// Package router provides a router for murcott.
package router

import (
	"bytes"
	"crypto/rand"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/h2so5/murcott/dht"
	"github.com/h2so5/murcott/internal"
	"github.com/h2so5/murcott/log"
	"github.com/h2so5/murcott/utils"
	"github.com/h2so5/utp"
)

type Message struct {
	Node    utils.NodeID
	Payload []byte
	ID      []byte
}

type Router struct {
	id       utils.NodeID
	mainDht  *dht.DHT
	groupDht map[utils.NodeID]*dht.DHT
	dhtMutex sync.RWMutex

	listener *utp.Listener
	key      *utils.PrivateKey

	sessions     map[utils.NodeID]*session
	sessionMutex sync.RWMutex

	queuedPackets   []internal.Packet
	receivedPackets map[[20]byte]int

	logger *log.Logger
	recv   chan Message
	send   chan internal.Packet
	exit   chan int
}

func getOpenPortConn(config utils.Config) (*utp.Listener, error) {
	for _, port := range config.Ports() {
		addr, err := utp.ResolveAddr("utp", ":"+strconv.Itoa(port))
		conn, err := utp.Listen("utp", addr)
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("fail to bind port")
}

func NewRouter(key *utils.PrivateKey, logger *log.Logger, config utils.Config) (*Router, error) {
	exit := make(chan int)
	listener, err := getOpenPortConn(config)
	if err != nil {
		return nil, err
	}

	logger.Info("Node ID: %s", key.Digest().String())
	logger.Info("Node Socket: %v", listener.Addr())

	ns := utils.GlobalNamespace
	id := utils.NewNodeID(ns, key.Digest())

	r := Router{
		id:       id,
		listener: listener,
		key:      key,
		sessions: make(map[utils.NodeID]*session),
		mainDht:  dht.NewDHT(10, id, id, listener.RawConn, logger),
		groupDht: make(map[utils.NodeID]*dht.DHT),

		receivedPackets: make(map[[20]byte]int),

		logger: logger,
		recv:   make(chan Message, 100),
		send:   make(chan internal.Packet, 100),
		exit:   exit,
	}

	go r.run()
	return &r, nil
}

func (p *Router) Discover(addrs []net.UDPAddr) {
	p.dhtMutex.RLock()
	defer p.dhtMutex.RUnlock()
	for _, addr := range addrs {
		p.mainDht.Discover(&addr)
		for _, d := range p.groupDht {
			d.Discover(&addr)
		}
		p.logger.Info("Sent discovery packet to %v:%d", addr.IP, addr.Port)
	}
}

func (p *Router) getGroupDht(group utils.NodeID) *dht.DHT {
	p.dhtMutex.RLock()
	defer p.dhtMutex.RUnlock()
	return p.groupDht[group]
}

func (p *Router) Join(group utils.NodeID) error {
	if p.getGroupDht(group) == nil {
		d := dht.NewDHT(10, p.ID(), group, p.listener.RawConn, p.logger)
		for _, n := range p.mainDht.LoadNodes(group.String()) {
			if !n.ID.Match(p.id) {
				d.Discover(n.Addr)
			}
		}
		p.dhtMutex.Lock()
		p.groupDht[group] = d
		p.dhtMutex.Unlock()
		p.mainDht.StoreNodes(group.String(), []utils.NodeInfo{
			utils.NodeInfo{ID: p.id, Addr: p.listener.Addr()},
		})
		return nil
	}
	return errors.New("already joined")
}

func (p *Router) Leave(group utils.NodeID) error {
	if p.getGroupDht(group) != nil {
		p.dhtMutex.Lock()
		delete(p.groupDht, group)
		p.dhtMutex.Unlock()
		return nil
	}
	return errors.New("not joined")
}

func (p *Router) SendMessage(dst utils.NodeID, payload []byte) error {
	pkt, err := p.makePacket(dst, "msg", payload)
	if err != nil {
		return err
	}
	p.send <- pkt
	return nil
}

func (p *Router) SendPing() {
	var list []utils.NodeID

	p.sessionMutex.RLock()
	for id, _ := range p.sessions {
		list = append(list, id)
	}
	p.sessionMutex.RUnlock()

	for _, id := range list {
		pkt, err := p.makePacket(id, "ping", nil)
		if err == nil {
			p.send <- pkt
		}
	}
}

func (p *Router) RecvMessage() (Message, error) {
	if m, ok := <-p.recv; ok {
		return m, nil
	}
	return Message{}, errors.New("Node closed")
}

func (p *Router) run() {
	acceptch := make(chan *session)

	go func() {
		for {
			conn, err := p.listener.Accept()
			if err != nil {
				p.logger.Error("%v", err)
				return
			}
			s, err := newSesion(conn, p.key)
			if err != nil {
				conn.Close()
				p.logger.Error("%v", err)
				continue
			} else {
				go p.readSession(s)
				acceptch <- s
			}
		}
	}()

	go func() {
		var b [102400]byte
		for {
			l, addr, err := p.listener.RawConn.ReadFrom(b[:])
			if err != nil {
				p.logger.Error("%v", err)
				return
			}
			p.dhtMutex.RLock()
			p.mainDht.ProcessPacket(b[:l], addr)
			for _, d := range p.groupDht {
				d.ProcessPacket(b[:l], addr)
			}
			p.dhtMutex.RUnlock()
		}
	}()

	tick := time.NewTicker(time.Second * 1)
	defer tick.Stop()

	for {
		select {
		case s := <-acceptch:
			p.addSession(s)
		case pkt := <-p.send:
			sessions := p.getSessions(pkt.Dst)
			if len(sessions) > 0 {
				for _, s := range sessions {
					err := s.Write(pkt)
					if err != nil {
						p.logger.Error("Remove session(%s): %v", pkt.Dst.String(), err)
						p.removeSession(s)
						p.queuedPackets = append(p.queuedPackets, pkt)
					}
				}
			} else {
				p.logger.Error("Route not found: %v", pkt.Dst)
				p.queuedPackets = append(p.queuedPackets, pkt)
			}
		case <-tick.C:
			p.SendPing()
			var rest []internal.Packet
			for _, pkt := range p.queuedPackets {
				p.dhtMutex.RLock()
				p.mainDht.FindNearestNode(pkt.Dst)
				for _, d := range p.groupDht {
					d.FindNearestNode(pkt.Dst)
				}
				p.dhtMutex.RUnlock()
				sessions := p.getSessions(pkt.Dst)
				if len(sessions) > 0 {
					for _, s := range sessions {
						err := s.Write(pkt)
						if err != nil {
							p.logger.Error("Remove session(%s): %v", pkt.Dst.String(), err)
							p.removeSession(s)
							rest = append(rest, pkt)
						}
					}
				} else {
					p.logger.Error("Route not found: %v", pkt.Dst)
					rest = append(rest, pkt)
				}
			}
			p.queuedPackets = rest
		case <-p.exit:
			return
		}
	}
}

func (p *Router) addSession(s *session) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()
	id := s.ID()
	if _, ok := p.sessions[id]; !ok {
		p.sessions[id] = s
	}
}

func (p *Router) removeSession(s *session) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()
	s.Close()
	delete(p.sessions, s.ID())
}

func (p *Router) readSession(s *session) {
	for {
		pkt, err := s.Read()
		if err != nil {
			p.logger.Error("Remove session(%s): %v", pkt.Dst.String(), err)
			p.removeSession(s)
			return
		}
		if pkt.Src.Match(p.id) {
			continue
		}
		d := pkt.Digest()
		if _, ok := p.receivedPackets[d]; ok {
			continue
		} else {
			p.receivedPackets[d] = 0
		}
		group := bytes.Equal(pkt.Dst.NS[:], utils.GroupNamespace[:])
		if group {
			d := p.getGroupDht(pkt.Dst)
			if d != nil {
				pkt.TTL--
				if pkt.TTL > 0 {
					p.send <- pkt
				}
			} else {
				continue
			}
		}
		if pkt.Type == "msg" && (!group || p.getGroupDht(pkt.Dst) != nil) {
			id, _ := time.Now().MarshalBinary()
			p.recv <- Message{Node: pkt.Src, Payload: pkt.Payload, ID: id}
		}
	}
}

func (p *Router) getSessions(id utils.NodeID) []*session {
	var sessions []*session
	if bytes.Equal(id.NS[:], utils.GlobalNamespace[:]) {
		s := p.getDirectSession(id)
		if s != nil {
			sessions = append(sessions, s)
		}
	} else {
		if d, ok := p.groupDht[id]; ok {
			for _, n := range p.mainDht.LoadNodes(id.String()) {
				if !n.ID.Match(p.id) {
					d.Discover(n.Addr)
				}
			}
			for _, n := range d.FingerNodes() {
				s := p.getDirectSession(n.ID)
				if s != nil {
					sessions = append(sessions, s)
				}
			}
		}
	}
	return sessions
}

func (p *Router) getDirectSession(id utils.NodeID) *session {
	if id.Match(p.id) {
		return nil
	}
	p.sessionMutex.RLock()
	if s, ok := p.sessions[id]; ok {
		p.sessionMutex.RUnlock()
		return s
	}
	p.sessionMutex.RUnlock()

	var info *utils.NodeInfo
	p.dhtMutex.RLock()
	info = p.mainDht.GetNodeInfo(id)
	if info == nil {
		for _, d := range p.groupDht {
			info = d.GetNodeInfo(id)
			if info != nil {
				break
			}
		}
	}
	p.dhtMutex.RUnlock()

	if info == nil {
		return nil
	}

	addr, err := utp.ResolveAddr("utp", info.Addr.String())
	if err != nil {
		p.logger.Error("%v", err)
		return nil
	}

	conn, err := utp.DialUTPTimeout("utp", nil, addr, 100*time.Millisecond)
	if err != nil {
		p.logger.Error("%v %v", addr, err)
		return nil
	}

	s, err := newSesion(conn, p.key)
	if err != nil {
		conn.Close()
		p.logger.Error("%v", err)
		return nil
	} else {
		go p.readSession(s)
		p.addSession(s)
	}

	return s
}

func (p *Router) makePacket(dst utils.NodeID, typ string, payload []byte) (internal.Packet, error) {
	var id [20]byte
	rand.Read(id[:])
	return internal.Packet{
		Dst:     dst,
		Src:     p.id,
		Type:    typ,
		Payload: payload,
		ID:      id,
		TTL:     3,
	}, nil
}

func (p *Router) AddNode(info utils.NodeInfo) {
	p.dhtMutex.RLock()
	defer p.dhtMutex.RUnlock()
	p.mainDht.AddNode(info)
	for _, d := range p.groupDht {
		d.AddNode(info)
	}
}

func (p *Router) DiscoverNode(info utils.NodeInfo) {
	p.dhtMutex.RLock()
	defer p.dhtMutex.RUnlock()
	p.mainDht.DiscoverNode(info)
	for _, d := range p.groupDht {
		d.DiscoverNode(info)
	}
}

func (p *Router) ActiveSessions() []utils.NodeInfo {
	var nodes []utils.NodeInfo
	p.sessionMutex.RLock()
	defer p.sessionMutex.RUnlock()
	for _, n := range p.KnownNodes() {
		if _, ok := p.sessions[n.ID]; ok {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func (p *Router) KnownNodes() []utils.NodeInfo {
	nodes := make(map[utils.NodeID]utils.NodeInfo)
	for _, n := range p.mainDht.KnownNodes() {
		nodes[n.ID] = n
	}
	for _, d := range p.groupDht {
		for _, n := range d.KnownNodes() {
			nodes[n.ID] = n
		}
	}
	list := make([]utils.NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	return list
}

func (p *Router) ID() utils.NodeID {
	return p.id
}

func (p *Router) Close() {
	p.exit <- 0
	p.mainDht.Close()
	for _, d := range p.groupDht {
		d.Close()
	}
}
