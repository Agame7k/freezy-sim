// Procedural FRC robot models, built like 2026 REBUILT robots are: a 2x1 tube drive base (swerve, West Coast tank,
// or the KOP chassis), bumpers with corner pads and team numbers, an over-the-bumper intake that deploys when
// running, a polycarbonate hopper on a tube frame with an indexer floor, a shooter between side plates (flywheels
// under a hood, motors outboard; on a turntable for turret robots; one lane per FUEL for wide shooters), a hook-arm
// climber, the electronics, and the RSL. Every design stays inside the 30 in limit (R104/R107) at all times.
import * as THREE from "three";
import { M, allianceColor, CanvasPanel, tagPick } from "./util.js";

export const MAX_HEIGHT = 0.762;

// Designs (frame and height match the simulator's archetypes in internal/world/robot.go).
export const ARCHETYPES = {
  scorer: { label: "Low shooter (trench)", height: 0.54, frame: 0.68, drive: "swerve", intake: 0.92, shooter: "fixed",
    lanes: 2, hopper: 0.78, capacity: 32, climber: true },
  tall: { label: "Tall shooter (30 in)", height: 0.75, frame: 0.72, drive: "swerve", intake: 0.95, shooter: "fixed",
    lanes: 3, hopper: 0.8, capacity: 66, climber: true },
  volley: { label: "Wide volley shooter (4 lanes)", height: 0.55, frame: 0.78, drive: "swerve", intake: 1.0,
    shooter: "wide", lanes: 4, hopper: 0.82, capacity: 50, climber: true },
  defender: { label: "Defender", height: 0.36, frame: 0.74, drive: "swerve", intake: 0.7, shooter: "small", lanes: 1,
    hopper: 0.5, hopperLength: 0.55, capacity: 6 },
  turret: { label: "Turret shooter", height: 0.55, frame: 0.7, drive: "swerve", intake: 0.95, shooter: "turret",
    lanes: 2, hopper: 0.78, capacity: 33, climber: true },
  bigdumper: { label: "Big dumper (4-wide)", height: 0.74, frame: 0.84, drive: "swerve", intake: 1.0, shooter: "wide",
    lanes: 4, hopper: 0.86, bin: true, capacity: 120, climber: true },
  shuttle: { label: "Shuttle bot", height: 0.45, frame: 0.74, drive: "swerve", intake: 1.12, shooter: "fixed",
    lanes: 3, hopper: 0.8, capacity: 29 },
  kitbot: { label: "KitBot", height: 0.5, frame: 0.7, drive: "swerve", intake: 0.86, shooter: "kitbot", lanes: 1,
    hopper: 0.5, capacity: 18, climber: true },
};

const BUMPER_H = 0.127;
const BUMPER_T = 0.085;
const BUMPER_Z = 0.03; // bumper bottom above the carpet
const TUBE = 0.025;
// Powder-coat colors teams pick (chosen per team).
const ACCENTS = [0x1c1d21, 0x3b4048, 0x7a1418, 0x123f7e, 0xd2a019, 0x1f6b33, 0xe0600a, 0x5a2a82, 0x2a7f8a, 0xb8bcc4];

// Bumper cloth: the deep red and royal blue teams actually buy (the field's alliance colors are brighter).
const BUMPER_FABRIC = { red: 0xa3121a, blue: 0x0f3384 };

/** White team numbers printed straight onto the bumper cloth (R405), on a transparent canvas. */
function drawNumber(panel, team) {
  panel.g.clearRect(0, 0, panel.canvas.width, panel.canvas.height);
  panel.last = null;
  panel.text(String(team || ""), { fg: "#f4f4f2", bg: "rgba(0,0,0,0)", font: "bold 84px 'Arial Black', 'Segoe UI', sans-serif" });
}

function bumperTexture(team) {
  const panel = new CanvasPanel(512, 96);
  drawNumber(panel, team);
  return panel;
}

function materials(team, alliance) {
  const accent = ACCENTS[Math.abs((team * 2654435761) >>> 0) % ACCENTS.length];
  return {
    // Pre-drilled 1x1 / 2x1 tube (holes every 1/2 in); the UVs of tube boxes are in meters (see box()).
    tube: new THREE.MeshStandardMaterial({ color: 0xa4aab2, metalness: 0.75, roughness: 0.35, map: holeTexture(),
      userData: { meterUV: true } }),
    darkTube: new THREE.MeshStandardMaterial({ color: 0x2d3036, metalness: 0.55, roughness: 0.45 }),
    plate: new THREE.MeshStandardMaterial({ color: accent, metalness: 0.35, roughness: 0.45 }),
    poly: new THREE.MeshPhysicalMaterial({ color: 0xdfe9f5, roughness: 0.06, transparent: true, opacity: 0.18,
      depthWrite: false, side: THREE.DoubleSide }),
    smoke: new THREE.MeshStandardMaterial({ color: 0x23272d, roughness: 0.4, transparent: true, opacity: 0.85 }),
    motor: new THREE.MeshStandardMaterial({ color: 0x15161a, metalness: 0.4, roughness: 0.4 }),
    motorCap: new THREE.MeshStandardMaterial({ color: 0xc9ccd1, metalness: 0.8, roughness: 0.3 }),
    rubber: new THREE.MeshStandardMaterial({ color: 0x141414, roughness: 0.95 }),
    tread: new THREE.MeshStandardMaterial({ color: 0x262626, roughness: 0.9, bumpMap: treadTexture(), bumpScale: 3 }),
    compliant: new THREE.MeshStandardMaterial({ color: 0x23a35f, roughness: 0.85 }),
    belt: new THREE.MeshStandardMaterial({ color: 0x3a3a3a, roughness: 0.8 }),
    sheet: new THREE.MeshStandardMaterial({ color: 0x8c929b, metalness: 0.7, roughness: 0.35 }),
    fabric: new THREE.MeshStandardMaterial({ color: BUMPER_FABRIC[alliance] ?? allianceColor(alliance), roughness: 0.95,
      bumpMap: weaveTexture(), bumpScale: 1.5 }),
    hub: new THREE.MeshStandardMaterial({ color: 0xb9bec6, metalness: 0.8, roughness: 0.3 }),
    redWire: new THREE.MeshStandardMaterial({ color: 0xb3161b, roughness: 0.55 }),
    blackWire: new THREE.MeshStandardMaterial({ color: 0x121214, roughness: 0.55 }),
    canWire: new THREE.MeshStandardMaterial({ color: 0xd8c21a, roughness: 0.6 }),
  };
}

// ---- Small builders (robot frame: +x forward / intake side, +y up, +z the robot's left) -------------------------

