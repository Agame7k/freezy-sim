package ds

import (
	"math"
	"time"
)

// Robot models the electrical and communications side of a robot: radio, roboRIO, user code and battery.
type Robot struct {
	Powered     bool    `json:"powered"`
	Radio       bool    `json:"radio"`
	Rio         bool    `json:"rio"`
	Code        bool    `json:"code"`
	BatteryFull float64 `json:"batteryFull"` // open-circuit voltage of a fully charged battery
	Resistance  float64 `json:"resistance"`  // battery + wiring internal resistance (ohms)
	CapacityAh  float64 `json:"capacityAh"`

	SoC          float64 `json:"soc"`
	LoadAmps     float64 `json:"loadAmps"`
	Voltage      float64 `json:"voltage"`
	Brownout     bool    `json:"brownout"`
	EStopLatched bool    `json:"eStopLatched"`

	radioBootUntil time.Time
	rioBootUntil   time.Time
	codeBootUntil  time.Time
	externalLoad   float64
	externalLoadAt time.Time
}

const (
	brownoutVolts        = 6.8
	brownoutRecoverVolts = 7.5
	rioResetVolts        = 4.5
	idleAmps             = 1.8
)

// Boot durations (shortened from real hardware so tests stay quick).
var (
	RadioBootTime = 20 * time.Second
	RioBootTime   = 12 * time.Second
	CodeBootTime  = 3 * time.Second
)

func newRobot() Robot {
	return Robot{
		Powered: true, Radio: true, Rio: true, Code: true,
		BatteryFull: 12.9, Resistance: 0.018, CapacityAh: 18, SoC: 1, Voltage: 12.9,
	}
}

// RadioReachable reports whether the radio is up and linked.
func (robot *Robot) RadioReachable(now time.Time) bool {
	return robot.Powered && robot.Radio && now.After(robot.radioBootUntil)
}

// RioReachable reports whether the roboRIO answers through the radio.
func (robot *Robot) RioReachable(now time.Time) bool {
	return robot.RadioReachable(now) && robot.Rio && now.After(robot.rioBootUntil)
}

// CodeRunning reports whether user robot code is running.
func (robot *Robot) CodeRunning(now time.Time) bool {
	return robot.Powered && robot.Rio && now.After(robot.rioBootUntil) && robot.Code && now.After(robot.codeBootUntil)
}

// RadioBootRemaining returns seconds until the radio finishes booting.
func (robot *Robot) RadioBootRemaining(now time.Time) float64 {
	return math.Max(0, robot.radioBootUntil.Sub(now).Seconds())
}

// RioBootRemaining returns seconds until the roboRIO finishes booting.
func (robot *Robot) RioBootRemaining(now time.Time) float64 {
	return math.Max(0, robot.rioBootUntil.Sub(now).Seconds())
}

// RebootRio restarts the roboRIO (clears an e-stop latch, restarts code).
func (robot *Robot) RebootRio(now time.Time) {
	robot.rioBootUntil = now.Add(RioBootTime)
	robot.codeBootUntil = robot.rioBootUntil.Add(CodeBootTime)
	robot.EStopLatched = false
	robot.Brownout = false
}

// RebootRadio power-cycles the radio.
func (robot *Robot) RebootRadio(now time.Time) {
	robot.radioBootUntil = now.Add(RadioBootTime)
}

// RestartCode restarts the user program.
func (robot *Robot) RestartCode(now time.Time) {
	robot.Code = true
	robot.codeBootUntil = now.Add(CodeBootTime)
}

// PowerCycle turns the robot off and back on (everything reboots, fresh state).
func (robot *Robot) PowerCycle(now time.Time) {
	robot.Powered = true
	robot.RebootRadio(now)
	robot.RebootRio(now)
}

// FreshBattery swaps in a fully charged battery.
func (robot *Robot) FreshBattery() {
	robot.SoC = 1
	robot.Brownout = false
}

// openCircuit is the battery's resting voltage at its state of charge.
func (robot *Robot) openCircuit() float64 {
	return robot.BatteryFull - 1.1*(1-robot.SoC)
}

// SetExternalLoad lets the physics simulation report the instantaneous motor current draw.
func (robot *Robot) SetExternalLoad(amps float64, now time.Time) {
	robot.externalLoad = amps
	robot.externalLoadAt = now
}

// Step advances the battery model. enabled reports whether motors may draw power.
func (robot *Robot) Step(dt float64, now time.Time, enabled bool) {
	if !robot.Powered {
		robot.LoadAmps = 0
		robot.Voltage = 0
		return
	}
	load := idleAmps
	if enabled && !robot.Brownout {
		if now.Sub(robot.externalLoadAt) < 500*time.Millisecond {
			load += robot.externalLoad
		} else {
			// Without a physics model attached, approximate a driving robot's draw.
			t := float64(now.UnixNano()) / 1e9
			load += 22 + 14*math.Sin(t*1.7) + 9*math.Sin(t*5.3+1)
		}
	}
	robot.LoadAmps = load

	robot.SoC = math.Max(0, robot.SoC-load*dt/3600/robot.CapacityAh)
	target := robot.openCircuit() - load*robot.Resistance
	// First-order filter (~60 ms) so the voltage sags smoothly.
	alpha := math.Min(1, dt/0.06)
	if robot.Voltage == 0 {
		robot.Voltage = target
	}
	robot.Voltage += (target - robot.Voltage) * alpha

	if robot.Voltage < brownoutVolts {
		robot.Brownout = true
	} else if robot.Brownout && robot.Voltage > brownoutRecoverVolts {
		robot.Brownout = false
	}
	if robot.Voltage < rioResetVolts && now.After(robot.rioBootUntil) {
		robot.RebootRio(now)
	}
}
