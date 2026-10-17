// Drive teams behind the alliance walls: a driver and an operator at each driver station shelf and their coach
// behind them. They react to the match like real drive teams: hands off the controllers in AUTO (G402), working the
// controllers in TELEOP with their heads tracking their robot and the coach pointing out the play, a fist pump when
// their alliance scores a burst, hands thrown up at a foul against them, arms up for a climb, and a celebration or a
// slump at the final buzzer.
import * as THREE from "three";
import { on } from "../core/store.js";
import { clock } from "../core/match.js";
import { allianceColor, f2s } from "./util.js";
import { registerFigure } from "./people.js";

const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];
const SKIN = [0xf1c7a5, 0xd9a07a, 0xb57a55, 0x8d5a3b, 0x5f3b26];
const SHIRT = [0x2a2d33, 0x6b6f78, 0xf2f2f2, 0x1f3a5f, 0x5a1f1f, 0x2f4f2f];

export function person(seed, { shirt, coach = false } = {}) {
  const pick = (list, k) => list[(seed * 7 + k * 13) % list.length];
  const root = new THREE.Group();
  const top = new THREE.MeshStandardMaterial({ color: shirt ?? pick(SHIRT, 1), roughness: 0.85 });
  const skin = new THREE.MeshStandardMaterial({ color: pick(SKIN, 2), roughness: 0.7 });
  const pants = new THREE.MeshStandardMaterial({ color: 0x23262c, roughness: 0.9 });
  const height = 0.92 + ((seed * 37) % 11) / 100; // a little variety in height
  const legs = new THREE.Mesh(new THREE.CapsuleGeometry(0.12, 0.6 * height, 4, 8), pants);
  legs.position.y = 0.44 * height;
  const torso = new THREE.Mesh(new THREE.CapsuleGeometry(0.17, 0.36, 4, 10), top);
  torso.position.y = 1.13 * height;
  const neck = new THREE.Group();
  neck.position.y = 1.47 * height;
  const head = new THREE.Mesh(new THREE.SphereGeometry(0.1, 14, 10), skin);
  head.position.y = 0.1;
  neck.add(head);
  // Safety glasses and a drive team button on the shirt.
  const glasses = new THREE.Mesh(new THREE.BoxGeometry(0.03, 0.03, 0.17), new THREE.MeshStandardMaterial({ color: 0x111111,
    transparent: true, opacity: 0.75 }));
  glasses.position.set(0.09, 0.12, 0);
  neck.add(glasses);
  const badge = new THREE.Mesh(new THREE.CircleGeometry(0.035, 12), new THREE.MeshStandardMaterial({
    color: coach ? 0xf6a823 : 0xf2f2f2 }));
  badge.position.set(0.172, 1.2 * height, 0.06);
  badge.rotation.y = Math.PI / 2;
  const arm = (side) => {
    const pivot = new THREE.Group();
    pivot.position.set(0, 1.34 * height, side * 0.22);
    const limb = new THREE.Mesh(new THREE.CapsuleGeometry(0.045, 0.46, 4, 8), top);
    limb.position.y = -0.27;
    const hand = new THREE.Mesh(new THREE.SphereGeometry(0.045, 10, 8), skin);
    hand.position.y = -0.53;
    pivot.add(limb, hand);
    return pivot;
  };
  const right = arm(1);
  const left = arm(-1);
  root.add(legs, torso, neck, badge, right, left);
  root.traverse((child) => {
    if (!child.isMesh) return;
    child.castShadow = true;
    child.userData.body = true; // (the capsule body: hidden once the rigged human takes over, people.js)
  });
  const figure = { root, neck, right, left, torso, height, baseY: 0, seed,
    look: { skin: skin.color.getHex(), shirt: top.color.getHex(), pants: pants.color.getHex() } };
  registerFigure(figure);
  return figure;
}

