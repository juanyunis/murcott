// Package internal provides internal classes for murcott.
package internal

import (
	"crypto/sha1"
	"errors"

	"github.com/h2so5/murcott/utils"
	"gopkg.in/vmihailenco/msgpack.v2"
)

type Packet struct {
	Dst     utils.NodeID    `msgpack:"dst"`
	Src     utils.NodeID    `msgpack:"src"`
	Type    string          `msgpack:"type"`
	Payload []byte          `msgpack:"payload"`
	ID      [20]byte        `msgpack:"id"`
	S       utils.Signature `msgpack:"sign"`
	TTL     uint8           `msgpack:"ttl"`
}

func (p *Packet) Serialize() []byte {
	ary := []interface{}{
		p.Dst.Bytes(),
		p.Src.Bytes(),
		p.Type,
		p.Payload,
		p.ID,
	}

	data, _ := msgpack.Marshal(ary)
	return data
}

func (p *Packet) Digest() [20]byte {
	return sha1.Sum(p.Serialize())
}

func (p *Packet) Sign(key *utils.PrivateKey) error {
	sign := key.Sign(p.Serialize())
	if sign == nil {
		return errors.New("cannot sign packet")
	}
	p.S = *sign
	return nil
}

func (p *Packet) Verify(key *utils.PublicKey) bool {
	return key.Verify(p.Serialize(), &p.S)
}