function box(parent, w, h, d, material, x, y, z) {
  const geometry = new THREE.BoxGeometry(w, h, d);
  if (material.userData.meterUV) {
    // BoxGeometry maps 0..1 across every face. Remap so u runs along the face's long side in 1/2 in steps and v
    // across it in 1 in steps, both from the face's center: one centered row of holes on a 1 in face, two on a 2 in.
    const uv = geometry.attributes.uv;
    const faces = [[d, h], [d, h], [w, d], [w, d], [w, h], [w, h]]; // +x, -x, +y, -y, +z, -z
    for (let i = 0; i < uv.count; i++) {
      const [su, sv] = faces[Math.floor(i / 4)];
      let along = (uv.getX(i) - 0.5) * su;
      let across = (uv.getY(i) - 0.5) * sv;
      if (sv > su) [along, across] = [across, along];
      uv.setXY(i, along / 0.0127 + 0.5, across / 0.0254 + (Math.min(su, sv) > 0.04 ? 0 : 0.5));
    }
  }
  const mesh = new THREE.Mesh(geometry, material);
  mesh.position.set(x, y, z);
  parent.add(mesh);
  return mesh;
}

/** A wire (or a bundle) along a smooth path through points [x, y, z]. */
function wire(parent, points, radius, material) {
  const curve = new THREE.CatmullRomCurve3(points.map(([x, y, z]) => new THREE.Vector3(x, y, z)));
  const mesh = new THREE.Mesh(new THREE.TubeGeometry(curve, Math.max(8, points.length * 6), radius, 6), material);
  parent.add(mesh);
  return mesh;
}

function cylinder(parent, radius, length, material, axis, x, y, z, segments = 16) {
  const geometry = new THREE.CylinderGeometry(radius, radius, length, segments);
  if (material.userData.meterUV) {
    const uv = geometry.attributes.uv;
    for (let i = 0; i < uv.count; i++) uv.setXY(i, uv.getY(i) * length / 0.0127, uv.getX(i) * 2 * Math.PI * radius / 0.0254);
  }
  const mesh = new THREE.Mesh(geometry, material);
  if (axis === "z") mesh.rotation.x = Math.PI / 2;
  if (axis === "x") mesh.rotation.z = Math.PI / 2;
  mesh.position.set(x, y, z);
  parent.add(mesh);
  return mesh;
}

/** A brushless motor (Kraken/NEO style): black can with a silver end cap, pointing along axis. */
function motor(parent, m, axis, x, y, z, length = 0.075, radius = 0.03) {
  const group = new THREE.Group();
  cylinder(group, radius, length, m.motor, "y", 0, 0, 0, 18);
  cylinder(group, radius * 0.92, 0.008, m.motorCap, "y", 0, length / 2 + 0.004, 0, 18);
  if (axis === "z") group.rotation.x = Math.PI / 2;
  if (axis === "x") group.rotation.z = Math.PI / 2;
  if (axis === "-x") group.rotation.z = -Math.PI / 2;
  group.position.set(x, y, z);
  parent.add(group);
  return group;
}

/** A CNC-cut side plate: rounded corners and lightening holes, in the x-y plane centered on z = 0. */
function sidePlate(parent, length, height, material, x, y, z, thickness = 0.006) {
  const r = Math.min(0.03, height * 0.15);
  const shape = new THREE.Shape();
  shape.moveTo(r, 0);
  shape.lineTo(length - r, 0);
  shape.quadraticCurveTo(length, 0, length, r);
  shape.lineTo(length, height - r);
  shape.quadraticCurveTo(length, height, length - r, height);
  shape.lineTo(r, height);
  shape.quadraticCurveTo(0, height, 0, height - r);
  shape.lineTo(0, r);
  shape.quadraticCurveTo(0, 0, r, 0);
  const holes = Math.max(1, Math.floor(length / 0.1));
  for (let i = 0; i < holes; i++) {
    const hole = new THREE.Path();
    hole.absarc((i + 0.5) * (length / holes), height * 0.38, Math.min(0.028, height * 0.14), 0, Math.PI * 2, true);
    shape.holes.push(hole);
  }
  const geometry = new THREE.ExtrudeGeometry(shape, { depth: thickness, bevelEnabled: false, curveSegments: 6 });
  geometry.translate(-length / 2, -height / 2, -thickness / 2);
  const mesh = new THREE.Mesh(geometry, material);
  mesh.position.set(x, y, z);
  parent.add(mesh);
  return mesh;
}

/** The 12 edges of a box of 1x1 tube from (x0,y0,z0) to (x1,y1,z1), skipping the bottom edges if asked. */
function tubeCage(parent, material, x0, x1, y0, y1, z0, z1, { bottom = false } = {}) {
  const t = TUBE * 0.7;
  const w = x1 - x0;
  const h = y1 - y0;
  const d = z1 - z0;
  const cx = (x0 + x1) / 2;
  const cy = (y0 + y1) / 2;
  const cz = (z0 + z1) / 2;
  for (const x of [x0, x1]) for (const z of [z0, z1]) box(parent, t, h, t, material, x, cy, z);
  for (const y of bottom ? [y0, y1] : [y1]) {
    for (const z of [z0, z1]) box(parent, w, t, t, material, cx, y, z);
    for (const x of [x0, x1]) box(parent, t, t, d, material, x, y, cz);
  }
}

// ---- Subsystems ----------------------------------------------------------------------------------------------

