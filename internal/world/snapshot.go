package world

import (
	"encoding/binary"
	"math"
)

// Snapshot wire format (little-endian), sent at 60 Hz on /ws/world:
//
//	"FSW1" | u32 frame | f32 simTime | u8 robotCount | u8 flags(bit0 paused, bit1 FUEL ids) | u16 fuelCount
//	robots × 40 bytes: u8 index | u8 flags | u16 team | f32 x y z heading pitch roll | u8 hopper | u8 climbLevel
//	                   | u8 archetype | u8 aiState | f32 climbProgress (0..3)
//	fuel × 8 bytes:     i16 x mm | i16 y mm | i16 z mm | u16 id  (balls on/over the field, inside the hubs and out on the venue floor;
//	                    the id is the same ball from frame to frame, so the view can roll it. Frames recorded without
//	                    flag bit1 have 6-byte records and no id.)
//	chute counts:       u8 red | u8 blue, then u8 doorFlags (bit0 red open, bit1 blue open)
//	hub stored:         u16 red | u16 blue
//	corral counts:      u8 red | u8 blue, then u8 throwFlags (bit0 red human player threw in the last 0.5 s, bit1 blue)
//	field reset crew:   u8 count, then per volunteer i16 x mm | i16 y mm | u8 mode | u8 bucket
//	hopper contents:   per robot (in order) u8 count, then per FUEL i16 x | i16 y | i16 z mm in the robot frame
//	                   (x forward, y left, z up; FUEL inside the hopper, simulated in hopper.go)
//	robot byte 36 is the cart state (0 on the field, 1 wheeling off, 2 off, 3 wheeling on)
//
// robot flags: 1 present, 2 enabled, 4 intake, 8 shooting, 16 climbing, 32 manual, 64 brownout, 128 auto.
const robotRecordSize = 40

const fuelRecordSize = 8

var aiStateCodes = map[string]byte{
	"": 0, "idle": 1, "collect": 2, "return": 3, "shoot": 4, "climb": 5, "defend": 6, "feed": 7, "hold": 8,
	"auto": 9, "park": 10,
}