export function setupDriveTeams(venue, layout) {
  const ds = layout.driverStation;
  const teams = [];
  let seed = 3;
  for (const [name, station] of Object.entries(venue.field.stations || {})) {
    const alliance = station.alliance;
    const crew = {};
    // Blue-frame coordinates inside the station group (the red half is mirrored): drivers stand behind the shelf
    // facing +x (the field), the coach a step behind.
    const behind = -0.05 - ds.shelfDepth - 0.28;
    const tint = allianceColor(alliance);
    crew.driver = person(seed++, { shirt: seed % 3 === 0 ? tint : undefined });
    crew.operator = person(seed++);
    crew.coach = person(seed++, { coach: true });
    // Controllers in the driver's and operator's hands.
    for (const member of [crew.driver, crew.operator]) {
      const pad = new THREE.Mesh(new THREE.BoxGeometry(0.1, 0.035, 0.16), new THREE.MeshStandardMaterial({ color: 0x15171a,
        roughness: 0.6 }));
      pad.position.set(0.26, 1.02 * member.height, 0);
      member.root.add(pad);
      member.pad = pad;
    }
    for (const [member, x, dy] of [[crew.driver, behind, 0.32], [crew.operator, behind, -0.32], [crew.coach, behind - 0.45, 0.05]]) {
      member.root.position.copy(f2s(x, station.center + dy, 0)); // blue-frame coordinates in the (mirrored) half
      station.group.parent?.add(member.root);
      member.home = member.root.position.clone();
    }
    crew.station = name;
    crew.alliance = alliance;
    crew.phase = Math.random() * 10;
    crew.half = station.group.parent;
    teams.push(crew);
  }

  // Human players: every team brings one, so three per alliance work the OUTPOST AREA. The outpost already has its
  // thrower; the other two load the chute from the corral and spot / hand FUEL up (behind the HUMAN STARTING LINE,
  // 24 in from the wall, during AUTO; G401).
  const outpostY = layout.outpost.openingY ?? 0.882;
  const helpers = [];
  for (const alliance of ["red", "blue"]) {
    const half = teams.find((crew) => crew.alliance === alliance)?.half;
    if (!half) continue;
    const tint = allianceColor(alliance);
    for (const [role, x, y] of [["loader", -0.95, outpostY + 0.5], ["spotter", -1.75, outpostY - 0.45]]) {
      const hp = person(seed++, { shirt: tint });
      hp.root.position.copy(f2s(x, y, 0));
      half.add(hp.root);
      hp.home = hp.root.position.clone();
      helpers.push({ hp, role, alliance, phase: Math.random() * 10 });
    }
  }

  // E-stops and A-stops: whoever is nearest the button hits it (E-stop on the drivers' left, A-stop on the right).
  const stops = {};
  on("field.io", (io) => {
    if (!io) return;
    STATIONS.forEach((station, i) => {
      const e = !!io.teamEStop?.[i];
      const a = !!io.teamAStop?.[i];
      const previous = stops[station] || {};
      stops[station] = {
        e, a,
        eAt: e && !previous.e ? performance.now() : previous.eAt ?? -1e9,
        aAt: a && !previous.a ? performance.now() : previous.aAt ?? -1e9,
      };
    });
  });

  // What the drive teams react to.
  let scores = { red: 0, blue: 0 };
  const bursts = { red: 0, blue: 0 };
  const lastScore = { red: 0, blue: 0 };
  let fouledAt = { red: -1e9, blue: -1e9 };
  const seenCalls = new Set();
  on("arena.realtimeScore", (score) => {
    if (!score) return;
    scores = { red: score.Red?.ScoreSummary?.Score ?? 0, blue: score.Blue?.ScoreSummary?.Score ?? 0 };
    for (const alliance of ["red", "blue"]) {
      if (scores[alliance] - lastScore[alliance] >= 6) bursts[alliance] = performance.now();
      if (scores[alliance] !== lastScore[alliance]) lastScore[alliance] = scores[alliance];
    }
  });
  on("world.state", (state) => {
    for (const call of state?.officials?.recent || []) {
      const key = `${call.at}|${call.rule}|${call.station}`;
      if (seenCalls.has(key)) continue;
      seenCalls.add(key);
      fouledAt = { ...fouledAt, [call.alliance === 0 ? "red" : "blue"]: performance.now() };
    }
  });

  const tmp = new THREE.Vector3();
  const ease = (object, axis, value, dt, rate = 7) => {
    object.rotation[axis] += (value - object.rotation[axis]) * Math.min(1, dt * rate);
  };
  venue.addUpdater((dt) => {
    const now = performance.now();
    const c = clock();
    const auto = c.state === 1 || c.state === 2;
    const teleop = c.state === 4 || c.state === 3;
    const post = c.state === 5;
    const t = now / 1000;
    for (const crew of teams) {
      const robot = venue.robots?.[crew.station];
      const other = crew.alliance === "red" ? "blue" : "red";
      const won = post && scores[crew.alliance] > scores[other];
      const lost = post && scores[crew.alliance] < scores[other];
      const cheering = now - bursts[crew.alliance] < 1300;
      const upset = now - fouledAt[crew.alliance] < 1800;
      const climbing = robot?.flags?.climbing || robot?.flags?.climbLevel > 0;
      const stop = stops[crew.station] || {};
      const empty = !robot || !robot.team; // no team in this station
      const carting = venue.betweenMatch?.carting?.has(crew.station);
      const pre = c.state === 0;
      for (const [role, member] of Object.entries({ driver: crew.driver, operator: crew.operator, coach: crew.coach })) {
        // The driver and operator are out pushing the robot cart; nobody stands at an empty station.
        member.root.visible = !empty && !(carting && role !== "coach");
        if (!member.root.visible) continue;
        const sway = Math.sin(t * 1.1 + crew.phase + (role === "coach" ? 1 : 0)) * 0.03;
        // E-stop (drivers' left, the driver's side) and A-stop (right, the operator's side): reach down and slap it,
        // then step back with the controller down while the robot sits stopped.
        const pressE = role === "driver" && stop.e && now - stop.eAt < 1200;
        const pressA = role === "operator" && stop.a && now - stop.aAt < 1200;
        if (pressE || pressA) {
          ease(pressE ? member.left : member.right, "z", 0.7, dt, 14);
          ease(pressE ? member.right : member.left, "z", 0.2, dt);
          member.torso.rotation.z = -0.3;
          ease(member.neck, "y", pressE ? 0.6 : -0.6, dt, 8);
          if (member.pad) member.pad.visible = false;
          continue;
        }
        if (stop.e && role !== "coach") {
          // Stopped for the match: controllers down, waiting it out.
          ease(member.right, "z", 0.1, dt);
          ease(member.left, "z", 0.1, dt);
          member.torso.rotation.z = 0.12;
          if (member.pad) member.pad.visible = false;
          continue;
        }
        // Heads follow their robot (in the world frame, so the mirrored red half works too).
        if (robot && robot.root.visible) {
          tmp.copy(robot.root.position);
          member.root.worldToLocal(tmp);
          const yaw = Math.atan2(-tmp.z, tmp.x);
          ease(member.neck, "y", Math.max(-1.2, Math.min(1.2, yaw)), dt, 4);
        } else {
          ease(member.neck, "y", sway * 4, dt, 2);
        }
        let right = 0.05;
        let left = -0.05;
        let hop = 0;
        let lean = sway;
        if (won) {
          right = Math.PI * 0.95 + Math.sin(t * 8) * 0.15;
          left = -(Math.PI * 0.95 + Math.cos(t * 8) * 0.15);
          hop = Math.max(0, Math.sin(t * 7 + crew.phase)) * 0.12;
        } else if (lost) {
          lean = 0.18;
          right = 0.1;
          left = -0.1;
        } else if (upset && (role === "coach" || role === "driver")) {
          right = Math.PI * 0.7;
          left = -Math.PI * 0.7; // hands up: "what was that?"
        } else if (cheering || (climbing && role === "coach")) {
          right = role === "coach" ? Math.PI * 0.9 : 1.0 + Math.sin(t * 14) * 0.25;
          left = role === "coach" ? -0.1 : -1.0;
          hop = cheering ? Math.max(0, Math.sin(t * 9)) * 0.05 : 0;
        } else if (role !== "coach" && teleop) {
          // Working the controller: forearms forward, thumbs busy with the robot's motion.
          const busy = robot?.enabled ? 1 : 0;
          right = 0.95 + Math.sin(t * 11 + crew.phase) * 0.06 * (1 + busy);
          left = -(0.95 + Math.cos(t * 9 + crew.phase) * 0.06 * (1 + busy));
        } else if (role === "coach" && teleop) {
          // The coach points out the play every few seconds.
          const pointing = Math.sin(t * 0.6 + crew.phase) > 0.35;
          right = pointing ? Math.PI / 2 : 0.15;
        } else if (auto && role !== "coach") {
          right = 0.25; // G402: hands off the controls in AUTO
          left = -0.25;
        } else if (pre && role !== "coach") {
          // Before the match: at the laptop, checking the Driver Station and the joysticks.
          right = 0.72 + Math.sin(t * 13 + crew.phase) * 0.04;
          left = -(0.72 + Math.cos(t * 12 + crew.phase) * 0.04);
          lean = -0.12;
        } else if (pre && role === "coach") {
          // Talking strategy: a gesture now and then.
          right = Math.sin(t * 0.9 + crew.phase) > 0.5 ? 0.9 + Math.sin(t * 5) * 0.2 : 0.1;
        }
        // Arms swing forward (about z) in this rig; right is +z side.
        ease(member.right, "z", right > 0 ? right : -right, dt);
        ease(member.left, "z", left < 0 ? -left : left, dt);
        member.root.position.y = member.home.y + hop;
        ease(member.root, "z", 0, dt);
        member.torso.rotation.z = lean * 0.6;
        if (member.pad) member.pad.visible = role !== "coach" && !(won && hop > 0.05);
      }
    }
    // The other two human players: the loader scoops FUEL from the corral into the chute when there is some; the
    // spotter watches the hub, hands FUEL forward and cheers the scoring.
    const corral = venue.world?.lastFrame?.corral || [0, 0];
    for (const helper of helpers) {
      const { hp, role, alliance } = helper;
      const stock = corral[alliance === "red" ? 0 : 1];
      const cheering = now - bursts[alliance] < 1300;
      if (role === "loader" && stock > 0 && !auto) {
        const scoop = (t * 1.1 + helper.phase) % 1;
        hp.torso.rotation.z = -0.6 * Math.sin(scoop * Math.PI);
        ease(hp.right, "z", 0.6 + Math.sin(scoop * Math.PI * 2) * 0.6, dt, 10);
        ease(hp.left, "z", 0.5, dt);
      } else if (cheering) {
        ease(hp.right, "z", Math.PI * 0.9, dt, 10);
        ease(hp.left, "z", Math.PI * 0.9, dt, 10);
        hp.torso.rotation.z = 0;
      } else {
        ease(hp.right, "z", role === "spotter" && stock > 0 ? 0.8 : 0.08, dt);
        ease(hp.left, "z", 0.08, dt);
        hp.torso.rotation.z = Math.sin(t * 1.2 + helper.phase) * 0.02;
      }
      hp.root.position.y = hp.home.y + (cheering ? Math.max(0, Math.sin(t * 9 + helper.phase)) * 0.05 : 0);
    }
  });
  return teams;
}