function driveBase(body, spec, m, parts, team) {
  const f = spec.frame;
  const half = f / 2;
  // 2x1 perimeter, cross members, belly pan.
  for (const s of [-1, 1]) {
    box(body, f, 0.05, TUBE, m.tube, 0, 0.075, s * (half - TUBE / 2));
    box(body, TUBE, 0.05, f - 2 * TUBE, m.tube, s * (half - TUBE / 2), 0.075, 0);
  }
  for (const x of [-0.2 * f, 0.2 * f]) box(body, TUBE, 0.05, f - 2 * TUBE, m.tube, x, 0.075, 0);
  box(body, f - 0.02, 0.005, f - 0.02, new THREE.MeshStandardMaterial({ color: 0x2a2d33, roughness: 0.6 }), 0, 0.048, 0);
  parts.wheels = [];
  parts.modules = [];
  // An aluminum hub on each face of the wheel with a hex bore and lightening holes, so its spin shows.
  const holeMaterial = new THREE.MeshStandardMaterial({ color: 0x0c0d0f, roughness: 0.8 });
  const spoke = (wheel, radius) => {
    const width = wheel.geometry.parameters.height;
    for (const face of [-1, 1]) {
      const hub = new THREE.Mesh(new THREE.CylinderGeometry(radius * 0.62, radius * 0.62, 0.004, 24), m.hub);
      hub.position.y = face * (width / 2 + 0.002);
      wheel.add(hub);
      const bore = new THREE.Mesh(new THREE.CylinderGeometry(radius * 0.13, radius * 0.13, 0.005, 6), holeMaterial);
      bore.position.y = face * (width / 2 + 0.0025);
      wheel.add(bore);
      for (let k = 0; k < 5; k++) {
        const a = (k / 5) * Math.PI * 2;
        const hole = new THREE.Mesh(new THREE.CylinderGeometry(radius * 0.12, radius * 0.12, 0.005, 10), holeMaterial);
        hole.position.set(Math.cos(a) * radius * 0.38, face * (width / 2 + 0.0025), Math.sin(a) * radius * 0.38);
        wheel.add(hole);
      }
    }
  };
  if (spec.drive === "swerve") {
    // Four COTS swerve modules in the corners, bolted into the frame rails. The housing stays put: a pre-drilled top
    // plate with the drive and steer motors standing on it (one belt-coupled under a team-colored cover), and the
    // absolute encoder in the middle. Under it, the fork (two side plates round the wheel) turns on the big bearing.
    const style = (Math.abs(team * 40503) >>> 0) % 2; // drive motor beside the steer motor, or behind it
    for (const [sx, sz] of [[1, 1], [1, -1], [-1, 1], [-1, -1]]) {
      const housing = new THREE.Group();
      housing.position.set(sx * (half - 0.075), 0, sz * (half - 0.075));
      box(housing, 0.13, 0.008, 0.13, m.tube, 0, 0.105, 0);
      box(housing, 0.13, 0.01, 0.012, m.darkTube, 0, 0.096, sz * 0.059); // the edges that sit on the frame rails
      box(housing, 0.012, 0.01, 0.13, m.darkTube, sx * 0.059, 0.096, 0);
      cylinder(housing, 0.05, 0.012, m.darkTube, "y", 0, 0.094, 0, 28); // bearing race
      const [dx, dz] = style ? [-0.032, -0.026] : [-0.034, 0.03];
      motor(housing, m, "y", 0.03, 0.152, 0.024, 0.075);
      motor(housing, m, "y", dx, 0.143, dz, 0.06, 0.026);
      box(housing, 0.075, 0.014, 0.05, m.plate, 0.012, 0.117, 0.02); // pulley cover
      box(housing, 0.026, 0.012, 0.026, m.motor, -0.02, 0.115, -0.03); // CANcoder
      box(housing, 0.018, 0.001, 0.012, m.motorCap, -0.02, 0.1215, -0.03);
      const steer = new THREE.Group();
      housing.add(steer);
      for (const s of [-1, 1]) {
        const plate = sidePlate(steer, 0.1, 0.07, m.darkTube, 0, 0.061, s * 0.026, 0.005);
        plate.userData.fork = true;
      }
      box(steer, 0.05, 0.008, 0.058, m.darkTube, 0, 0.088, 0); // fork crossbar under the bearing
      const wheel = cylinder(steer, 0.051, 0.038, m.tread, "z", 0, 0.051, 0, 20);
      cylinder(steer, 0.012, 0.06, m.motorCap, "z", 0, 0.051, 0, 10); // axle
      body.add(housing);
      spoke(wheel, 0.051);
      parts.wheels.push({ wheel, radius: 0.051, side: 0 });
      parts.modules.push({ group: steer, wheel, x: housing.position.x, z: housing.position.z, angle: 0, direction: 1 });
    }
  } else {
    // West Coast tank (or the KOP chassis): six wheels per side with the center pair dropped, gearboxes mid-side.
    const kop = spec.drive === "kop";
    for (const s of [-1, 1]) {
      if (kop) {
        box(body, f, 0.11, 0.006, m.sheet, 0, 0.075, s * (half - 0.012)); // sheet-metal chassis side
        box(body, f, 0.11, 0.006, m.sheet, 0, 0.075, s * (half - 0.075));
      } else {
        box(body, f - 0.04, 0.05, TUBE, m.tube, 0, 0.075, s * (half - 0.07));
      }
      for (const [i, x] of [-1, 0, 1].entries()) {
        const lift = i === 1 ? 0 : 0.004; // the center pair is dropped: the outer wheels ride just off the carpet
        const wheel = cylinder(body, 0.052, 0.04, m.tread, "z", x * (half - 0.11), 0.052 + lift, s * (half - 0.045), 20);
        spoke(wheel, 0.052);
        parts.wheels.push({ wheel, radius: 0.052, side: s }); // side -1 is the robot's left (-z), +1 its right
      }
      // Gearbox with two motors pointing inboard.
      box(body, 0.09, 0.09, 0.03, m.darkTube, 0, 0.1, s * (half - 0.1));
      motor(body, m, "z", 0.025, 0.12, s * (half - 0.16), 0.075);
      motor(body, m, "z", -0.025, 0.12, s * (half - 0.16), 0.075);
    }
  }
  // Electronics under the superstructure: battery, roboRIO, PDH (with its breakers), radio.
  const battery = { x: -0.12 * f, z: -0.26 * f };
  const pdh = { x: -0.18 * f, z: 0.24 * f };
  box(body, 0.18, 0.17, 0.08, m.motor, battery.x, 0.14, battery.z);
  for (const s of [-1, 1]) { // the white label band on both faces, and a velcro strap over the top
    box(body, 0.16, 0.06, 0.002, new THREE.MeshStandardMaterial({ color: 0xe4e2da, roughness: 0.7 }), battery.x, 0.15,
      battery.z + s * 0.041);
  }
  box(body, 0.03, 0.002, 0.084, m.motor, battery.x + 0.04, 0.226, battery.z);
  box(body, 0.06, 0.02, 0.03, new THREE.MeshStandardMaterial({ color: 0xc81e1e }), battery.x, 0.235, battery.z);
  box(body, 0.14, 0.03, 0.1, new THREE.MeshStandardMaterial({ color: 0xe8e8e4, roughness: 0.5 }), 0.02 * f, 0.065, 0.24 * f);
  box(body, 0.2, 0.04, 0.09, new THREE.MeshStandardMaterial({ color: 0x1c1f24 }), pdh.x, 0.07, pdh.z);
  box(body, 0.18, 0.012, 0.02, new THREE.MeshStandardMaterial({ color: 0xf6a823 }), pdh.x, 0.095, pdh.z);
  box(body, 0.1, 0.03, 0.07, new THREE.MeshStandardMaterial({ color: 0xf2f2f2, roughness: 0.5 }), -0.36 * f, 0.065, -0.05);
  // 6 AWG battery leads (through the Anderson connector and main breaker) to the PDH.
  for (const [dz, material] of [[0.008, m.redWire], [-0.008, m.blackWire]]) {
    wire(body, [[battery.x, 0.245, battery.z + dz], [battery.x - 0.03, 0.2, battery.z + 0.08], [pdh.x + 0.06, 0.075,
      pdh.z - 0.12 + dz], [pdh.x + 0.06, 0.07, pdh.z - 0.045 + dz]], 0.0055, material);
  }
  // Motor power from the PDH and the yellow CAN daisy chain to every module or gearbox, along the belly pan.
  const drops = spec.drive === "swerve"
    ? [[1, 1], [1, -1], [-1, 1], [-1, -1]].map(([sx, sz]) => [sx * (half - 0.075), sz * (half - 0.075)])
    : [[0, half - 0.16], [0, -(half - 0.16)]];
  drops.forEach(([x, z], i) => {
    const from = [pdh.x + (i - 1.5) * 0.04, 0.06, pdh.z - 0.045];
    const run = [from, [from[0], 0.058, z * 0.45], [x * 0.85, 0.06, z * 0.8], [x, 0.13, z]];
    wire(body, run, 0.0028, m.redWire);
    wire(body, run.map(([px, py, pz]) => [px + 0.006, py, pz]), 0.0028, m.blackWire);
    wire(body, run.map(([px, py, pz]) => [px - 0.008, py + 0.002, pz]), 0.0018, m.canWire);
  });
}

