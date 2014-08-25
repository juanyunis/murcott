package murcott

import (
	"encoding/base64"
	"image"
	"reflect"
	"strings"
	"testing"
)

func TestClientMessage(t *testing.T) {
	key1 := GeneratePrivateKey()
	key2 := GeneratePrivateKey()
	client1 := NewClient(key1, NewStorage(":memory:"))
	client2 := NewClient(key2, NewStorage(":memory:"))

	success := make(chan bool)
	plainmsg := NewPlainChatMessage("Hello")

	client2.HandleMessages(func(src NodeId, msg ChatMessage) {
		if src.cmp(key1.PublicKeyHash()) == 0 {
			if msg.Text() == plainmsg.Text() {
				success <- true
			} else {
				t.Errorf("wrong message body")
				success <- false
			}
		} else {
			t.Errorf("wrong source id")
			success <- false
		}
	})

	client1.SendMessage(key2.PublicKeyHash(), plainmsg, func(ok bool) {
		if ok {
			success <- true
		} else {
			t.Errorf("SendMessage() timed out")
			success <- false
		}
	})

	go client1.Run()
	go client2.Run()

	for i := 0; i < 2; i++ {
		if !<-success {
			return
		}
	}

	client1.Close()
	client2.Close()
}

func TestClientProfile(t *testing.T) {
	const data = `iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAADw0lEQVRIx+1WXU
xTZxjuhTe7UTMSMy6mA5wttJRz+p2/7/yXnpYON3SAP9MRiBFZwtgWxcRE1Bm3sC0jcz+GLLohm8sM
m1Nh6gLqyMAoVXRQKFBByk8pLQWENep2tVe2W7eWo1db8uUkJznned7vfd7nfV/DGsr2RI/hv0dgpG
zptC2LZzI52rTw+ngI/sbFDLaLqiYTmSaCsCCBNdG6CQAaUAjMyA5JwcielpxvWXmgcseb5cUkmQn3
0EsA6IhnsmX2JUtKVdkrDfUf3bzVPDJxa3/lDtKakcWzegmsHC1L7Gsbc6+0nxmb7IrM+e/+PnK55V
styaA4Fb0pgvzwdlF5bkXDV4dnHwQmpn1wHoZfttkhUCTP6hU5g0GqQyoSrN3eS+E5/1jYG40NNp+r
z01NgvBBdl0E8DMSOcVmrqmqCE37gtO+0Gx/IHhz77bCHIXlVQHbJUrkLAwy0WgxBJBfSZPdKUkXL5
yAwMci3qnYUHNjXTE2b1jnstNW9JRBQRZJ5c3MoggsLKUq+I1C1+Bwx+RsP4Q/PHqjZournEl9fa1Y
XVnadPqLr+sP06Y0SsLGRAngB1YRVNOq458disz7x6d6wvO3rzR9+V31rraLJ339v4DakbsD71VVYN
JsE7mECdJppGhyoTXF0/Hj1G+DwWjvRLQ3MNYJV4FEwTMaGzrbUOtYZlAX1E4sRRAOwTMqR75d/upo
qCsY9QFBaKYvEhuCRIEY4bkBuESZi3NqkpWjjFTiBHy2JCUvbfrhKNgqMn8b/DUe7u7ruXxnuAM4Jm
f6jry7WyNMWBX+2WuGR9QPYiT8Ik9e85zv7vn5QmPd8U8Pfryz5MhWzT/QNnM/0Npyct3zKxwuNYNG
xkX4wAiNk6VynFJxgTvftsa56un1OKuEWHnq2AfT9+4MjXj2lby81sHr6qbgYYKjEIukbAnUXp/nKr
Wjrq6WmQeBU7XvbF1iyHMr0AQXT/BXlzazlBXcoMk5ltRP9ldMxQZ/vdb4+fa8b46+X1qUT1AEfKC3
FwkyLnCKezY4Oz3noF79/vZQtPcSaCAhTsbpj/ZwXASUzFueTT5WWx2MeKFMxyM9oHBb6/cFacsddp
7AtN5uSgock2n68MBbkwvoYK6r7WeK0OoXXHI8Cv87AfgZBNhIpHZ2/jT3x+h1z/ntKunWRGii8aDH
NXA4VWCfWX6irsbray11sm47TGccJ3pcI9PMULzAbMrTtuSqDpHmFtCNj3EvAtPBWEaYonkoKTY9Ef
R496KHSxGDMljK+P9u+gTOnzzpHBoBGFBEAAAAAElFTkSuQmCC`

	key1 := GeneratePrivateKey()
	key2 := GeneratePrivateKey()
	client1 := NewClient(key1, NewStorage(":memory:"))
	client2 := NewClient(key2, NewStorage(":memory:"))

	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))
	img, _, err := image.Decode(reader)
	if err != nil {
		t.Errorf("fail to decode image: %v", err)
		return
	}

	profile := UserProfile{
		Nickname: "nick",
		Avatar:   UserAvatar{Image: img},
		Extension: map[string]string{
			"Location": "Tokyo",
		},
	}
	client2.SetProfile(profile)

	success := make(chan bool)

	go client1.Run()
	go client2.Run()

	client1.RequestProfile(key2.PublicKeyHash(), func(p *UserProfile) {
		if p.Nickname != profile.Nickname {
			t.Errorf("wrong Nickname: %s; expects %s", p.Nickname, profile.Nickname)
			success <- false
		}
		if !reflect.DeepEqual(p.Extension, profile.Extension) {
			t.Errorf("wrong Extension: %v; expects %v", p.Extension, profile.Extension)
			success <- false
		}
	loop:
		for x := 0; x < p.Avatar.Image.Bounds().Max.X; x++ {
			for y := 0; y < p.Avatar.Image.Bounds().Max.Y; y++ {
				r1, g1, b1, a1 := p.Avatar.Image.At(x, y).RGBA()
				r2, g2, b2, a2 := profile.Avatar.Image.At(x, y).RGBA()
				if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
					t.Errorf("avatar image color mismatch at (%d, %d)", x, y)
					success <- false
					break loop
				}
			}
		}
		success <- true
	})

	if !<-success {
		return
	}

	client1.Close()
	client2.Close()
}
