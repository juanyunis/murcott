package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/h2so5/murcott"
	"github.com/h2so5/murcott/utils"
	"github.com/skratchdot/open-golang/open"
	"github.com/wsxiaoys/terminal/color"
	"gopkg.in/yaml.v2"
)

func main() {
	path := os.Getenv("TANGORPATH")
	if path == "" {
		path = os.Getenv("HOME") + "/.tangor"
	}

	config := getConfig(path)

	keyfile := flag.String("i", path+"/id_dsa", "Identity file")
	bootstrap := flag.String("b", "", "Additional bootstrap node")
	web := flag.Bool("web", false, "Open web browser")
	flag.Parse()

	color.Print("\n@{Gk} @{Yk}  tangor  @{Gk} @{|}\n\n")

	if len(*bootstrap) > 0 {
		config.B = append(config.B, *bootstrap)
	}

	key, err := getKey(*keyfile)
	if err != nil {
		color.Printf(" -> @{Rk}ERROR:@{|} %v\n", err)
		os.Exit(-1)
	}

	id := utils.NewNodeID(utils.GlobalNamespace, key.Digest())
	color.Printf("Your ID: @{Wk} %s @{|}\n\n", id.String())

	client, err := murcott.NewClient(key, config)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	filename := filepath.Join(path, id.Digest.String()+".dat")
	data, err := ioutil.ReadFile(filename)
	if err == nil {
		client.UnmarshalBinary(data)
	}

	if *web {
		go webui()
		open.Run("http://localhost:3000")
	}

	exit := make(chan int)
	s := Session{cli: client}

	go func() {
		for {
			select {
			case <-exit:
				return
			case <-time.After(time.Minute):
				s.save(filename)
			}
		}
	}()

	s.bootstrap()
	s.commandLoop()
	s.save(filename)
	close(exit)
}

func getConfig(path string) utils.Config {
	config := utils.DefaultConfig
	filename := path + "/config.yml"
	data, err := ioutil.ReadFile(filename)
	if err == nil {
		yaml.Unmarshal(data, &config)
	} else {
		data, err := yaml.Marshal(config)
		if err == nil {
			ioutil.WriteFile(filename, data, 0644)
		}
	}
	return config
}

func getKey(keyfile string) (*utils.PrivateKey, error) {
	_, err := os.Stat(filepath.Dir(keyfile))

	if _, err := os.Stat(keyfile); err != nil {
		err := os.MkdirAll(filepath.Dir(keyfile), 0755)
		if err != nil {
			return nil, err
		}
		key := utils.GeneratePrivateKey()
		pem, err := key.MarshalText()
		err = ioutil.WriteFile(keyfile, pem, 0644)
		if err != nil {
			return nil, err
		}
		fmt.Printf(" -> Create a new private key: %s\n", keyfile)
	}

	pem, err := ioutil.ReadFile(keyfile)
	if err != nil {
		return nil, err
	}

	var key utils.PrivateKey
	err = key.UnmarshalText(pem)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

type Session struct {
	cli *murcott.Client
}

func (s *Session) bootstrap() {
	fmt.Println(" -> Searching bootstrap nodes")
	go func() {
		s.cli.Run()
	}()

	var nodes int
	for i := 0; i < 5; i++ {
		nodes = len(s.cli.KnownNodes())
		fmt.Printf(" -> Found %d nodes", nodes)
		time.Sleep(200 * time.Millisecond)
		fmt.Printf("\r")
	}
	fmt.Println()

	if nodes == 0 {
		color.Printf(" -> @{Yk}WARNING:@{|} node not found\n")
	}
	fmt.Println()
}

func (s *Session) save(filename string) error {
	data, err := s.cli.MarshalBinary()
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filename, data, 0755)
}

func (s *Session) commandLoop() {
	var chatID *utils.NodeID

	go func() {
		for {
			m, src, err := s.cli.Read()
			if err != nil {
				return
			}
			if msg, ok := m.(murcott.ChatMessage); ok {
				/*
					if chatID == nil {
						chatID = &src
						color.Printf("\n -> Start a chat with @{Wk} %s @{|}\n\n", src.String())
					}
				*/
				str := src.String()
				color.Printf("\r* @{Wk}%s@{|} %s\n", str[len(str)-8:], msg.Text())
				fmt.Print("* ")
			}
		}
	}()

	bio := bufio.NewReader(os.Stdin)
	for {
		if chatID == nil {
			fmt.Print("> ")
		} else {
			fmt.Print("* ")
		}
		line, _, err := bio.ReadLine()
		if err != nil {
			return
		}
		c := strings.Split(string(line), " ")
		if len(c) == 0 || c[0] == "" {
			continue
		}
		switch c[0] {
		case "/chat":
			if len(c) != 2 {
				color.Printf(" -> @{Rk}ERROR:@{|} /chat takes 1 argument\n")
			} else {
				nid, err := utils.NewNodeIDFromString(c[1])
				if err != nil {
					color.Printf(" -> @{Rk}ERROR:@{|} invalid ID\n")
				} else {
					s.cli.Join(nid)
					chatID = &nid
					color.Printf(" -> Start a chat with @{Wk} %s @{|}\n\n", nid.String())
				}
			}
		case "/add":
			if len(c) != 2 {
				color.Printf(" -> @{Rk}ERROR:@{|} /add takes 1 argument\n")
			} else {
				nid, err := utils.NewNodeIDFromString(c[1])
				if err != nil {
					color.Printf(" -> @{Rk}ERROR:@{|} invalid ID\n")
				} else {
					s.cli.Roster.Set(nid, murcott.UserProfile{})
				}
			}
		case "/mkg":
			key := utils.GeneratePrivateKey()
			id := utils.NewNodeID(utils.GroupNamespace, key.Digest())
			color.Printf("Group ID: @{Wk} %s @{|}\n\n", id.String())

		case "/stat":
			color.Printf("  * NumGoroutine() = %d\n", runtime.NumGoroutine())
			nodes := s.cli.ActiveSessions()
			color.Printf("  * active sessions (%d) *\n", len(nodes))
			for _, n := range nodes {
				color.Printf(" %v\n", n)
			}
			list := s.cli.Roster.List()
			color.Printf("  * Roster (%d) *\n", len(list))
			for _, n := range list {
				color.Printf(" %v %s \n", n, s.cli.Roster.Get(n).Nickname)
			}

		case "/end":
			if chatID != nil {
				color.Printf(" -> End current chat\n")
				s.cli.Leave(*chatID)
				chatID = nil
			}
		case "/exit", "/quit":
			color.Printf(" -> See you@{Kg}.@{Kr}.@{Ky}.@{|}\n")
			return
		case "/help":
			showHelp()
		default:
			if chatID == nil {
				color.Printf(" -> @{Rk}ERROR:@{|} unknown command\n")
				showHelp()
			} else {
				s.cli.SendMessage(*chatID, murcott.NewPlainChatMessage(string(line)))
			}
		}
	}
}

func showHelp() {
	fmt.Println()
	color.Printf(
		`  * HELP *
 @{Kg}/chat [ID]@{|}	Start a chat with [ID]
 @{Kg}/end      @{|}	End current chat
 @{Kg}/add  [ID]@{|}	Add [ID] to roster
 @{Kg}/mkg      @{|}	Generate new group id
 @{Kg}/help     @{|}	Show this message
 @{Kg}/stat     @{|}	Show node status
 @{Kg}/exit     @{|}	Exit this program`)
	fmt.Println()
}