/** One bumper side: pool noodles under fabric, so the outer face is rounded top and bottom with a flat band
 * across the middle where the numbers are; the plywood backing is flat. Length along x, outward +z. */
function bumperGeometry(length) {
  const t = BUMPER_T;
  const h = BUMPER_H;
  const r = 0.022;
  const shape = new THREE.Shape();
  shape.moveTo(-t / 2, -h / 2);
  shape.lineTo(t / 2 - r, -h / 2);
  shape.quadraticCurveTo(t / 2, -h / 2, t / 2, -h / 2 + r);
  shape.lineTo(t / 2, h / 2 - r);
  shape.quadraticCurveTo(t / 2, h / 2, t / 2 - r, h / 2);
  shape.lineTo(-t / 2, h / 2);
  shape.lineTo(-t / 2, -h / 2);
  const geometry = new THREE.ExtrudeGeometry(shape, { depth: length, bevelEnabled: false, curveSegments: 6 });
  geometry.translate(0, 0, -length / 2);
  geometry.rotateY(-Math.PI / 2);
  geometry.computeVertexNormals();
  return geometry;
}

function bumpers(body, spec, m, team, alliance, parts) {
  const f = spec.frame;
  const y = BUMPER_Z + BUMPER_H / 2;
  const offset = f / 2 + BUMPER_T / 2;
  parts.numberPanels = [];
  parts.bumpers = [];
  for (let side = 0; side < 4; side++) {
    const angle = (side * Math.PI) / 2;
    const segment = new THREE.Mesh(bumperGeometry(f), m.fabric);
    segment.position.set(Math.sin(angle) * offset, y, Math.cos(angle) * offset);
    segment.rotation.y = angle;
    body.add(segment);
    parts.bumpers.push(segment);
    // The white team number, printed on the cloth (the cloth wraps the plywood, so no wood shows).
    const panel = bumperTexture(team);
    const label = new THREE.Mesh(new THREE.PlaneGeometry(Math.min(f * 0.8, 0.56), BUMPER_H * 0.76),
      new THREE.MeshStandardMaterial({ map: panel.texture, transparent: true, roughness: 0.9, polygonOffset: true,
        polygonOffsetFactor: -2 }));
    label.position.set(Math.sin(angle) * (offset + BUMPER_T / 2 + 0.002), y, Math.cos(angle) * (offset + BUMPER_T / 2 + 0.002));
    label.rotation.y = angle;
    body.add(label);
    parts.numberPanels.push(panel);
  }
  // Rounded corner pads (R406: corners filled with padding).
  for (const [sx, sz] of [[1, 1], [1, -1], [-1, 1], [-1, -1]]) {
    const corner = new THREE.Mesh(new THREE.CylinderGeometry(BUMPER_T, BUMPER_T, BUMPER_H, 14, 1, false, 0, Math.PI / 2),
      m.fabric);
    corner.position.set(sx * f / 2, y, sz * f / 2);
    corner.rotation.y = Math.atan2(sx, sz) - Math.PI / 4;
    body.add(corner);
  }
}

/** Over-the-bumper intake: side plates on a pivot at the top front of the frame, a compliant-wheel roller and a
 * polycord top roller, the motor inboard. Deployed it reaches past the bumper to the carpet; stowed it folds up inside
 * the perimeter. An intake wider than the frame (spec.intake > 1) gets roller extensions that slide out of the ends
 * as it deploys, so it still starts inside the frame. */
function intake(body, spec, m, parts) {
  const f = spec.frame;
  const width = f * spec.intake;
  const core = Math.min(width, f - 0.06); // between the side plates, inside the frame rails
  const pivot = new THREE.Group();
  pivot.position.set(f / 2 - 0.04, 0.24, 0);
  const reach = 0.26;
  const drop = -0.17;
  for (const s of [-1, 1]) {
    const arm = sidePlate(pivot, Math.hypot(reach, drop), 0.055, m.plate, reach / 2, drop / 2, s * core / 2);
    arm.rotation.z = Math.atan2(drop, reach);
  }
  const roller = cylinder(pivot, 0.016, core, m.tube, "z", reach, drop, 0, 10);
  const wheels = (shaft, span, from) => {
    const count = Math.max(1, Math.round(span / 0.1));
    for (let i = 0; i < count; i++) {
      const wheel = new THREE.Mesh(new THREE.CylinderGeometry(0.05, 0.05, 0.028, 14), m.compliant);
      wheel.position.y = from + ((i + 0.5) * span) / count; // along the roller's own axis
      shaft.add(wheel);
    }
  };
  wheels(roller, core, -core / 2);
  parts.intakeWings = [];
  const extra = (width - core) / 2;
  if (extra > 0.01) {
    const length = extra + 0.04;
    for (const s of [-1, 1]) {
      const wing = new THREE.Mesh(new THREE.CylinderGeometry(0.013, 0.013, length, 10), m.motorCap);
      wheels(wing, length, -length / 2);
      roller.add(wing);
      parts.intakeWings.push({ wing, stowed: s * (core / 2 - 0.005 - length / 2), deployed: s * (width / 2 - length / 2) });
    }
  }
  cylinder(pivot, 0.018, core - 0.22, m.belt, "z", reach * 0.55, drop * 0.45, 0, 10);
  motor(pivot, m, "z", reach * 0.5, drop * 0.5 + 0.03, core / 2 - 0.06, 0.07);
  body.add(pivot);
  parts.intake = pivot;
  parts.intakeRoller = roller;
  // Stowed it folds up and back inside the frame perimeter (R105) and under the robot's height: upright where
  // the robot is tall enough, leaning back over the hopper where it isn't.
  const length = Math.hypot(reach, drop);
  const headroom = Math.min(spec.height, MAX_HEIGHT) - pivot.position.y - 0.06; // room for the roller's wheels
  const stowedAngle = Math.max(Math.PI / 2 + 0.15, Math.PI - Math.asin(Math.max(0, Math.min(1, headroom / length))));
  parts.intakeStowed = stowedAngle - Math.atan2(drop, reach); // radians the pivot turns from deployed
  pivot.rotation.z = parts.intakeStowed;
  for (const { wing, stowed } of parts.intakeWings) wing.position.y = stowed;
}

