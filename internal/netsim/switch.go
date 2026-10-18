package netsim

import (
	"bufio"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/inspector"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

var (
	vlanInterface = regexp.MustCompile(`(?i)^interface\s+vlan\s*(\d+)$`)
	ipAddress     = regexp.MustCompile(`(?i)^ip address 10\.(\d+)\.(\d+)\.(\d+) 255\.255\.255\.0$`)
)

var vlanStations = map[int]string{10: "R1", 20: "R2", 30: "R3", 40: "B1", 50: "B2", 60: "B3"}

// Switch emulates the Cisco field switch's telnet CLI closely enough for the arena's configuration sessions.
type Switch struct {
	address  string
	password string

	mu         sync.Mutex
	online     bool
	listener   net.Listener
	bindErr    string
	vlans      map[int]int // vlan -> team number (from the gateway address 10.TE.AM.4)
	sessions   int
	lastConfig time.Time
	transcript []string
}

func NewSwitch(bind string, port int, password string) *Switch {
	return &Switch{address: net.JoinHostPort(bind, strconv.Itoa(port)), password: password, online: true,
		vlans: map[int]int{}}
}

func (sw *Switch) start() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.listener != nil || !sw.online {
		return
	}
	listener, err := net.Listen("tcp", sw.address)
	if err != nil {
		sw.bindErr = err.Error()
		return
	}
	sw.bindErr = ""
	sw.listener = listener
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer safe.Recover(nil, "network")
				sw.serve(conn)
			}()
		}
	}()
}

func (sw *Switch) stop() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.listener != nil {
		_ = sw.listener.Close()
		sw.listener = nil
	}
}

func (sw *Switch) log(line string) {
	sw.transcript = append(sw.transcript, time.Now().Format("15:04:05 ")+line)
	if len(sw.transcript) > 120 {
		sw.transcript = sw.transcript[len(sw.transcript)-120:]
	}
}

// serve runs one telnet session: password, enable + password, then commands until "exit" from exec mode.
func (sw *Switch) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	write := func(text string) { _, _ = conn.Write([]byte(text)) }
	readLine := func() (string, bool) {
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", false
		}
		return strings.TrimSpace(line), true
	}
	sw.mu.Lock()
	sw.sessions++
	sw.mu.Unlock()

	write("\r\nUser Access Verification\r\n\r\nPassword: ")
	if password, ok := readLine(); !ok || password != sw.password {
		write("\r\n% Bad passwords\r\n")
		sw.mu.Lock()
		sw.log("login failed (bad password)")
		sw.mu.Unlock()
		return
	}
	mode := "user"
	currentVlan := 0
	prompt := func() {
		switch mode {
		case "user":
			write("\r\nFieldSwitch>")
		case "exec":
			write("\r\nFieldSwitch#")
		case "config":
			write("\r\nFieldSwitch(config)#")
		case "config-if":
			write("\r\nFieldSwitch(config-if)#")
		case "dhcp":
			write("\r\nFieldSwitch(dhcp-config)#")
		}
	}
	prompt()
	for {
		line, ok := readLine()
		if !ok {
			return
		}
		lower := strings.ToLower(line)
		if line != "" && !(mode == "user" && strings.HasPrefix(lower, "enable")) {
			inspector.Note("telnet", "in", "field switch", mode+"> "+line, nil)
		}
		sw.mu.Lock()
		if mode != "user" && line != "" && !strings.HasPrefix(lower, "terminal") {
			sw.log(line)
		}
		sw.mu.Unlock()
		switch {
		case line == "":
		case lower == "enable" && mode == "user":
			write("\r\nPassword: ")
			if password, ok := readLine(); !ok || password != sw.password {
				write("\r\n% Access denied\r\n")
				continue
			}
			mode = "exec"
		case strings.HasPrefix(lower, "terminal length"):
		case lower == "config terminal" || lower == "configure terminal":
			mode = "config"
			write("\r\nEnter configuration commands, one per line.  End with CNTL/Z.")
		case lower == "end":
			mode = "exec"
			sw.mu.Lock()
			sw.lastConfig = time.Now()
			sw.mu.Unlock()
		case lower == "exit":
			switch mode {
			case "config-if", "dhcp":
				mode = "config"
			case "config":
				mode = "exec"
			default:
				return // logging out closes the session (the arena reads until EOF)
			}
		case vlanInterface.MatchString(line):
			vlan, _ := strconv.Atoi(vlanInterface.FindStringSubmatch(line)[1])
			currentVlan = vlan
			mode = "config-if"
		case lower == "no ip address" && mode == "config-if":
			sw.mu.Lock()
			delete(sw.vlans, currentVlan)
			sw.mu.Unlock()
		case ipAddress.MatchString(line) && mode == "config-if":
			match := ipAddress.FindStringSubmatch(line)
			high, _ := strconv.Atoi(match[1])
			low, _ := strconv.Atoi(match[2])
			sw.mu.Lock()
			sw.vlans[currentVlan] = high*100 + low
			sw.mu.Unlock()
		case strings.HasPrefix(lower, "ip dhcp pool"):
			mode = "dhcp"
		case strings.HasPrefix(lower, "interface ") && (mode == "config" || mode == "dhcp" || mode == "config-if"):
			mode = "config-if"
			currentVlan = 0
		case mode == "config" && strings.HasPrefix(lower, "interface"):
			mode = "config-if"
		default:
			// Accept everything else (dhcp network/router/lease, excluded addresses, no dhcp pool ...).
			if strings.HasPrefix(lower, "no ip dhcp pool") || strings.HasPrefix(lower, "ip dhcp excluded") {
				if mode == "dhcp" {
					mode = "config"
				}
			}
		}
		prompt()
	}
}

// SwitchState is the published switch view.
type SwitchState struct {
	Online     bool           `json:"online"`
	Address    string         `json:"address"`
	BindError  string         `json:"bindError"`
	Sessions   int            `json:"sessions"`
	LastConfig time.Time      `json:"lastConfig"`
	Vlans      map[string]int `json:"vlans"` // station -> team
	Transcript []string       `json:"transcript"`
}

func (sw *Switch) state() SwitchState {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	vlans := map[string]int{}
	for vlan, team := range sw.vlans {
		if station, ok := vlanStations[vlan]; ok {
			vlans[station] = team
		} else {
			vlans[fmt.Sprintf("vlan%d", vlan)] = team
		}
	}
	return SwitchState{Online: sw.online, Address: sw.address, BindError: sw.bindErr, Sessions: sw.sessions,
		LastConfig: sw.lastConfig, Vlans: vlans, Transcript: append([]string(nil), sw.transcript...)}
}

// TeamOnVlan reports which team's subnet a station's VLAN is configured for (0 if none).
func (sw *Switch) TeamOnVlan(station int) int {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.vlans[(station+1)*10]
}
