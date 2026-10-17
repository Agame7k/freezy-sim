package app

import (
	"fmt"
	"net"
	"strconv"
)

// PortProblem is an emulator address another program is already using.
type PortProblem struct {
	Device  string `json:"device"`
	Address string `json:"address"`
	Error   string `json:"error"`
	Fix     string `json:"fix"`
}

// preflightPorts tries every address the emulated field hardware needs before the emulators bind them, so a port
// conflict is reported once, clearly, with what to do about it (instead of a device that silently never answers).
func (app *App) preflightPorts() []PortProblem {
	cfg := app.Config
	type check struct {
		device, network, address, fix string
	}
	host := func(ip string, port int) string { return net.JoinHostPort(ip, strconv.Itoa(port)) }
	checks := []check{
		{"PLC (Modbus)", "tcp", host(cfg.PLC.Bind, cfg.PLC.Port),
			"another PLC simulator or Modbus server is running; close it or change plc.bind in freezy-sim.json"},
		{"sACN LED receiver", "udp", host(cfg.SACN.Bind, cfg.SACN.Port),
			"another sACN/E1.31 program is listening; close it or change sacn.bind"},
	}
	for _, box := range []struct{ name, ip string }{
		{"Scoring table E-stop box", cfg.AltIO.ScoreTableIP},
		{"Red alliance E-stop box", cfg.AltIO.RedIP},
		{"Blue alliance E-stop box", cfg.AltIO.BlueIP},
	} {
		checks = append(checks, check{box.name + " (ESP32)", "tcp", host(box.ip, cfg.AltIO.HealthPort),
			"something is serving HTTP on that address; change altio IPs in freezy-sim.json"})
	}
	if cfg.Field.NetworkSecurity {
		checks = append(checks,
			check{"Access point", "tcp", host(cfg.AP.Bind, cfg.AP.Port), "change ap.bind/ap.port in freezy-sim.json"},
			check{"Field switch (telnet)", "tcp", host(cfg.Switch.Bind, cfg.Switch.Port),
				"a telnet server is using that address; change switch.bind"},
			check{"Red SCC (SSH)", "tcp", host(cfg.SCC.RedBind, cfg.SCC.Port),
				"an SSH server is listening on all addresses; stop it or change scc.redBind"},
			check{"Blue SCC (SSH)", "tcp", host(cfg.SCC.BlueBind, cfg.SCC.Port),
				"an SSH server is listening on all addresses; stop it or change scc.blueBind"},
		)
	}
	var problems []PortProblem
	for _, c := range checks {
		var err error
		if c.network == "udp" {
			var addr *net.UDPAddr
			if addr, err = net.ResolveUDPAddr("udp4", c.address); err == nil {
				var conn *net.UDPConn
				if conn, err = net.ListenUDP("udp4", addr); err == nil {
					_ = conn.Close()
				}
			}
		} else {
			var listener net.Listener
			if listener, err = net.Listen("tcp", c.address); err == nil {
				_ = listener.Close()
			}
		}
		if err != nil {
			problems = append(problems, PortProblem{Device: c.device, Address: c.address, Error: err.Error(), Fix: c.fix})
			app.Hub.Errorf("setup", "%s cannot use %s: %v. Fix: %s", c.device, c.address, err, c.fix)
		}
	}
	if problems == nil {
		problems = []PortProblem{}
	}
	app.Hub.Publish("sim.ports", problems)
	if len(problems) > 0 {
		app.Hub.Warnf("setup", "%d emulated device(s) could not claim their address; see the Arena panel", len(problems))
	}
	return problems
}

func (problem PortProblem) String() string {
	return fmt.Sprintf("%s %s: %s", problem.Device, problem.Address, problem.Error)
}