/** A hopper's walls in the robot frame (x forward, left, up): mirrors hopperShape in internal/world/hopper.go, where
 * the FUEL inside is simulated. It runs from just behind the intake to the back of the frame, under the shooter. */
export function hopperShape(spec) {
  const f = spec.frame;
  const top = Math.min(spec.height, MAX_HEIGHT);
  const share = spec.hopper || 0.75;
  if (spec.shooter === "kitbot") {
    return { back: -0.36 * f, front: 0.26 * f, halfWidth: 0.27 * f, floor: 0.17, ceiling: top - 0.04, notchX: -1, notchTop: top };
  }
  const shape = { back: -f / 2 + 0.04, front: f / 2 - 0.1, halfWidth: (f * share) / 2, floor: 0.1, ceiling: top - 0.03, notchX: -1, notchTop: top };
  if (spec.hopperLength > 0) shape.front = shape.back + (shape.front - shape.back) * spec.hopperLength;
  if (spec.bin) {
    shape.halfWidth += 0.04;
    shape.floor = 0.07;
  }
  if (spec.shooter === "turret") shape.ceiling = top - 0.17;
  else if (spec.shooter === "small") [shape.notchX, shape.notchTop] = [-f / 2 + 0.32 * 0.7, top - 0.26 * 0.7 - 0.01];
  else [shape.notchX, shape.notchTop] = [-f / 2 + 0.32, top - 0.27];
  shape.notchTop = Math.max(shape.notchTop, shape.floor);
  return shape;
}

/** Hopper: polycarbonate walls on a 1x1 tube cage over an indexer floor (belt rollers feeding the shooter). Behind
 * the shooter's front edge the walls stop under the shooter. */
function hopper(body, spec, m, parts, team) {
  const shape = hopperShape(spec);
  const { back, front, halfWidth, floor } = shape;
  const length = front - back;
  box(body, length, 0.008, halfWidth * 2, m.smoke, (front + back) / 2, floor - 0.006, 0);
  parts.indexer = [];
  for (let i = 0; i < 3; i++) {
    const roller = cylinder(body, 0.018, halfWidth * 1.9, m.belt, "z", back + ((i + 0.5) * length) / 3, floor + 0.01, 0, 10);
    box(roller, 0.03, halfWidth * 1.92, 0.006, m.motorCap, 0, 0, 0); // a lacing strip, so the roll shows
    parts.indexer.push(roller);
  }
  // Clear walls: both sides (lower under the shooter) and the back; the front is low so FUEL comes in over the intake.
  const split = shape.notchX > back ? shape.notchX : back;
  const tall = shape.ceiling - floor;
  for (const s of [-1, 1]) {
    box(body, front - split, tall, 0.004, m.poly, (front + split) / 2, floor + tall / 2, s * halfWidth);
    if (split > back) {
      const low = shape.notchTop - floor;
      box(body, split - back, low, 0.004, m.poly, (split + back) / 2, floor + low / 2, s * halfWidth);
    }
  }
  const backHeight = (split > back ? shape.notchTop : shape.ceiling) - floor;
  box(body, 0.004, backHeight, halfWidth * 2, m.poly, back, floor + backHeight / 2, 0);
  box(body, 0.004, tall * 0.45, halfWidth * 2, m.poly, front, floor + tall * 0.225, 0);
  tubeCage(body, m.tube, split, front, floor, shape.ceiling, -halfWidth, halfWidth);
  parts.hopperShape = shape;
  // The team number in white vinyl on both side walls, where the wall is big enough to carry it.
  const decalW = Math.min(front - split - 0.05, 0.26);
  const decalH = Math.min(decalW * 0.3, tall * 0.35);
  if (decalW < 0.1 || decalH < 0.035) return;
  for (const s of [-1, 1]) {
    const panel = bumperTexture(team);
    const decal = new THREE.Mesh(new THREE.PlaneGeometry(decalW, decalH), new THREE.MeshStandardMaterial({
      map: panel.texture, transparent: true, roughness: 0.5, depthWrite: false }));
    decal.position.set((front + split) / 2, shape.ceiling - decalH / 2 - 0.025, s * (halfWidth + 0.003));
    if (s < 0) decal.rotation.y = Math.PI;
    body.add(decal);
    parts.numberPanels.push(panel);
  }
}

const FUEL_D = 0.15;
const HOPPER_DRAWN = 160; // more than any hopper holds

/** Where the k-th FUEL rests when a hopper is loaded without the simulator's positions (mirrors loadHopper in
 * internal/world/hopper.go). */
function hopperRest(shape, k) {
  const r = FUEL_D / 2;
  const nx = Math.max(1, Math.floor((shape.front - shape.back) / FUEL_D));
  const ny = Math.max(1, Math.floor((2 * shape.halfWidth) / FUEL_D));
  const layer = Math.floor(k / (nx * ny));
  const cell = k % (nx * ny);
  return {
    x: shape.front - r - (cell % nx) * FUEL_D - (layer % 2) * r,
    left: -shape.halfWidth + r + Math.floor(cell / nx) * FUEL_D + (layer % 2) * r * 0.5,
    up: Math.min(shape.floor + r + layer * FUEL_D * 0.8, shape.ceiling - r),
  };
}

// Flywheel wheels teams run: black Stealth, orange or blue compliant, grey urethane.
const FLYWHEEL_COLORS = [0x2b2b2e, 0xe0701c, 0x2f5fb8, 0x8a8f96];

/** Shooter between two side plates at the back: flywheels on a shaft under a hood, a feeder roller, motors
 * outboard (on the shaft, or belted to it from below). lanes FUEL wide, each lane a ball width plus dividers.
 * Returns the group (for turret mounting). */
