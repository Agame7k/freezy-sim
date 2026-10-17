// Binary world stream (/ws/world): robot poses and every FUEL from the Go physics, applied to the 3D scene.
import * as THREE from "three";
import { f2s } from "./util.js";
import { createRobot } from "./robots.js";
import { on } from "../core/store.js";

const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];
const ARCHETYPE_NAMES = ["scorer", "tall", "volley", "defender", "turret", "bigdumper", "shuttle", "kitbot"];
const ROBOT_RECORD = 40;

export function connectWorldStream(venue) {
  const state = { connected: false, frames: 0, lastFrame: null, previous: null, receivedAt: 0, interval: 1000 / 60, fuelPositions: new Float32Array(0) };
  venue.world = state;
  // Team-built robots: each station's design (its parts) from the 10 Hz world summary; the stream only carries the
  // nearest stock design.
  const designs = new Map();

  function connect() {
    const socket = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/world`);
    socket.binaryType = "arraybuffer";
    socket.onopen = () => { state.connected = true; };
    socket.onclose = () => {
      state.connected = false;
      setTimeout(connect, 1000);
    };
    socket.onmessage = (event) => {
      const frame = decode(event.data);
      if (!frame) return;
      state.previous = state.lastFrame;
      state.lastFrame = frame;
      const now = performance.now();
      // Live frames come at 60 Hz, replayed ones at 20 Hz: interpolate over the measured spacing.
      if (state.receivedAt) state.interval += (Math.min(250, Math.max(8, now - state.receivedAt)) - state.interval) * 0.2;
      state.receivedAt = now;
      state.frames++;
      applyFuel(frame);
      applyStatic(frame);
    };
  }

  function decode(buffer) {
    const view = new DataView(buffer);
    if (view.byteLength < 16 || String.fromCharCode(view.getUint8(0), view.getUint8(1), view.getUint8(2), view.getUint8(3)) !== "FSW1") return null;
    const frame = {
      seq: view.getUint32(4, true),
      time: view.getFloat32(8, true),
      paused: (view.getUint8(13) & 1) !== 0,
      robots: [],
    };
    const robotCount = view.getUint8(12);
    const fuelCount = view.getUint16(14, true);
    let offset = 16;
    for (let i = 0; i < robotCount; i++) {
      const flags = view.getUint8(offset + 1);
      frame.robots.push({
        index: view.getUint8(offset),
        present: (flags & 1) !== 0, enabled: (flags & 2) !== 0, intake: (flags & 4) !== 0, shooting: (flags & 8) !== 0,
        climbing: (flags & 16) !== 0, manual: (flags & 32) !== 0, brownout: (flags & 64) !== 0, auto: (flags & 128) !== 0,
        team: view.getUint16(offset + 2, true),
        x: view.getFloat32(offset + 4, true), y: view.getFloat32(offset + 8, true), z: view.getFloat32(offset + 12, true),
        heading: view.getFloat32(offset + 16, true), pitch: view.getFloat32(offset + 20, true), roll: view.getFloat32(offset + 24, true),
        hopper: view.getUint8(offset + 28), climbLevel: view.getUint8(offset + 29), archetype: view.getUint8(offset + 30),
        aiState: view.getUint8(offset + 31), climbProgress: view.getFloat32(offset + 32, true),
        cart: view.getUint8(offset + 36),
      });
      offset += ROBOT_RECORD;
    }
    // FUEL records: x y z (mm), plus a stable u16 id when header flag bit1 is set (older recordings have none).
    const withIds = (view.getUint8(13) & 2) !== 0;
    const record = withIds ? 8 : 6;
    frame.fuel = new Int16Array(fuelCount * 3);
    frame.fuelIds = withIds ? new Uint16Array(fuelCount) : null;
    for (let i = 0; i < fuelCount; i++) {
      const at = offset + i * record;
      frame.fuel[3 * i] = view.getInt16(at, true);
      frame.fuel[3 * i + 1] = view.getInt16(at + 2, true);
      frame.fuel[3 * i + 2] = view.getInt16(at + 4, true);
      if (withIds) frame.fuelIds[i] = view.getUint16(at + 6, true);
    }
    offset += fuelCount * record;
    frame.chute = [view.getUint8(offset), view.getUint8(offset + 1)];
    frame.doors = view.getUint8(offset + 2);
    frame.hubStored = [view.getUint16(offset + 3, true), view.getUint16(offset + 5, true)];
    if (buffer.byteLength >= offset + 10) {
      frame.corral = [view.getUint8(offset + 7), view.getUint8(offset + 8)];
      frame.throws = view.getUint8(offset + 9);
      // The field reset crew (volunteers walking the field between matches).
      frame.crew = [];
      if (buffer.byteLength >= offset + 11) {
        const count = view.getUint8(offset + 10);
        for (let i = 0; i < count && buffer.byteLength >= offset + 11 + (i + 1) * 6; i++) {
          const at = offset + 11 + i * 6;
          frame.crew.push({ x: view.getInt16(at, true) / 1000, y: view.getInt16(at + 2, true) / 1000,
            mode: view.getUint8(at + 4), bucket: view.getUint8(at + 5) });
        }
        // What is inside each robot's hopper: per robot a count, then each FUEL's position in the robot frame.
        let at = offset + 11 + count * 6;
        frame.hoppers = [];
        for (let i = 0; i < robotCount && at < buffer.byteLength; i++) {
          const n = view.getUint8(at);
          at++;
          if (at + n * 6 > buffer.byteLength) break;
          frame.hoppers.push(new Int16Array(buffer.slice(at, at + n * 6)));
          at += n * 6;
        }
      }
    }
    return frame;
  }

  function applyFuel(frame) {
    const count = frame.fuel.length / 3;
    if (state.fuelPositions.length !== frame.fuel.length) state.fuelPositions = new Float32Array(frame.fuel.length);
    for (let i = 0; i < frame.fuel.length; i++) state.fuelPositions[i] = frame.fuel[i] / 1000;
    venue.fuel.setFieldPositions(state.fuelPositions, count, frame.fuelIds);
  }

  function applyStatic(frame) {
    // Outpost chute doors.
    for (const [alliance, bit, index] of [["red", 1, 0], ["blue", 2, 1]]) {
      const outpost = venue.field.outposts[alliance];
      if (!outpost) continue;
      outpost.doorPivot.rotation.z = (frame.doors & bit) ? 1.4 : 0;
      outpost.chuteBalls.count = Math.min(outpost.chuteBalls.instanceMatrix.count, frame.chute[index]);
      if (frame.corral) {
        outpost.corralBalls.count = Math.min(outpost.corralBalls.instanceMatrix.count, frame.corral[index]);
        const human = outpost.human;
        const now = performance.now();
        if ((frame.throws & bit) && now - human.throwAt > 700) human.throwAt = now;
        // Wind up and release over ~0.5 s, then rest; the held ball shows while there is FUEL in the corral.
        const t = (now - human.throwAt) / 500;
        human.shoulder.rotation.z = t < 1 ? Math.PI * (0.2 + 0.9 * Math.sin(Math.PI * t)) : 0.15;
        human.heldBall.visible = frame.corral[index] > 0 && (t > 1 || t < 0.55);
      }
    }
    for (const r of frame.robots) {
      const station = STATIONS[r.index];
      let robot = venue.robots[station];
      const design = designs.get(STATIONS[r.index]);
      const own = design && design.team === r.team ? design : null;
      const archetype = own ? own.name : ARCHETYPE_NAMES[r.archetype] || "scorer";
      if (robot && robot.archetype !== archetype) {
        venue.scene.remove(robot.root);
        robot = null;
      }
      if (!robot) {
        robot = createRobot({ team: r.team, alliance: station[0] === "R" ? "red" : "blue", archetype, spec: own?.model });
        venue.scene.add(robot.root);
        venue.robots[station] = robot;
      }
      robot.root.visible = r.present;
      robot.setTeam(r.team);
      const held = frame.hoppers?.[r.index];
      if (held) robot.setHopperPositions(held);
      else if (robot.lastHopper !== r.hopper) {
        robot.setHopperFuel(r.hopper);
        robot.lastHopper = r.hopper;
      }
      robot.streamEnabled = r.enabled;
      robot.flags = r;
    }
  }

  // Which robots have a jammed shooter (from the 10 Hz world summary), for the smoke.
  const jammed = new Set();
  on("world.state", (world) => {
    jammed.clear();
    for (const r of (world && world.robots) || []) {
      if (r.model?.frame) designs.set(r.station, { team: r.team, name: r.archetype, model: r.model });
      if ((r.faults || []).includes("shooter jam")) jammed.add(r.station);
    }
  });

  // Per-frame interpolation of robot poses between the last two snapshots.
  const position = new THREE.Vector3();
  venue.addUpdater((dt, now) => {
    const frame = state.lastFrame;
    if (!frame) return;
    const previous = state.previous;
    const alpha = previous ? Math.min(1, (performance.now() - state.receivedAt) / state.interval) : 1;
    for (const r of frame.robots) {
      const robot = venue.robots[STATIONS[r.index]];
      if (!robot || !r.present) continue;
      const p = previous?.robots[r.index];
      const lerp = (a, b) => (p ? a + (b - a) * alpha : b);
      let heading = r.heading;
      if (p) {
        let delta = r.heading - p.heading;
        if (delta > Math.PI) delta -= 2 * Math.PI;
        if (delta < -Math.PI) delta += 2 * Math.PI;
        heading = p.heading + delta * alpha;
      }
      const x = lerp(p?.x ?? r.x, r.x);
      const y = lerp(p?.y ?? r.y, r.y);
      position.copy(f2s(x, y, lerp(p?.z ?? r.z, r.z)));
      robot.root.position.copy(position);
      robot.root.rotation.y = heading;
      // Motion in the robot's own frame (smoothed), for the wheels, swerve steering and body lean.
      const m = robot.motionTrack || (robot.motionTrack = { x, y, heading, vf: 0, vl: 0, omega: 0, af: 0, al: 0 });
      if (dt > 0 && dt < 0.2 && r.cart === 0) {
        const fx = (x - m.x) / dt;
        const fy = (y - m.y) / dt;
        let turn = heading - m.heading;
        turn = Math.atan2(Math.sin(turn), Math.cos(turn));
        const c = Math.cos(heading);
        const s = Math.sin(heading);
        const vf = fx * c + fy * s;
        const vl = -fx * s + fy * c;
        const k = Math.min(1, dt * 12);
        const nextVf = m.vf + (vf - m.vf) * k;
        const nextVl = m.vl + (vl - m.vl) * k;
        m.af += ((nextVf - m.vf) / dt - m.af) * Math.min(1, dt * 8);
        m.al += ((nextVl - m.vl) / dt + nextVf * m.omega - m.al) * Math.min(1, dt * 8); // (turning is sideways acceleration)
        m.vf = nextVf;
        m.vl = nextVl;
        m.omega += (turn / dt - m.omega) * k;
      }
      m.x = x;
      m.y = y;
      m.heading = heading;
      let turretAim = null;
      if (robot.parts.turret && venue.layout) {
        // The turret tracks its alliance's hub regardless of the drivetrain.
        const hub = venue.layout.hub.positions.blue;
        const red = STATIONS[r.index][0] === "R";
        const hx = red ? venue.layout.field.length - hub[0] : hub[0];
        const hy = red ? venue.layout.field.width - hub[1] : hub[1];
        turretAim = Math.atan2(hy - r.y, hx - r.x) - heading + Math.PI; // the shooter fires out the back of the turret
      }
      const motion = robot.animate(dt, { forward: m.vf, left: m.vl }, m.omega, { forward: m.af, left: m.al }, r, turretAim,
        jammed.has(STATIONS[r.index]), now);
      robot.body.rotation.z = -r.pitch + motion.lean;
      robot.body.rotation.x = r.roll + motion.roll;
      // Mechanisms: the intake deploys over the bumper while running and folds back in; the climber arms swing up
      // to the rung (they never extend past the 30 in limit, R107); flywheels spin up while enabled.
      robot.intakeDeploy = (robot.intakeDeploy ?? 0) + ((r.intake ? 1 : 0) - (robot.intakeDeploy ?? 0)) * Math.min(1, dt * 7);
      if (robot.setIntake) robot.setIntake(robot.intakeDeploy);
      robot.parts.intakeRoller.rotation.y += r.intake ? dt * 25 : 0;
      robot.climbShown = (robot.climbShown ?? 0) + ((r.climbing || r.climbLevel ? 1 : 0) - (robot.climbShown ?? 0)) * Math.min(1, dt * 3);
      if (robot.setClimb) robot.setClimb(robot.climbShown);
      if (robot.parts.flywheel && (r.shooting || r.enabled)) robot.parts.flywheel.rotation.z -= dt * (r.shooting ? 60 : 15);
      if (robot.parts.flap) robot.parts.flap.rotation.z = r.shooting ? -0.9 : 0;
      robot.enabled = r.enabled;
    }
  });
  connect();
  return state;
}