func (world *World) encodeSnapshot() []byte {
	world.frame++
	visible := 0
	for _, ball := range world.balls {
		if ball.State == BallFree || ball.State == BallHub || (ball.State == BallHeld && ball.intakeT < 1) || ball.offField(world.time) {
			visible++ // FUEL inside a hub shows through its clear hopper and lower panel
		}
	}
	crew := world.volunteers()
	contents := make([][][3]float64, len(world.robots))
	heldBytes := 0
	for i, robot := range world.robots {
		contents[i] = world.hopperContents(robot)
		if len(contents[i]) > 255 {
			contents[i] = contents[i][:255]
		}
		heldBytes += 1 + len(contents[i])*6
	}
	size := 16 + len(world.robots)*robotRecordSize + visible*fuelRecordSize + 3 + 4 + 3 + 1 + len(crew)*6 + heldBytes
	buffer := make([]byte, size)
	copy(buffer[0:4], "FSW1")
	binary.LittleEndian.PutUint32(buffer[4:8], world.frame)
	binary.LittleEndian.PutUint32(buffer[8:12], math.Float32bits(float32(world.time)))
	buffer[12] = byte(len(world.robots))
	if world.paused {
		buffer[13] |= 1
	}
	buffer[13] |= 2 // FUEL records carry ids
	binary.LittleEndian.PutUint16(buffer[14:16], uint16(visible))
	offset := 16
	for _, robot := range world.robots {
		var flags byte
		if robot.Present && robot.Cart != CartOff {
			flags |= 1
		}
		if robot.Enabled {
			flags |= 2
		}
		if robot.IntakeOn {
			flags |= 4
		}
		if robot.Shooting || (robot.Ctl.Shoot && robot.Enabled) {
			flags |= 8
		}
		if robot.Climbing {
			flags |= 16
		}
		if robot.Manual {
			flags |= 32
		}
		if robot.Brownout {
			flags |= 64
		}
		if robot.Auto {
			flags |= 128
		}
		record := buffer[offset : offset+robotRecordSize]
		record[0] = byte(robot.Index)
		record[1] = flags
		binary.LittleEndian.PutUint16(record[2:4], uint16(robot.Team))
		putFloat := func(at int, value float64) {
			binary.LittleEndian.PutUint32(record[at:at+4], math.Float32bits(float32(value)))
		}
		putFloat(4, robot.Pos.X)
		putFloat(8, robot.Pos.Y)
		putFloat(12, robot.Z)
		putFloat(16, robot.Heading)
		putFloat(20, robot.Pitch)
		putFloat(24, robot.Roll)
		record[28] = byte(clamp(float64(world.packedHopper(robot)), 0, 255)) // FUEL still on its way in is drawn on its own
		record[29] = byte(robot.ClimbLevel)
		record[30] = byte(robot.Arch.Id)
		record[31] = aiStateCodes[robot.AiState]
		putFloat(32, robot.ClimbProg)
		record[36] = byte(robot.Cart)
		offset += robotRecordSize
	}
	for id, ball := range world.balls {
		if ball.State != BallFree && ball.State != BallHub && (ball.State != BallHeld || ball.intakeT >= 1) && !ball.offField(world.time) {
			continue
		}
		for axis := 0; axis < 3; axis++ {
			millimeters := clamp(ball.P[axis]*1000, -32000, 32000)
			binary.LittleEndian.PutUint16(buffer[offset:offset+2], uint16(int16(millimeters)))
			offset += 2
		}
		binary.LittleEndian.PutUint16(buffer[offset:offset+2], uint16(id))
		offset += 2
	}
	buffer[offset] = byte(world.outposts[Red].chuteCount(world))
	buffer[offset+1] = byte(world.outposts[Blue].chuteCount(world))
	var doors byte
	if world.outposts[Red].doorOpen(world) {
		doors |= 1
	}
	if world.outposts[Blue].doorOpen(world) {
		doors |= 2
	}
	buffer[offset+2] = doors
	offset += 3
	binary.LittleEndian.PutUint16(buffer[offset:offset+2], uint16(world.hubs[Red].Stored))
	binary.LittleEndian.PutUint16(buffer[offset+2:offset+4], uint16(world.hubs[Blue].Stored))
	offset += 4
	var throws byte
	for alliance, outpost := range world.outposts {
		buffer[offset+alliance] = byte(min(255, outpost.Corral))
		if world.time-outpost.lastThrow < 0.5 {
			throws |= 1 << alliance
		}
	}
	buffer[offset+2] = throws
	offset += 3
	// The field reset crew: count, then per volunteer i16 x mm | i16 y mm | u8 mode | u8 bucket.
	buffer[offset] = byte(len(crew))
	offset++
	for _, v := range crew {
		binary.LittleEndian.PutUint16(buffer[offset:offset+2], uint16(int16(clamp(v.Pos.X*1000, -32000, 32000))))
		binary.LittleEndian.PutUint16(buffer[offset+2:offset+4], uint16(int16(clamp(v.Pos.Y*1000, -32000, 32000))))
		buffer[offset+4] = byte(v.Mode)
		buffer[offset+5] = byte(min(v.Bucket, 255))
		offset += 6
	}
	for _, held := range contents {
		buffer[offset] = byte(len(held))
		offset++
		for _, p := range held {
			for axis := 0; axis < 3; axis++ {
				binary.LittleEndian.PutUint16(buffer[offset:offset+2], uint16(int16(clamp(p[axis]*1000, -32000, 32000))))
				offset += 2
			}
		}
	}
	return buffer
}

// Subscribe implements gui.BinaryStream.
func (world *World) Subscribe() (<-chan []byte, func()) {
	channel := make(chan []byte, 4)
	world.subsMu.Lock()
	world.subs[channel] = struct{}{}
	world.subsMu.Unlock()
	cancelled := false
	return channel, func() {
		world.subsMu.Lock()
		defer world.subsMu.Unlock()
		if !cancelled {
			cancelled = true
			delete(world.subs, channel)
			close(channel)
		}
	}
}

func (world *World) broadcast(frame []byte) {
	world.subsMu.Lock()
	defer world.subsMu.Unlock()
	for channel := range world.subs {
		select {
		case channel <- frame:
		default:
			// Slow viewer: drop this frame rather than stall the simulation.
		}
	}
}