function shooter(spec, m, lanes, team) {
  const group = new THREE.Group();
  const laneWidth = 0.18;
  const width = Math.max(0.2, lanes * laneWidth);
  const plateH = 0.26;
  const plateL = 0.3;
  const pick = Math.abs(team * 2246822519) >>> 0;
  const belted = pick % 2 === 1;
  const wheelMaterial = new THREE.MeshStandardMaterial({ color: FLYWHEEL_COLORS[(pick >>> 3) % FLYWHEEL_COLORS.length],
    roughness: 0.6, metalness: 0.1 });
  const shaft = { x: -0.07, y: plateH - 0.08 };
  for (const s of [-1, 1]) {
    const plate = sidePlate(group, plateL, plateH, m.plate, 0, plateH / 2, s * (width / 2 + 0.004));
    plate.userData.sidePlate = true;
    if (!belted) {
      motor(group, m, "z", shaft.x + 0.01, shaft.y, s * (width / 2 + 0.05), 0.075);
      continue;
    }
    // A motor low on the plate, belted up to the flywheel shaft (pulleys and a belt outside the plate).
    const low = { x: -0.1, y: 0.075 };
    motor(group, m, "z", low.x, low.y, s * (width / 2 + 0.06), 0.075);
    const z = s * (width / 2 + 0.017);
    const big = 0.03;
    const small = 0.02;
    cylinder(group, big, 0.014, m.hub, "z", shaft.x, shaft.y, z, 20);
    cylinder(group, small, 0.014, m.hub, "z", low.x, low.y, z, 16);
    const dx = shaft.x - low.x;
    const dy = shaft.y - low.y;
    const span = Math.hypot(dx, dy);
    const along = Math.atan2(dy, dx);
    for (const side of [-1, 1]) {
      const r = (big + small) / 2 + 0.002;
      const run = box(group, span, 0.003, 0.012, m.belt, (shaft.x + low.x) / 2 - side * Math.sin(along) * r,
        (shaft.y + low.y) / 2 + side * Math.cos(along) * r, z);
      run.rotation.z = along;
    }
  }
  // Flywheel shaft with one pair of wheels per lane.
  const flywheels = new THREE.Group();
  flywheels.position.set(shaft.x, shaft.y, 0);
  for (let lane = 0; lane < lanes; lane++) {
    const z = -width / 2 + (lane + 0.5) * (width / lanes);
    for (const dz of [-0.04, 0.04]) {
      const wheel = new THREE.Mesh(new THREE.CylinderGeometry(0.05, 0.05, 0.03, 20), wheelMaterial);
      wheel.rotation.x = Math.PI / 2;
      wheel.position.z = z + dz;
      flywheels.add(wheel);
    }
    if (lane > 0) box(group, plateL * 0.8, plateH * 0.7, 0.004, m.tube, 0, plateH * 0.45, -width / 2 + lane * (width / lanes));
  }
  cylinder(flywheels, 0.008, width, m.motorCap, "z", 0, 0, 0, 8);
  group.add(flywheels);
  // Hood: a curved smoked-polycarbonate sheet over the flywheels that sets the launch angle (the wheels show through).
  const hood = new THREE.Mesh(new THREE.CylinderGeometry(0.11, 0.11, width, 20, 1, true, Math.PI * 0.05, Math.PI * 0.6),
    new THREE.MeshPhysicalMaterial({ color: 0x3a4048, roughness: 0.15, transparent: true, opacity: 0.55,
      depthWrite: false, side: THREE.DoubleSide }));
  hood.rotation.x = Math.PI / 2;
  hood.position.set(shaft.x, shaft.y, 0);
  group.add(hood);
  // Feeder roller from the indexer.
  const feeder = cylinder(group, 0.025, width * 0.95, m.compliant, "z", 0.06, plateH * 0.45, 0, 12);
  box(feeder, 0.04, width * 0.96, 0.008, m.motorCap, 0, 0, 0);
  // A spoke on each flywheel too.
  for (const wheel of flywheels.children) {
    if (wheel.geometry?.parameters?.radiusTop === 0.05) box(wheel, 0.075, 0.032, 0.01, m.motorCap, 0, 0, 0);
  }
  return { group, flywheels, feeder, width, plateH };
}

/** Hook-arm climber: a 1x2 arm on a pivot that swings up to the LOW RUNG (it never extends past 30 in). */
function climber(body, spec, m, parts) {
  const f = spec.frame;
  const group = new THREE.Group();
  group.position.set(0.05 * f, 0.16, 0);
  const armLength = Math.min(spec.height, MAX_HEIGHT) - 0.2;
  for (const s of [-1, 1]) {
    const arm = new THREE.Group();
    arm.position.z = s * f * 0.3;
    box(arm, 0.025, armLength, 0.05, m.darkTube, 0, armLength / 2, 0);
    const hook = new THREE.Mesh(new THREE.TorusGeometry(0.03, 0.009, 6, 14, Math.PI * 1.2), m.motorCap);
    hook.position.set(0.02, armLength, 0);
    hook.rotation.z = Math.PI * 0.1;
    arm.add(hook);
    group.add(arm);
  }
  motor(group, m, "z", -0.04, 0.03, 0, 0.09);
  // Stowed lying back over the frame, but no further back than the frame's back rail (long arms stay tilted up).
  const room = group.position.x + f / 2 - 0.04;
  parts.climberStowed = Math.min(Math.PI / 2 - 0.15, Math.asin(Math.min(1, room / (armLength + 0.04))));
  group.rotation.z = parts.climberStowed;
  body.add(group);
  parts.climber = group;
  parts.climberRest = 1;
  parts.climberMax = 1;
}

function rslLight(body, m, x, y, z) {
  // Robot Signal Light (R709): the standard orange RSL on its black base, solid when disabled, blinking when enabled.
  const mount = new THREE.Group();
  box(mount, 0.062, 0.016, 0.042, m.motor, 0, 0, 0);
  const core = box(mount, 0.044, 0.03, 0.028,
    new THREE.MeshStandardMaterial({ color: 0x7a3a06, emissive: 0xff7a12, emissiveIntensity: 0.8, roughness: 0.3 }), 0, 0.022, 0);
  const lens = box(mount, 0.058, 0.04, 0.04, new THREE.MeshStandardMaterial({ color: 0xff9a3c, emissive: 0xff7a12,
    transparent: true, opacity: 0.45, roughness: 0.2, depthWrite: false }), 0, 0.028, 0);
  lens.onBeforeRender = () => { lens.material.emissiveIntensity = core.material.emissiveIntensity * 0.25; };
  mount.position.set(x, y, z);
  body.add(mount);
  return core;
}

// ---- Robots ----------------------------------------------------------------------------------------------------

