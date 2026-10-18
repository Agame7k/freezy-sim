package netsim

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Agame7k/freezy-sim/internal/inspector"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

var interfaceRange = regexp.MustCompile(`(?i)^interface range gigabitethernet\s*1/(\d+)-(\d+)$`)
var interfaceSingle = regexp.MustCompile(`(?i)^interface gigabitethernet\s*1/(\d+)$`)

// SCC emulates an alliance station control cabinet switch reached over SSH. Ports 1/2-1/4 feed driver stations 1-3.
type SCC struct {
	Name     string
	address  string
	username string
	password string
	signer   ssh.Signer

	mu         sync.Mutex
	online     bool
	listener   net.Listener
	bindErr    string
	shutdown   map[int]bool // port -> shut down
	sessions   int
	transcript []string
}

func NewSCC(name, bind string, port int, username, password string) *SCC {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(key)
	return &SCC{Name: name, address: net.JoinHostPort(bind, strconv.Itoa(port)), username: username,
		password: password, signer: signer, online: true, shutdown: map[int]bool{}}
}

func (scc *SCC) start() {
	scc.mu.Lock()
	defer scc.mu.Unlock()
	if scc.listener != nil || !scc.online {
		return
	}
	listener, err := net.Listen("tcp", scc.address)
	if err != nil {
		scc.bindErr = err.Error()
		return
	}
	scc.bindErr = ""
	scc.listener = listener
	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if meta.User() == scc.username && string(password) == scc.password {
				return nil, nil
			}
			scc.mu.Lock()
			scc.log("login rejected for " + meta.User())
			scc.mu.Unlock()
			return nil, fmt.Errorf("access denied")
		},
	}
	config.AddHostKey(scc.signer)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer safe.Recover(nil, "network")
				scc.serve(conn, config)
			}()
		}
	}()
}

func (scc *SCC) stop() {
	scc.mu.Lock()
	defer scc.mu.Unlock()
	if scc.listener != nil {
		_ = scc.listener.Close()
		scc.listener = nil
	}
}

func (scc *SCC) log(line string) {
	scc.transcript = append(scc.transcript, time.Now().Format("15:04:05 ")+line)
	if len(scc.transcript) > 80 {
		scc.transcript = scc.transcript[len(scc.transcript)-80:]
	}
}

func (scc *SCC) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	scc.mu.Lock()
	scc.sessions++
	scc.mu.Unlock()
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions are supported")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		shell := make(chan struct{})
		go func() {
			for request := range channelRequests {
				switch request.Type {
				case "pty-req", "env", "window-change":
					_ = request.Reply(true, nil)
				case "shell":
					_ = request.Reply(true, nil)
					close(shell)
				default:
					_ = request.Reply(false, nil)
				}
			}
		}()
		select {
		case <-shell:
		case <-time.After(5 * time.Second):
			_ = channel.Close()
			continue
		}
		scc.runShell(channel)
	}
}

// runShell interprets IOS-style commands for the port range the arena toggles.
func (scc *SCC) runShell(channel ssh.Channel) {
	defer channel.Close()
	write := func(text string) { _, _ = channel.Write([]byte(text)) }
	reader := bufio.NewReader(channel)
	mode := "exec"
	var selected []int
	prompt := func() {
		switch mode {
		case "exec":
			write("\r\n" + scc.Name + "#")
		case "config":
			write("\r\n" + scc.Name + "(config)#")
		case "config-if":
			write("\r\n" + scc.Name + "(config-if-range)#")
		}
	}
	prompt()
	for {
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return
		}
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line != "" {
			inspector.Note("ssh", "in", scc.Name, mode+"> "+line, nil)
			scc.mu.Lock()
			scc.log(line)
			scc.mu.Unlock()
		}
		switch {
		case line == "":
		case lower == "configure terminal" || lower == "config terminal":
			mode = "config"
		case interfaceRange.MatchString(line):
			match := interfaceRange.FindStringSubmatch(line)
			from, _ := strconv.Atoi(match[1])
			to, _ := strconv.Atoi(match[2])
			selected = nil
			for port := from; port <= to; port++ {
				selected = append(selected, port)
			}
			mode = "config-if"
		case interfaceSingle.MatchString(line):
			port, _ := strconv.Atoi(interfaceSingle.FindStringSubmatch(line)[1])
			selected = []int{port}
			mode = "config-if"
		case lower == "shutdown" || lower == "no shutdown":
			scc.mu.Lock()
			for _, port := range selected {
				scc.shutdown[port] = lower == "shutdown"
			}
			scc.mu.Unlock()
		case lower == "end":
			mode = "exec"
		case lower == "exit":
			switch mode {
			case "config-if":
				mode = "config"
			case "config":
				mode = "exec"
			default:
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
		}
		prompt()
	}
}

// PortUp reports whether a driver station's port (station 0-2 within the alliance → ports 2-4) is enabled.
func (scc *SCC) PortUp(station int) bool {
	scc.mu.Lock()
	defer scc.mu.Unlock()
	return !scc.shutdown[station+2]
}

// SCCState is the published SCC view.
type SCCState struct {
	Name       string   `json:"name"`
	Online     bool     `json:"online"`
	Address    string   `json:"address"`
	BindError  string   `json:"bindError"`
	Sessions   int      `json:"sessions"`
	Ports      [3]bool  `json:"ports"` // DS ports 1/2-1/4 up
	Transcript []string `json:"transcript"`
}

func (scc *SCC) state() SCCState {
	scc.mu.Lock()
	defer scc.mu.Unlock()
	return SCCState{Name: scc.Name, Online: scc.online, Address: scc.address, BindError: scc.bindErr, Sessions: scc.sessions,
		Ports: [3]bool{!scc.shutdown[2], !scc.shutdown[3], !scc.shutdown[4]}, Transcript: append([]string(nil), scc.transcript...)}
}
