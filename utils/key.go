package utils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"reflect"

	"github.com/tv42/base58"
	"gopkg.in/vmihailenco/msgpack.v2"
)

// PublicKey represents an ECDSA public key.
type PublicKey struct {
	x, y *big.Int
}

// PrivateKey represents an ECDSA private key.
type PrivateKey struct {
	PublicKey
	d *big.Int
}

type Signature struct {
	r, s *big.Int
}

func (p PublicKeyDigest) String() string {
	var i big.Int
	i.SetBytes(p[:])
	return string(base58.EncodeBig(nil, &i))
}

// Digest returns a SHA-1 digest for the public key.
func (p *PublicKey) Digest() PublicKeyDigest {
	return sha1.Sum(append(p.x.Bytes(), p.y.Bytes()...))
}

func (p *PublicKey) MarshalText() (text []byte, err error) {
	pub := ecdsa.PublicKey{Curve: elliptic.P256(), X: p.x, Y: p.y}
	x, err := x509.MarshalPKIXPublicKey(&pub)
	if err != nil {
		return nil, err
	}
	b := pem.Block{
		Type:  "DSA PUBLIC KEY",
		Bytes: x,
	}
	return pem.EncodeToMemory(&b), nil
}

func (p *PublicKey) UnmarshalText(text []byte) error {
	for {
		b, r := pem.Decode(text)
		if b != nil {
			if b.Type == "DSA PUBLIC KEY" {
				k, err := x509.ParsePKIXPublicKey(b.Bytes)
				if err != nil {
					return err
				}
				p.x = k.(*ecdsa.PublicKey).X
				p.y = k.(*ecdsa.PublicKey).Y
				return nil
			}
		} else {
			break
		}
		text = r
	}
	return errors.New("Public key block not found")
}

// GeneratePrivateKey generates new ECDSA key pair.
func GeneratePrivateKey() *PrivateKey {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err == nil {
		return &PrivateKey{
			PublicKey: PublicKey{x: key.X, y: key.Y},
			d:         key.D,
		}
	}
	return nil
}

// PrivateKeyFromString generates PrivateKey from the given base58-encoded string.
func PrivateKeyFromString(str string) *PrivateKey {
	b, err := base58.DecodeToBig([]byte(str))
	if err != nil {
		return nil
	}
	var out PrivateKey
	err = msgpack.Unmarshal(b.Bytes(), &out)
	if err != nil {
		return nil
	}
	if !out.verifyKey() {
		return nil
	}
	return &out
}

// String returns the private key as a base58-encoded byte array.
func (p *PrivateKey) String() string {
	data, _ := msgpack.Marshal(p)
	return string(base58.EncodeBig(nil, big.NewInt(0).SetBytes(data)))
}

func (p *PrivateKey) verifyKey() bool {
	data := []byte("test")
	return p.PublicKey.Verify(data, p.Sign(data))
}

func (p *PrivateKey) Sign(data []byte) *Signature {
	key := ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: p.x, Y: p.y},
		D:         p.d,
	}
	hash := sha1.Sum(data)
	r, s, err := ecdsa.Sign(rand.Reader, &key, hash[:])
	if err == nil {
		return &Signature{r: r, s: s}
	}
	return nil
}

func (p *PrivateKey) MarshalText() (text []byte, err error) {
	pri := ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: p.x, Y: p.y},
		D:         p.d,
	}
	x, err := x509.MarshalECPrivateKey(&pri)
	if err != nil {
		return nil, err
	}
	b := pem.Block{
		Type:  "DSA PRIVATE KEY",
		Bytes: x,
	}
	return pem.EncodeToMemory(&b), nil
}

func (p *PrivateKey) UnmarshalText(text []byte) error {
	for {
		b, r := pem.Decode(text)
		if b != nil {
			if b.Type == "DSA PRIVATE KEY" {
				k, err := x509.ParseECPrivateKey(b.Bytes)
				if err != nil {
					return err
				}
				p.d = k.D
				p.PublicKey.x = k.PublicKey.X
				p.PublicKey.y = k.PublicKey.Y
				return nil
			}
		} else {
			break
		}
		text = r
	}
	return errors.New("Private key block not found")
}

func (p *PublicKey) Verify(data []byte, sign *Signature) bool {
	if p.IsZero() {
		return false
	}
	if sign == nil || sign.r == nil || sign.s == nil {
		return false
	}
	if p.x == nil || p.y == nil {
		return false
	}
	key := ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     p.x,
		Y:     p.y,
	}
	hash := sha1.Sum(data)
	return ecdsa.Verify(&key, hash[:], sign.r, sign.s)
}

func (p *PublicKey) IsZero() bool {
	return (p.x == nil || p.y == nil || p.x.Int64() == 0 || p.y.Int64() == 0)
}

func init() {
	msgpack.Register(reflect.TypeOf(Signature{}),
		func(e *msgpack.Encoder, v reflect.Value) error {
			sign := v.Interface().(Signature)
			return e.Encode(map[string][]byte{
				"r": sign.r.Bytes(),
				"s": sign.s.Bytes(),
			})
		},
		func(d *msgpack.Decoder, v reflect.Value) error {
			i, err := d.DecodeMap()
			if err != nil {
				return err
			}
			m := i.(map[interface{}]interface{})
			if r, ok := m["r"].([]byte); ok {
				if s, ok := m["s"].([]byte); ok {
					v.Set(reflect.ValueOf(Signature{
						r: big.NewInt(0).SetBytes(r),
						s: big.NewInt(0).SetBytes(s),
					}))
				}
			}
			return nil
		})

	msgpack.Register(reflect.TypeOf(PrivateKey{}),
		func(e *msgpack.Encoder, v reflect.Value) error {
			sign := v.Interface().(PrivateKey)
			return e.Encode(map[string][]byte{
				"x": sign.x.Bytes(),
				"y": sign.y.Bytes(),
				"d": sign.d.Bytes(),
			})
		},
		func(d *msgpack.Decoder, v reflect.Value) error {
			i, err := d.DecodeMap()
			if err != nil {
				return err
			}
			m := i.(map[interface{}]interface{})
			if x, ok := m["x"].([]byte); ok {
				if y, ok := m["y"].([]byte); ok {
					if d, ok := m["d"].([]byte); ok {
						v.Set(reflect.ValueOf(PrivateKey{
							PublicKey: PublicKey{
								x: big.NewInt(0).SetBytes(x),
								y: big.NewInt(0).SetBytes(y),
							},
							d: big.NewInt(0).SetBytes(d),
						}))
					}
				}
			}
			return nil
		})

	msgpack.Register(reflect.TypeOf(PublicKey{}),
		func(e *msgpack.Encoder, v reflect.Value) error {
			sign := v.Interface().(PublicKey)
			return e.Encode(map[string][]byte{
				"x": sign.x.Bytes(),
				"y": sign.y.Bytes(),
			})
		},
		func(d *msgpack.Decoder, v reflect.Value) error {
			i, err := d.DecodeMap()
			if err != nil {
				return err
			}
			m := i.(map[interface{}]interface{})
			if x, ok := m["x"].([]byte); ok {
				if y, ok := m["y"].([]byte); ok {
					v.Set(reflect.ValueOf(PublicKey{
						x: big.NewInt(0).SetBytes(x),
						y: big.NewInt(0).SetBytes(y),
					}))
				}
			}
			return nil
		})
}