// archetype names a stock design; spec (a team-built design's parts, ModelSpec in internal/world/design.go) overrides it.
export function createRobot({ team = 0, alliance = "blue", archetype = "scorer", spec: built = null } = {}) {
  const spec = built || ARCHETYPES[archetype] || ARCHETYPES.scorer;
  const m = materials(team, alliance);
  const root = new THREE.Group();
  root.name = `robot-${alliance}`;
  // body holds everything that pitches/rolls on bumps; root carries position/heading.
  const body = new THREE.Group();
  root.add(body);
  const f = spec.frame;
  const top = Math.min(spec.height, MAX_HEIGHT);
  const parts = { spec };

  driveBase(body, spec, m, parts, team);
  bumpers(body, spec, m, team, alliance, parts);
  intake(body, spec, m, parts);

  // Superstructure by design.
  if (spec.shooter === "turret") {
    hopper(body, spec, m, parts, team);
    // Turntable on a deck over the back of the hopper, the shooter riding on it.
    const deckY = top - 0.17;
    box(body, 0.34, 0.008, 0.34, m.darkTube, -0.18 * f, deckY, 0);
    const turret = new THREE.Group();
    turret.position.set(-0.18 * f, deckY + 0.004, 0);
    const ring = new THREE.Mesh(new THREE.TorusGeometry(0.15, 0.012, 8, 32), m.motorCap);
    ring.rotation.x = Math.PI / 2;
    turret.add(ring);
    box(turret, 0.3, 0.008, 0.28, m.plate, 0, 0.006, 0);
    const shot = shooter(spec, m, spec.lanes, team);
    shot.group.scale.setScalar(0.62);
    shot.group.position.y = 0.01;
    turret.add(shot.group);
    motor(turret, m, "y", 0.1, 0.05, 0.11, 0.05, 0.022);
    body.add(turret);
    parts.turret = turret;
    parts.flywheel = shot.flywheels;
    parts.feeder = shot.feeder;
  } else if (spec.shooter === "kitbot") {
    // The 2026 KitBot: a bent sheet-metal superstructure with a roller path to a shooter roller at the top back.
    box(body, f * 0.62, top - 0.14, 0.006, m.sheet, -0.05 * f, 0.12 + (top - 0.14) / 2, f * 0.3);
    box(body, f * 0.62, top - 0.14, 0.006, m.sheet, -0.05 * f, 0.12 + (top - 0.14) / 2, -f * 0.3);
    const ramp = box(body, f * 0.5, 0.006, f * 0.58, m.sheet, -0.05 * f, 0.2, 0);
    ramp.rotation.z = -0.35;
    const flywheels = new THREE.Group();
    flywheels.position.set(-0.3 * f, top - 0.07, 0);
    for (let i = 0; i < 4; i++) {
      const wheel = new THREE.Mesh(new THREE.CylinderGeometry(0.05, 0.05, 0.03, 18), m.compliant);
      wheel.rotation.x = Math.PI / 2;
      wheel.position.z = -0.21 + i * 0.14;
      flywheels.add(wheel);
    }
    body.add(flywheels);
    motor(body, m, "z", -0.3 * f, top - 0.07, f * 0.3 + 0.045, 0.075);
    parts.flywheel = flywheels;
    parts.hopperShape = hopperShape(spec);
  } else {
    hopper(body, spec, m, parts, team);
    const lanes = spec.shooter === "small" ? 1 : spec.lanes;
    const shot = shooter(spec, m, lanes, team);
    const scale = spec.shooter === "small" ? 0.7 : 1;
    shot.group.scale.setScalar(scale);
    // Mount the shooter at the back so its top is at the robot height.
    shot.group.position.set(-f / 2 + 0.17 * scale, top - shot.plateH * scale, 0);
    body.add(shot.group);
    // Uprights from the drive base up to the shooter plates.
    for (const s of [-1, 1]) box(body, TUBE, top - 0.1 - shot.plateH * scale, TUBE, m.tube, -f / 2 + 0.17,
      0.1 + (top - 0.1 - shot.plateH * scale) / 2, s * (shot.width * scale / 2 + 0.02));
    parts.hood = shot.group;
    parts.flywheel = shot.flywheels;
    parts.feeder = shot.feeder;
  }
  if (spec.climber) climber(body, spec, m, parts);
  else {
    parts.climber = new THREE.Group();
    parts.climberRest = 1;
    parts.climberMax = 1;
    body.add(parts.climber);
  }
  parts.rsl = rslLight(body, m, -f * 0.4, Math.min(top, MAX_HEIGHT - 0.05) - 0.05, f * 0.36);

  // FUEL visible inside the hopper (it packs in layers; see internal/world/fuel.go intakePath).
  const hopperFuel = new THREE.InstancedMesh(new THREE.SphereGeometry(0.075, 12, 8), M.fuel(), HOPPER_DRAWN);
  hopperFuel.count = 0;
  hopperFuel.castShadow = true;
  body.add(hopperFuel);
  parts.hopperFuel = hopperFuel;

  // A puff of smoke over the shooter when it jams (hidden until then).
  const smoke = new THREE.Group();
  const puffs = [];
  for (let i = 0; i < 5; i++) {
    const puff = new THREE.Mesh(new THREE.SphereGeometry(0.05 + i * 0.012, 10, 8),
      new THREE.MeshBasicMaterial({ color: 0x9a9a9a, transparent: true, opacity: 0, depthWrite: false }));
    smoke.add(puff);
    puffs.push(puff);
  }
  smoke.position.set(-f * 0.35, top + 0.05, 0);
  smoke.visible = false;
  body.add(smoke);

  root.traverse((child) => {
    if (child.isMesh) {
      child.castShadow = true;
      tagPick(child, { kind: "robot", alliance, team, label: `${team} (${alliance})` });
    }
  });

  // Motion state for animate(): the body leans with acceleration and bounces on hard hits; swerve modules steer.
  const motion = { lean: 0, roll: 0, bounce: 0, bounceAt: -1, turret: null, smokeAt: -1 };
  let climbShown = -1;
  const robot = {
    root, body, parts, team, alliance, archetype,
    setTeam(newTeam) {
      if (newTeam === robot.team) return;
      robot.team = newTeam;
      for (const panel of parts.numberPanels) drawNumber(panel, newTeam);
      root.traverse((child) => {
        if (child.userData.pick) child.userData.pick.label = `${newTeam} (${alliance})`;
        if (child.userData.pick) child.userData.pick.team = newTeam;
      });
    },
    /** Intake deploy (0 stowed .. 1 deployed), eased by the caller. */
    setIntake(amount) {
      parts.intake.rotation.z = parts.intakeStowed * (1 - amount);
      for (const { wing, stowed, deployed } of parts.intakeWings) wing.position.y = stowed + (deployed - stowed) * amount;
    },
    /** Climber: 0 stowed, 1 hooks up at the rung (the arm swings upright; the robot lifts itself). */
    setClimb(amount) {
      if (Math.abs(amount - climbShown) < 0.001) return;
      climbShown = amount;
      if (parts.climber) parts.climber.rotation.z = (parts.climberStowed ?? 0) * (1 - amount);
    },
    /**
     * Animates the mechanisms for one frame. v: { forward, left } m/s in the robot frame; omega rad/s (CCW from above);
     * accel: { forward, left } m/s²; flags: the snapshot's robot flags; turretAim: field-relative aim minus heading
     * (radians) or null; jammed: the shooter is jammed; now: ms.
     */
    animate(dt, v, omega, accel, flags, turretAim, jammed, now) {
      // Drive wheels roll with the ground; swerve modules steer each wheel along its own velocity (and take the
      // shorter way round, reversing the wheel instead of turning more than 90°); tank sides differ in a turn.
      const halfTrack = spec.frame / 2 - 0.045;
      if (parts.modules.length) {
        for (const module of parts.modules) {
          const forward = v.forward + omega * module.z;
          const left = v.left + omega * module.x;
          const speed = Math.hypot(forward, left);
          if (speed > 0.05) {
            let target = Math.atan2(left, forward);
            let diff = target - module.angle;
            diff = Math.atan2(Math.sin(diff), Math.cos(diff));
            module.direction = 1;
            if (Math.abs(diff) > Math.PI / 2) {
              diff -= Math.sign(diff) * Math.PI;
              module.direction = -1;
            }
            module.angle += Math.max(-dt * 14, Math.min(dt * 14, diff)); // steer motors are fast but not instant
            module.group.rotation.y = module.angle;
          }
          module.wheel.rotation.y -= module.direction * speed * dt / 0.051;
        }
      } else {
        for (const { wheel, radius, side } of parts.wheels) {
          const speed = v.forward + omega * halfTrack * side; // (side -1 = left, slower in a left turn)
          wheel.rotation.y -= speed * dt / radius;
        }
      }
      // Body lean: squat back under acceleration, dip forward braking, lean out in turns; a hard hit bounces it.
      const leanTarget = Math.max(-0.05, Math.min(0.05, accel.forward * 0.006)); // (+ = nose up)
      const rollTarget = Math.max(-0.04, Math.min(0.04, accel.left * 0.004));
      motion.lean += (leanTarget - motion.lean) * Math.min(1, dt * 10);
      motion.roll += (rollTarget - motion.roll) * Math.min(1, dt * 10);
      if (Math.hypot(accel.forward, accel.left) > 25 && now - motion.bounceAt > 300) motion.bounceAt = now;
      const since = (now - motion.bounceAt) / 1000;
      motion.bounce = motion.bounceAt > 0 && since < 0.6 ? 0.02 * Math.exp(-since * 7) * Math.sin(since * 40) : 0;
      body.position.y = motion.bounce;
      // Mechanisms: the indexer and feeder run while shooting (the indexer also creeps while intaking).
      const feeding = flags.shooting ? 1 : flags.intake ? 0.35 : 0;
      for (const roller of parts.indexer || []) roller.rotation.y -= feeding * dt * 22;
      if (parts.feeder && flags.shooting) parts.feeder.rotation.y -= dt * 40;
      // The turret slews toward its target at a turret's speed rather than snapping.
      if (parts.turret && turretAim !== null) {
        if (motion.turret === null) motion.turret = turretAim;
        let diff = turretAim - motion.turret;
        diff = Math.atan2(Math.sin(diff), Math.cos(diff));
        motion.turret += Math.max(-dt * 6, Math.min(dt * 6, diff));
        parts.turret.rotation.y = motion.turret;
      }
      // Smoke from a jammed shooter: a few puffs rising and fading.
      if (jammed && motion.smokeAt < 0) motion.smokeAt = now;
      if (!jammed) motion.smokeAt = -1;
      smoke.visible = motion.smokeAt >= 0;
      if (smoke.visible) {
        const t = (now - motion.smokeAt) / 1000;
        puffs.forEach((puff, i) => {
          const phase = (t * 0.8 + i / puffs.length) % 1;
          puff.position.set(-phase * 0.15, phase * 0.5, Math.sin(i * 2.1 + t) * 0.06);
          puff.material.opacity = 0.55 * (1 - phase);
        });
      }
      return motion;
    },
    // Without positions from the simulator (an old recording): the FUEL rests in layers.
    setHopperFuel(count) {
      const dummy = new THREE.Object3D();
      const n = Math.min(count, HOPPER_DRAWN);
      for (let i = 0; i < n; i++) {
        const slot = hopperRest(parts.hopperShape, i);
        dummy.position.set(slot.x, slot.up, -slot.left); // the robot's left is -z in its local frame
        dummy.updateMatrix();
        parts.hopperFuel.setMatrixAt(i, dummy.matrix);
      }
      parts.hopperFuel.count = n;
      parts.hopperFuel.instanceMatrix.needsUpdate = true;
    },
    // Where the simulator has each FUEL in the hopper: mm in the robot frame (x forward, y left, z up).
    setHopperPositions(mm) {
      const dummy = new THREE.Object3D();
      const n = Math.min(mm.length / 3, HOPPER_DRAWN);
      for (let i = 0; i < n; i++) {
        dummy.position.set(mm[i * 3] / 1000, mm[i * 3 + 2] / 1000, -mm[i * 3 + 1] / 1000);
        dummy.updateMatrix();
        parts.hopperFuel.setMatrixAt(i, dummy.matrix);
      }
      parts.hopperFuel.count = n;
      parts.hopperFuel.instanceMatrix.needsUpdate = true;
      robot.lastHopper = n;
    },
  };
  return robot;
}

/** Bumper fabric weave (a bump map): the cloth's over-under texture, about 1 cm a repeat. */
function weaveTexture() {
  if (weaveTexture.cached) return weaveTexture.cached;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 32;
  const g = canvas.getContext("2d");
  for (let y = 0; y < 32; y++) {
    for (let x = 0; x < 32; x++) {
      const over = (Math.floor(x / 4) + Math.floor(y / 4)) % 2 === 0;
      const along = over ? Math.sin((x % 4) / 4 * Math.PI) : Math.sin((y % 4) / 4 * Math.PI);
      const v = Math.floor(90 + 120 * along);
      g.fillStyle = `rgb(${v},${v},${v})`;
      g.fillRect(x, y, 1, 1);
    }
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.repeat.set(110, 110); // the extruded bumper's UVs are in meters
  weaveTexture.cached = texture;
  return texture;
}

/** Pre-drilled tube: one tile is 1/2 in along the tube by 1 in across (see box()) with a 0.2 in hole in the middle,
 * dark where the tube is hollow. */
function holeTexture() {
  if (holeTexture.cached) return holeTexture.cached;
  const canvas = document.createElement("canvas");
  canvas.width = 32;
  canvas.height = 64;
  const g = canvas.getContext("2d");
  g.fillStyle = "#ffffff";
  g.fillRect(0, 0, 32, 64);
  g.fillStyle = "#1e2024";
  g.beginPath();
  g.arc(16, 32, 6.5, 0, Math.PI * 2);
  g.fill();
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.anisotropy = 4;
  holeTexture.cached = texture;
  return texture;
}

/** Wheel tread grooves (a bump map): bands across the tread around the wheel. */
function treadTexture() {
  if (treadTexture.cached) return treadTexture.cached;
  const canvas = document.createElement("canvas");
  canvas.width = 128;
  canvas.height = 16;
  const g = canvas.getContext("2d");
  g.fillStyle = "#c8c8c8";
  g.fillRect(0, 0, 128, 16);
  g.fillStyle = "#202020";
  for (let x = 0; x < 128; x += 8) g.fillRect(x, 0, 3, 16);
  const texture = new THREE.CanvasTexture(canvas);
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  treadTexture.cached = texture;
  return texture;
}
