// Procedural 2026 REBUILT field built from layouts/field-2026.json.
// One alliance half is built in "blue frame" field coordinates; the red half is the same build rotated 180° about
// the field center (the 2026 field is rotationally symmetric).
import * as THREE from "three";
import {
  ctx, f2s, place, fieldBox, yawFromQuat, M, COLORS, CanvasPanel, carpetTexture, aprilTagTexture, tagPick,
  allianceColor, noiseTexture, towerLight,
} from "./util.js";
import { HubLeds, makeLens } from "./leds.js";
import { registerFigure } from "./people.js";
import { monitor } from "./environment.js";

const PIXELS = 8;

export async function loadLayout() {
  const response = await fetch("/layouts/field-2026.json");
  const layout = await response.json();
  ctx.L = layout.field.length;
  ctx.W = layout.field.width;
  return layout;
}

/** Builds the whole field. Returns { group, parts } where parts holds handles for live updates. */
// The official field CAD (imported by the user, cadfield.js) replaces the static structure built here when it is
// shown. markStructure tags what it replaces; the live parts (LEDs, lenses, doors, signs, buttons, screens, FUEL) stay.
function markStructure(group, keep = []) {
  for (const child of group.children) {
    if (!keep.includes(child) && !child.userData.keep) child.userData.cad = true;
  }
}

export function buildField(layout) {
  const group = new THREE.Group();
  group.name = "field";
  const parts = { hubs: {}, stations: {}, timers: {}, towers: {}, outposts: {}, tags: [], teamDisplays: {} };

  group.add(buildCarpet(layout));
  group.add(buildLines(layout));
  const guardrails = buildGuardrails(layout);
  guardrails.userData.cad = true;
  group.add(guardrails);

  for (const alliance of ["blue", "red"]) {
    const half = new THREE.Group();
    half.name = `${alliance}-half`;
    if (alliance === "red") half.rotation.y = Math.PI;
    buildHalf(layout, alliance, half, parts);
    group.add(half);
  }
  for (const tag of layout.aprilTags) {
    const mesh = buildTag(tag, layout.aprilTagSize);
    parts.tags.push(mesh);
    group.add(mesh);
  }
  return { group, parts };
}

function buildCarpet(layout) {
  const { length: L, width: W } = layout.field;
  const carpet = new THREE.Mesh(
    new THREE.PlaneGeometry(L, W),
    new THREE.MeshStandardMaterial({ color: 0xffffff, map: carpetTexture(), bumpMap: carpetTexture(), bumpScale: 0.8,
      roughness: 0.97, metalness: 0 }),
  );
  carpet.rotation.x = -Math.PI / 2;
  carpet.receiveShadow = true;
  carpet.name = "carpet";
  tagPick(carpet, { kind: "carpet" });
  return carpet;
}

function tapeStrip(x0, y0, x1, y1, width, color) {
  const length = Math.hypot(x1 - x0, y1 - y0);
  const mesh = new THREE.Mesh(
    new THREE.PlaneGeometry(length, width),
    new THREE.MeshStandardMaterial({ color, roughness: 0.6, polygonOffset: true, polygonOffsetFactor: -2 }),
  );
  mesh.rotation.x = -Math.PI / 2;
  mesh.rotation.z = Math.atan2(y1 - y0, x1 - x0);
  mesh.position.copy(f2s((x0 + x1) / 2, (y0 + y1) / 2, 0.002));
  mesh.receiveShadow = true;
  return mesh;
}

function buildLines(layout) {
  const { length: L, width: W } = layout.field;
  const tape = layout.zones.tapeWidth;
  const group = new THREE.Group();
  group.name = "tape";
  group.add(tapeStrip(L / 2, 0, L / 2, W, tape, 0xf4f4f4));
  const start = layout.zones.allianceZoneDepth;
  group.add(tapeStrip(start, 0, start, W, tape, COLORS.blue));
  group.add(tapeStrip(L - start, 0, L - start, W, tape, COLORS.red));
  // Neutral-zone fuel staging outline (removed before the match on a real field; shown faintly here).
  const box = layout.fuel.neutralBox;
  const x0 = box.centerX - box.lengthX / 2;
  const x1 = box.centerX + box.lengthX / 2;
  const y0 = box.centerY - box.widthY / 2;
  const y1 = box.centerY + box.widthY / 2;
  for (const [a, b, c, d] of [[x0, y0, x1, y0], [x0, y1, x1, y1], [x0, y0, x0, y1], [x1, y0, x1, y1]]) {
    const strip = tapeStrip(a, b, c, d, 0.02, 0x9aa3ad);
    strip.material.transparent = true;
    strip.material.opacity = 0.35;
    group.add(strip);
  }
  return group;
}

function buildGuardrails(layout) {
  const { length: L, width: W } = layout.field;
  const height = layout.guardrail.height;
  const group = new THREE.Group();
  group.name = "guardrails";
  for (const y of [0, W]) {
    const sign = y === 0 ? -1 : 1;
    const glass = fieldBox(L, 0.012, height - 0.06, M.glass());
    glass.castShadow = false;
    place(glass, L / 2, y + sign * 0.02, (height - 0.06) / 2 + 0.03);
    group.add(glass);
    for (const z of [0.02, height - 0.02]) {
      const rail = fieldBox(L, 0.045, 0.045, M.alu());
      place(rail, L / 2, y + sign * 0.02, z);
      group.add(rail);
    }
    for (let x = 0; x <= L + 0.01; x += L / 9) {
      const post = fieldBox(0.045, 0.06, height, M.alu());
      place(post, x, y + sign * 0.03, height / 2);
      group.add(post);
    }
    for (const [gateX, side] of layout.guardrail.gates) {
      if ((side === "low") !== (y === 0)) continue;
      const latch = fieldBox(0.06, 0.08, 0.12, new THREE.MeshStandardMaterial({ color: 0xf2c200, roughness: 0.5 }));
      place(latch, gateX, y + sign * 0.06, height - 0.1);
      group.add(latch);
    }
  }
  return group;
}

function buildTag(tag, size) {
  const yaw = yawFromQuat(tag.q);
  const holder = new THREE.Group();
  const backing = new THREE.Mesh(new THREE.PlaneGeometry(0.2667, 0.2667), M.white());
  const face = new THREE.Mesh(
    new THREE.PlaneGeometry(size * 10 / 8, size * 10 / 8),
    new THREE.MeshBasicMaterial({ map: aprilTagTexture(tag.id) }),
  );
  face.position.z = 0.001;
  holder.add(backing, face);
  holder.position.copy(f2s(tag.x, tag.y, tag.z));
  holder.position.add(new THREE.Vector3(Math.cos(yaw), 0, -Math.sin(yaw)).multiplyScalar(0.004));
  holder.rotation.y = yaw + Math.PI / 2;
  tagPick(face, { kind: "apriltag", id: tag.id, label: `AprilTag ${tag.id}` });
  holder.name = `tag-${tag.id}`;
  return holder;
}

// ---------------------------------------------------------------------------------------------------------------
// One alliance's half (blue-frame coordinates)
// ---------------------------------------------------------------------------------------------------------------

function buildHalf(layout, alliance, half, parts) {
  parts.hubs[alliance] = buildHub(layout, alliance, half);
  buildBumps(layout, alliance, half);
  buildTrenches(layout, alliance, half);
  parts.towers[alliance] = buildTower(layout, alliance, half);
  parts.outposts[alliance] = buildOutpost(layout, alliance, half);
  buildDepot(layout, alliance, half);
  buildDriverStations(layout, alliance, half, parts);
}

function buildHub(layout, alliance, half) {
  // Modeled on FIRST drawing GE-26300 (HUB, sheets 1–4): a 47 in square body with clear side panels to 49.75 in,
  // a sloped roof up to the clear hex hopper whose front edge is at 72 in, the FUEL exit slot under the AprilTag band
  // on the neutral-zone face, and the aesthetic net leaning out to 120 in over the neutral side.
  const hub = layout.hub;
  const exit = hub.exit || { bottom: 0.765, top: 0.98, lanes: [-0.42, -0.14, 0.14, 0.42], deflectorDepth: 0.145 };
  const net = hub.net || { bottom: hub.bodyHeight, meshBottom: 1.65, top: 3.057, width: 1.484, overhang: 0.261 };
  const roofHeight = hub.roofHeight || hub.bodyHeight + 0.2;
  const [cx, cy] = hub.positions.blue;
  const s = hub.size;
  const g = new THREE.Group();
  g.name = `${alliance}-hub`;
  place(g, cx, cy, 0);
  half.add(g);
  // In the hub's local frame: +x faces the neutral zone (center), -x faces this alliance's driver stations.
  const local = (x, y, z) => new THREE.Vector3(x, z, -y);
  const box = (dx, dy, dz, material, x, y, z) => {
    const mesh = fieldBox(dx, dy, dz, material);
    mesh.position.copy(local(x, y, z));
    g.add(mesh);
    return mesh;
  };
  const body = hub.bodyHeight;
  const polycarb = new THREE.MeshStandardMaterial({ color: 0x8c98a4, roughness: 0.15, metalness: 0, transparent: true,
    opacity: 0.14, depthWrite: false, side: THREE.DoubleSide, envMapIntensity: 0.2 });
  const white = M.white();

  // Base frame (GE-26301) and corner posts.
  box(s, s, 0.06, M.darkAlu(), 0, 0, 0.03);
  for (const [px, py] of [[-1, -1], [-1, 1], [1, -1], [1, 1]]) {
    box(0.045, 0.045, body - 0.06, M.alu(), px * (s / 2 - 0.022), py * (s / 2 - 0.022), 0.06 + (body - 0.06) / 2);
  }
  // Alliance-side and side panels: clear polycarbonate, so the FUEL falling through and rolling down the ramp inside
  // (internal/world/hubflow.go) shows the whole way.
  for (const [nx, ny] of [[-1, 0], [0, 1], [0, -1]]) {
    const w = s - 0.01;
    box(nx ? 0.012 : w, ny ? 0.012 : w, body - 0.06, polycarb, nx * (s / 2 - 0.006), ny * (s / 2 - 0.006), 0.06 + (body - 0.06) / 2);
  }
  // Neutral-zone face: clear lower panel, the exit slot, then the clear band (carrying the AprilTags) up to the roof.
  box(0.01, s - 0.02, exit.bottom - 0.06, polycarb, s / 2 - 0.005, 0, 0.06 + (exit.bottom - 0.06) / 2);
  // Hub brace assemblies (GE-26302) seen through the clear panel.
  for (const z of [0.3, 0.56]) box(0.04, s - 0.06, 0.04, M.alu(), s / 2 - 0.05, 0, z);
  box(0.012, s - 0.02, body - exit.top, polycarb, s / 2 - 0.006, 0, exit.top + (body - exit.top) / 2);
  // Inside the body (internal/world/hubflow.go): the ramp FUEL rolls down from the throat to the exit slot, the lane
  // dividers in front of the exits. Then the small ramp lip (GE-26332) and the fuel counters (GE-26400) per lane.
  const rampRise = 0.35;
  const inner = s - 0.06;
  const rampTilt = new THREE.Group(); // high at the back wall, down to the slot at the neutral-zone face
  rampTilt.position.copy(local(0, 0, exit.bottom + rampRise / 2));
  rampTilt.rotation.z = -Math.atan2(rampRise, inner);
  const ramp = new THREE.Mesh(new THREE.PlaneGeometry(Math.hypot(inner, rampRise), s - 0.08),
    new THREE.MeshStandardMaterial({ color: 0xd9dde2, roughness: 0.6, side: THREE.DoubleSide }));
  ramp.rotation.x = -Math.PI / 2;
  rampTilt.add(ramp);
  g.add(rampTilt);
  const sortedLanes = [...exit.lanes].sort((a, b) => a - b);
  for (let k = 1; k < sortedLanes.length; k++) {
    const v = (sortedLanes[k - 1] + sortedLanes[k]) / 2;
    box(0.42, 0.006, 0.2, M.darkAlu(), s / 2 - 0.03 - 0.21, v, exit.bottom + 0.1 + 0.42 * rampRise / inner / 2);
  }
  const lip = box(0.09, s - 0.04, 0.012, white, s / 2 + 0.02, 0, exit.bottom + 0.004);
  lip.rotation.z = -0.25;
  const exits = [];
  for (const lane of exit.lanes) {
    const counter = box(0.08, 0.2, 0.05, M.darkAlu(), s / 2 - 0.05, lane, exit.top - 0.04);
    const roller = new THREE.Mesh(new THREE.CylinderGeometry(0.022, 0.022, 0.2, 10), M.alliance(alliance));
    roller.rotation.x = Math.PI / 2;
    roller.position.copy(local(s / 2 - 0.02, lane, exit.top - 0.075));
    g.add(roller);
    exits.push({ exit: counter, roller });
  }
  // Deflectors at both ends of the slot turn the outer lanes' FUEL inward.
  for (const sy of [-1, 1]) {
    const deflector = box(exit.deflectorDepth, 0.012, exit.top - exit.bottom, M.darkAlu(),
      s / 2 + exit.deflectorDepth / 2, sy * (s / 2 - 0.04), (exit.bottom + exit.top) / 2);
    deflector.rotation.y = sy * 0.45;
  }
  // Top frame rim and the sloped roof (front panels / diffusers) up to the hopper.
  box(s + 0.01, s + 0.01, 0.03, M.darkAlu(), 0, 0, body);
  const throatRadius = (hub.throatDiameter || 0.61) / 2;
  const hexRadius = hub.openingDiameter / 2 / Math.cos(Math.PI / 6);
  const roof = new THREE.Mesh(new THREE.CylinderGeometry(throatRadius / Math.cos(Math.PI / 6) * 1.15, s / Math.SQRT2,
    roofHeight - body, 4, 1, true), polycarb); // clear, like the rest of the hub
  roof.rotation.y = Math.PI / 4;
  roof.position.copy(local(0, 0, body + (roofHeight - body) / 2));
  g.add(roof);
  // The roof panel (alliance colored) around the hex throat.
  const roofPanel = new THREE.Mesh(new THREE.RingGeometry(throatRadius, throatRadius * 1.55, 6, 1), M.alliance(alliance));
  roofPanel.rotation.x = -Math.PI / 2;
  roofPanel.rotation.z = Math.PI / 6;
  roofPanel.position.copy(local(0, 0, roofHeight + 0.003));
  g.add(roofPanel);
  // Clear hex hopper (GE-26329 funnel sides): from the throat at the roof up to the 41.7 in opening at 72 in.
  const hopper = new THREE.Mesh(new THREE.CylinderGeometry(hexRadius, throatRadius / Math.cos(Math.PI / 6),
    hub.openingHeight - roofHeight, 6, 1, true), polycarb);
  hopper.position.copy(local(-0.05, 0, roofHeight + (hub.openingHeight - roofHeight) / 2));
  hopper.rotation.y = Math.PI / 6;
  g.add(hopper);
  const hopperEdges = new THREE.LineSegments(new THREE.EdgesGeometry(hopper.geometry),
    new THREE.LineBasicMaterial({ color: 0x9aa3ad }));
  hopperEdges.position.copy(hopper.position);
  hopperEdges.rotation.copy(hopper.rotation);
  g.add(hopperEdges);
  const rim = new THREE.Mesh(new THREE.TorusGeometry(hexRadius, 0.012, 4, 6), M.alu());
  rim.rotation.x = Math.PI / 2;
  rim.rotation.z = Math.PI / 6;
  rim.position.copy(local(-0.05, 0, hub.openingHeight));
  g.add(rim);
  tagPick(hopper, { kind: "hub", alliance, label: `${alliance === "red" ? "Red" : "Blue"} hub` });
  tagPick(roof, { kind: "hub", alliance, label: `${alliance === "red" ? "Red" : "Blue"} hub` });

  // Net (GE-26318) on its pole frame: it leans out over the neutral zone to 120 in and is 58.4 in wide; a U-shaped
  // bottom pole runs back around the hopper at about 65 in.
  const netBase = s / 2;
  const netHeight = net.top - net.meshBottom;
  const lean = Math.atan2(net.overhang, netHeight);
  const netLength = Math.hypot(net.overhang, netHeight);
  const mesh = new THREE.Mesh(new THREE.PlaneGeometry(net.width, netLength),
    new THREE.MeshStandardMaterial({ map: netTexture(), transparent: true, side: THREE.DoubleSide, depthWrite: false, roughness: 0.9 }));
  mesh.position.copy(local(netBase + net.overhang / 2, 0, net.meshBottom + netHeight / 2));
  mesh.rotation.y = Math.PI / 2;
  mesh.rotateX(-lean);
  g.add(mesh);
  const pole = (from, to) => {
    const a = local(...from);
    const b = local(...to);
    const tube = new THREE.Mesh(new THREE.CylinderGeometry(0.019, 0.019, a.distanceTo(b), 8), M.darkAlu());
    tube.position.copy(a).add(b).multiplyScalar(0.5);
    tube.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), b.clone().sub(a).normalize());
    tube.castShadow = true;
    g.add(tube);
  };
  const halfNet = net.width / 2;
  for (const sy of [-1, 1]) {
    pole([netBase, sy * (s / 2), body], [netBase + net.overhang, sy * halfNet, net.top]); // long poles
    pole([0.05, sy * halfNet, net.meshBottom], [netBase + 0.02, sy * halfNet, net.meshBottom]); // U loop sides
    pole([netBase, sy * (s / 2 - 0.1), body], [0.05, sy * halfNet, net.meshBottom]); // short braces
  }
  pole([netBase + net.overhang, -halfNet, net.top], [netBase + net.overhang, halfNet, net.top]); // top pole
  pole([0.05, -halfNet, net.meshBottom], [0.05, halfNet, net.meshBottom]); // U loop back
  pole([netBase + 0.02, -halfNet, net.meshBottom], [netBase + 0.02, halfNet, net.meshBottom]);

  // DMX light bars on the top angles of the hub: 4 sides × (lower, upper) × 8 pixels, in fixture order side1..side4.
  // Side 1 faces this alliance's driver stations (-x), side 2 -y, side 3 the center (+x), side 4 +y.
  const pixelGeometry = new THREE.BoxGeometry(0.012, 0.03, 0.05);
  const pixelMaterial = new THREE.MeshBasicMaterial({ color: 0xffffff });
  const leds = new HubLeds();
  const housingMaterial = new THREE.MeshStandardMaterial({ color: 0x2b2f36, metalness: 0.7, roughness: 0.4 });
  const pixels = new THREE.InstancedMesh(pixelGeometry, pixelMaterial, 4 * 2 * PIXELS);
  pixels.instanceColor = new THREE.InstancedBufferAttribute(new Float32Array(4 * 2 * PIXELS * 3), 3);
  const sideNormals = [[-1, 0], [0, -1], [1, 0], [0, 1]];
  const dummy = new THREE.Object3D();
  let index = 0;
  for (let side = 0; side < 4; side++) {
    const [nx, ny] = sideNormals[side];
    for (let row = 0; row < 2; row++) {
      const z = row === 0 ? body + 0.035 : roofHeight - 0.03;
      const halfSpan = row === 0 ? s / 2 - 0.08 : throatRadius * 1.3;
      const outward = row === 0 ? s / 2 + 0.02 : throatRadius * 1.55;
      const along3 = local(ny, -nx, 0).normalize();
      const normal3 = local(nx, ny, 0).normalize();
      const housing = new THREE.Mesh(new THREE.BoxGeometry(2 * halfSpan + 0.06, 0.06, 0.035), housingMaterial);
      housing.quaternion.setFromRotationMatrix(new THREE.Matrix4().makeBasis(along3.clone().negate(), new THREE.Vector3(0, 1, 0), normal3));
      housing.position.copy(local(nx * (outward - 0.012), ny * (outward - 0.012), z));
      g.add(housing);
      const lens = makeLens(leds, side * 2 + row, 2 * halfSpan + 0.03, local(nx * (outward + 0.012), ny * (outward + 0.012), z), along3, normal3);
      lens.userData.keep = true;
      g.add(lens);
      for (let p = 0; p < PIXELS; p++) {
        const t = -halfSpan + ((p + 0.5) * 2 * halfSpan) / PIXELS;
        // Pixels run left to right as seen from outside the face.
        const along = [ny, -nx];
        dummy.position.copy(local(nx * outward + along[0] * t, ny * outward + along[1] * t, z));
        dummy.rotation.set(0, Math.atan2(ny, nx), 0);
        dummy.updateMatrix();
        pixels.setMatrixAt(index, dummy.matrix);
        pixels.setColorAt(index, new THREE.Color(0x111111));
        index++;
      }
    }
  }
  pixels.instanceMatrix.needsUpdate = true;
  g.add(pixels);
  leds.pixels = pixels;
  // Where each side's light spill comes from (the quality settings decide whether to add lights there).
  leds.sides = sideNormals.map(([nx, ny]) => ({
    center: local(nx * (s / 2 + 0.15), ny * (s / 2 + 0.15), (body + roofHeight) / 2),
    normal: local(nx, ny, 0).normalize(),
    width: s,
  }));
  leds.group = g;

  // Hub light (PLC coil) glow inside the hopper.
  const glow = new THREE.PointLight(allianceColor(alliance), 0, 4, 2);
  glow.position.copy(local(0, 0, roofHeight + 0.15));
  g.add(glow);
  const glowDisc = new THREE.Mesh(new THREE.CircleGeometry(throatRadius * 0.95, 6),
    new THREE.MeshBasicMaterial({ color: allianceColor(alliance), transparent: true, opacity: 0 }));
  glowDisc.rotation.x = -Math.PI / 2;
  glowDisc.position.copy(local(0, 0, roofHeight - 0.01));
  g.add(glowDisc);
  markStructure(g, [pixels, glow, glowDisc, ...exits.map((e) => e.roller)]);

  return { group: g, pixels, leds, glow, glowDisc, exits, local };
}

export function netTexture() {
  const size = 128;
  const c = document.createElement("canvas");
  c.width = c.height = size;
  const g = c.getContext("2d");
  g.strokeStyle = "rgba(20,20,20,0.85)";
  g.lineWidth = 2;
  for (let i = 0; i <= size; i += 12) {
    g.beginPath(); g.moveTo(i, 0); g.lineTo(i, size); g.stroke();
    g.beginPath(); g.moveTo(0, i); g.lineTo(size, i); g.stroke();
  }
  const texture = new THREE.CanvasTexture(c);
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.repeat.set(6, 4);
  return texture;
}

function buildBumps(layout, alliance, half) {
  const { width, depth, height } = layout.bump;
  const [cx, cy] = layout.hub.positions.blue;
  const offset = layout.hub.size / 2 + width / 2;
  const shape = new THREE.Shape();
  const run = depth / 2;
  const flat = 0.06;
  shape.moveTo(-run, 0);
  shape.lineTo(-flat, height);
  shape.lineTo(flat, height);
  shape.lineTo(run, 0);
  shape.lineTo(-run, 0);
  const geometry = new THREE.ExtrudeGeometry(shape, { depth: width, bevelEnabled: false });
  geometry.translate(0, 0, -width / 2);
  for (const y of [cy - offset, cy + offset]) {
    const bump = new THREE.Mesh(geometry, M.hdpe(alliance));
    bump.castShadow = true;
    bump.receiveShadow = true;
    place(bump, cx, y, 0);
    tagPick(bump, { kind: "bump", alliance, label: "Bump" });
    bump.userData.cad = true;
    half.add(bump);
  }
}

/** Trenches: a thin (3 in) frame across the field on the hub line, matching internal/world/world.go buildStatics.
 * Two legs and the panel over the 50.34 in x 22.25 in opening, which carries the AprilTags on both faces and reaches
 * 40.25 in. The opening is centered on the tags, so it runs from the guardrail inward and the outer leg stands
 * behind the rail. */
function buildTrenches(layout, alliance, half) {
  const t = layout.trench;
  const [cx] = layout.hub.positions.blue;
  const W = layout.field.width;
  const leg = (t.width - t.clearWidth) / 2;
  const thick = t.thickness || 0.075;
  const centerY = t.openingCenterY || 0.6445;
  for (const center of [centerY, W - centerY]) {
    const group = new THREE.Group();
    const open0 = center - t.clearWidth / 2;
    const open1 = center + t.clearWidth / 2;
    for (const ly of [open0 - leg / 2, open1 + leg / 2]) {
      const legMesh = fieldBox(thick, leg, t.clearHeight, M.alliance(alliance));
      place(legMesh, cx, ly, t.clearHeight / 2);
      group.add(legMesh);
    }
    const panelHeight = t.height - t.clearHeight;
    const panel = fieldBox(thick, t.width, panelHeight, M.alliance(alliance));
    place(panel, cx, center, t.clearHeight + panelHeight / 2);
    tagPick(panel, { kind: "trench", alliance, label: "Trench (22.25 in clearance)" });
    group.add(panel);
    // Aluminum edging along the bottom of the panel (the edge robots duck under) and its top.
    for (const z of [t.clearHeight + 0.01, t.height - 0.01]) {
      const edge = fieldBox(thick + 0.01, t.width + 0.01, 0.02, M.darkAlu());
      place(edge, cx, center, z);
      group.add(edge);
    }
    group.userData.cad = true;
    half.add(group);
  }
}

function buildTower(layout, alliance, half) {
  const t = layout.tower;
  const cy = t.centerY.blue;
  const group = new THREE.Group();
  group.name = `${alliance}-tower`;
  const colored = M.alliance(alliance);

  const wall = fieldBox(0.05, t.width, t.height, M.diamond());
  place(wall, -0.025, cy, t.height / 2);
  group.add(wall);
  const base = fieldBox(t.baseDepth, t.baseWidth, 0.008, M.steel());
  place(base, t.baseDepth / 2, cy, 0.004);
  group.add(base);
  const uprights = [];
  for (const sign of [-1, 1]) {
    const y = cy + sign * (t.uprightSpacing / 2 + t.uprightThickness / 2);
    const upright = fieldBox(t.uprightDepth, t.uprightThickness, t.uprightHeight, colored);
    place(upright, t.uprightSetback, y, t.uprightHeight / 2);
    group.add(upright);
    uprights.push(upright);
    for (const z of [0.72, 1.1]) {
      const brace = fieldBox(t.uprightSetback, 0.03, 0.04, colored);
      place(brace, t.uprightSetback / 2, y, z);
      group.add(brace);
    }
  }
  const rungLength = t.uprightSpacing + 2 * t.uprightThickness + 2 * t.rungOverhang;
  const rungs = {};
  for (const [name, z] of Object.entries(t.rungs)) {
    const rung = new THREE.Mesh(new THREE.CylinderGeometry(t.rungDiameter / 2, t.rungDiameter / 2, rungLength, 12), colored);
    rung.rotation.x = Math.PI / 2;
    place(rung, t.uprightSetback, cy, z);
    rung.rotation.x = Math.PI / 2;
    rung.castShadow = true;
    tagPick(rung, { kind: "rung", alliance, level: name, label: `Tower ${name.toUpperCase()} rung (${Math.round(z / 0.0254)} in)` });
    group.add(rung);
    rungs[name] = rung;
  }
  markStructure(group);
  half.add(group);
  return { group, rungs, center: [t.uprightSetback, cy] };
}

function buildOutpost(layout, alliance, half) {
  const o = layout.outpost;
  const [y0, y1] = o.range.blue;
  const wallHeight = 2.0;
  const width = y1 - y0;
  const cy = (y0 + y1) / 2;
  const group = new THREE.Group();
  group.name = `${alliance}-outpost`;

  const panel = fieldBox(0.05, width, wallHeight, M.diamond());
  place(panel, -0.025, cy, wallHeight / 2);
  group.add(panel);
  const trim = fieldBox(0.055, width, 0.08, M.alliance(alliance));
  place(trim, -0.025, cy, wallHeight - 0.04);
  group.add(trim);
  // Openings (dark recesses): chute (upper) and floor opening (lower), divided by a vertical pipe.
  const chuteY = o.openingY || cy + 0.25;
  const chuteOpening = fieldBox(0.056, o.chuteOpening.width, o.chuteOpening.height, new THREE.MeshBasicMaterial({ color: 0x050607 }));
  place(chuteOpening, -0.025, chuteY, o.chuteOpening.bottom + o.chuteOpening.height / 2);
  group.add(chuteOpening);
  const floorOpening = fieldBox(0.056, o.floorOpening.width, o.floorOpening.height, new THREE.MeshBasicMaterial({ color: 0x050607 }));
  place(floorOpening, -0.025, chuteY, o.floorOpening.bottom + o.floorOpening.height / 2);
  group.add(floorOpening);
  const divider = new THREE.Mesh(new THREE.CylinderGeometry(0.021, 0.021, o.floorOpening.height, 8), M.alu());
  place(divider, 0, chuteY, o.floorOpening.bottom + o.floorOpening.height / 2);
  group.add(divider);
  // Chute door (rotates ~90° to release fuel).
  const door = fieldBox(0.012, o.chuteOpening.width, o.chuteOpening.height + 0.02,
    new THREE.MeshStandardMaterial({ color: 0xe8e8e8, roughness: 0.6 }));
  const doorPivot = new THREE.Group();
  place(doorPivot, 0.012, chuteY, o.chuteOpening.bottom + o.chuteOpening.height);
  door.position.set(0, -(o.chuteOpening.height + 0.02) / 2, 0);
  doorPivot.add(door);
  group.add(doorPivot);
  tagPick(door, { kind: "chute", alliance, label: "Outpost chute door" });

  // Sloped chute behind the wall (15°) and the corral on the human-player side.
  const chuteLength = 1.2;
  const chute = fieldBox(chuteLength, o.chuteOpening.width + 0.06, 0.02, M.steel());
  chute.rotation.z = THREE.MathUtils.degToRad(15);
  place(chute, -chuteLength / 2 * Math.cos(0.26) - 0.05, chuteY, o.chuteOpening.bottom + chuteLength / 2 * Math.sin(0.26));
  chute.rotation.z = THREE.MathUtils.degToRad(15);
  group.add(chute);
  const corral = new THREE.Group();
  const c = o.corral;
  for (const [x, y, dx, dy] of [
    [-c.depth / 2, chuteY - c.width / 2, c.depth, 0.01], [-c.depth / 2, chuteY + c.width / 2, c.depth, 0.01],
    [-c.depth, chuteY, 0.01, c.width],
  ]) {
    const wall = fieldBox(dx, dy, c.wallHeight, M.glass());
    place(wall, x, y, c.wallHeight / 2);
    corral.add(wall);
  }
  group.add(corral);

  // FUEL waiting on the chute (rows of 4 across, rolling down toward the door) and piled in the corral.
  const ball = new THREE.SphereGeometry(0.075, 12, 8);
  const chuteBalls = new THREE.InstancedMesh(ball, M.fuel(), o.chuteCapacity || 24);
  const dummy = new THREE.Object3D();
  for (let i = 0; i < chuteBalls.count; i++) {
    const row = Math.floor(i / 4);
    const along = 0.1 + row * 0.155;
    dummy.position.set(0, 0, 0);
    place(dummy, -0.06 - along * Math.cos(0.26), chuteY - 0.24 + (i % 4) * 0.16, o.chuteOpening.bottom + 0.085 + along * Math.sin(0.26));
    dummy.updateMatrix();
    chuteBalls.setMatrixAt(i, dummy.matrix);
  }
  chuteBalls.count = 0;
  chuteBalls.castShadow = true;
  group.add(chuteBalls);
  const corralMax = 60;
  const corralBalls = new THREE.InstancedMesh(ball, M.fuel(), corralMax);
  for (let i = 0; i < corralMax; i++) {
    const layer = Math.floor(i / 30);
    const k = i % 30;
    const jitter = ((i * 37) % 11) / 110;
    place(dummy, -0.08 - (k % 6) * 0.155 - layer * 0.07 - jitter * 0.3, chuteY - 0.37 + Math.floor(k / 6) * 0.17 + layer * 0.07,
      0.075 + layer * 0.13);
    dummy.updateMatrix();
    corralBalls.setMatrixAt(i, dummy.matrix);
  }
  corralBalls.count = 0;
  corralBalls.castShadow = true;
  group.add(corralBalls);

  // The human player, standing behind the corral (an alliance-coloured shirt, the throwing arm animated).
  const human = new THREE.Group();
  const shirt = new THREE.MeshStandardMaterial({ color: alliance === "red" ? 0xc8343a : 0x2a64c8, roughness: 0.8 });
  const skin = new THREE.MeshStandardMaterial({ color: 0xd9a07a, roughness: 0.7 });
  const pants = new THREE.MeshStandardMaterial({ color: 0x24262b, roughness: 0.9 });
  const legs = new THREE.Mesh(new THREE.CapsuleGeometry(0.13, 0.62, 4, 8), pants);
  legs.position.y = 0.45;
  const torso = new THREE.Mesh(new THREE.CapsuleGeometry(0.19, 0.38, 4, 10), shirt);
  torso.position.y = 1.18;
  const head = new THREE.Mesh(new THREE.SphereGeometry(0.11, 14, 10), skin);
  head.position.y = 1.63;
  const shoulder = new THREE.Group();
  shoulder.position.set(0, 1.42, 0.22);
  const arm = new THREE.Mesh(new THREE.CapsuleGeometry(0.05, 0.5, 4, 8), shirt);
  arm.position.y = -0.3;
  shoulder.add(arm);
  const heldBall = new THREE.Mesh(ball, M.fuel());
  heldBall.position.y = -0.6;
  shoulder.add(heldBall);
  const otherArm = new THREE.Mesh(new THREE.CapsuleGeometry(0.05, 0.5, 4, 8), shirt);
  otherArm.position.set(0, 1.12, -0.22);
  for (const part of [legs, torso, head, arm, otherArm]) {
    part.castShadow = true;
    part.userData.body = true; // (the capsule body: hidden once the rigged human takes over, people.js)
  }
  human.add(legs, torso, head, shoulder, otherArm);
  // Face the field (+x in the blue frame).
  human.rotation.y = 0;
  place(human, -1.35, chuteY, 0);
  human.rotation.y = 0;
  group.add(human);
  tagPick(torso, { kind: "humanPlayer", alliance, label: `${alliance === "red" ? "Red" : "Blue"} human player` });
  registerFigure({ root: human, right: shoulder, look: { shirt: alliance === "red" ? 0xc8343a : 0x2a64c8, skin: 0xd9a07a } });

  markStructure(group, [doorPivot, chuteBalls, corralBalls, human]);
  half.add(group);
  return { group, doorPivot, chuteY, chuteBalls, corralBalls, human: { root: human, shoulder, heldBall, throwAt: -1e9 } };
}

function buildDepot(layout, alliance, half) {
  const d = layout.depot;
  const cy = d.centerY.blue;
  const group = new THREE.Group();
  const barrier = new THREE.MeshStandardMaterial({ color: 0x6b7178, metalness: 0.7, roughness: 0.4 });
  for (const [x, y, dx, dy] of [
    [d.depth / 2, cy - d.width / 2, d.depth, d.barrierWidth],
    [d.depth / 2, cy + d.width / 2, d.depth, d.barrierWidth],
    [d.depth, cy, d.barrierWidth, d.width + d.barrierWidth],
  ]) {
    const strip = fieldBox(dx, dy, d.barrierHeight, barrier);
    place(strip, x, y, d.barrierHeight / 2);
    strip.userData.cad = true;
    group.add(strip);
  }
  const floor = new THREE.Mesh(new THREE.PlaneGeometry(d.depth, d.width),
    new THREE.MeshStandardMaterial({ color: allianceColor(alliance), transparent: true, opacity: 0.18, roughness: 1 }));
  floor.rotation.x = -Math.PI / 2;
  floor.position.copy(f2s(d.depth / 2, cy, 0.003));
  tagPick(floor, { kind: "depot", alliance, label: "Depot" });
  group.add(floor);
  half.add(group);
}

function buildDriverStations(layout, alliance, half, parts) {
  const ds = layout.driverStation;
  const prefix = alliance === "red" ? "R" : "B";
  const towerY0 = layout.tower.centerY.blue - layout.tower.width / 2;
  const towerY1 = layout.tower.centerY.blue + layout.tower.width / 2;
  const outpostEnd = layout.outpost.range.blue[1];
  const W = layout.field.width;
  const bounds = { 3: [outpostEnd, towerY0], 2: [towerY1, (towerY1 + W) / 2], 1: [(towerY1 + W) / 2, W] };

  for (const number of [1, 2, 3]) {
    const name = prefix + number;
    const [y0, y1] = bounds[number];
    const width = y1 - y0;
    const cy = (y0 + y1) / 2;
    const group = new THREE.Group();
    group.name = `station-${name}`;

    const base = fieldBox(0.05, width, ds.baseHeight, M.diamond());
    place(base, -0.025, cy, ds.baseHeight / 2);
    group.add(base);
    const glass = fieldBox(0.012, width - 0.05, ds.glassHeight, M.glass());
    glass.castShadow = false;
    place(glass, -0.01, cy, ds.baseHeight + ds.glassHeight / 2);
    group.add(glass);
    const rail = fieldBox(0.06, width, 0.04, M.alu());
    place(rail, -0.02, cy, ds.baseHeight + ds.glassHeight + 0.02);
    group.add(rail);
    for (const py of [y0 + 0.025, y1 - 0.025]) {
      const post = fieldBox(0.05, 0.05, ds.baseHeight + ds.glassHeight + 0.04, M.alu());
      place(post, -0.025, py, (ds.baseHeight + ds.glassHeight + 0.04) / 2);
      group.add(post);
    }
    const sponsor = fieldBox(0.052, width - 0.06, 0.152, M.alliance(alliance));
    place(sponsor, -0.024, cy, 1.09 - 0.076);
    group.add(sponsor);

    // Shelf on the drivers' side, with the laptop, E-stop (drivers' left) and A-stop (drivers' right).
    const shelf = fieldBox(ds.shelfDepth, width - 0.06, 0.03, M.darkAlu());
    place(shelf, -0.05 - ds.shelfDepth / 2, cy, ds.shelfHeight);
    group.add(shelf);
    const leftY = cy + width / 2 - 0.16; // blue drivers face +x, so their left is +y
    const rightY = cy - width / 2 + 0.16;

    const eStop = buildStopButton(0xd31f26, 0xf3c400, "E");
    place(eStop.group, -0.05 - ds.shelfDepth / 2, leftY, ds.shelfHeight + 0.015);
    tagPick(eStop.cap, { kind: "estop", station: name, label: `${name} E-stop` });
    group.add(eStop.group);
    const aStop = buildStopButton(0x1b1d22, 0x2b2f36, "A");
    place(aStop.group, -0.05 - ds.shelfDepth / 2, rightY, ds.shelfHeight + 0.015);
    tagPick(aStop.cap, { kind: "astop", station: name, label: `${name} A-stop` });
    group.add(aStop.group);

    // DS laptop showing a live driver station screen.
    const laptop = new THREE.Group();
    const laptopBase = fieldBox(0.24, 0.34, 0.02, M.black());
    laptopBase.position.set(0, 0.01, 0);
    laptop.add(laptopBase);
    const screenPanel = new CanvasPanel(512, 320);
    screenPanel.text("Driver Station", { fg: "#888", bg: "#1f1f1f", font: "bold 40px 'Segoe UI'" });
    const screen = new THREE.Mesh(new THREE.PlaneGeometry(0.34, 0.21), new THREE.MeshBasicMaterial({ map: screenPanel.texture }));
    const lid = new THREE.Group();
    lid.position.set(0.12, 0.02, 0);
    lid.rotation.z = -0.25;
    const lidBack = fieldBox(0.012, 0.35, 0.22, M.black());
    lidBack.position.set(0.006, 0.11, 0);
    screen.position.set(-0.002, 0.11, 0);
    screen.rotation.y = -Math.PI / 2;
    lid.add(lidBack, screen);
    laptop.add(lid);
    place(laptop, -0.05 - ds.shelfDepth / 2 - 0.02, cy - 0.15, ds.shelfHeight + 0.015);
    tagPick(screen, { kind: "dsLaptop", station: name, label: `${name} driver station` });
    group.add(laptop);

    // Team sign (front: team number in alliance color; rear: driver-facing text) and the team LED stack.
    const signTop = ds.baseHeight + ds.glassHeight + 0.04;
    const signWidth = Math.min(1.0, width - 0.2);
    const front = new CanvasPanel(512, 160);
    const rear = new CanvasPanel(512, 160);
    front.text("", { bg: "#050505" });
    rear.text("", { bg: "#050505" });
    const sign = new THREE.Mesh(new THREE.BoxGeometry(0.1, ds.signHeight, signWidth), [
      new THREE.MeshBasicMaterial({ map: front.texture }), // +x (field side)
      new THREE.MeshBasicMaterial({ map: rear.texture }), // -x (driver side)
      M.black(), M.black(), M.black(), M.black(),
    ]);
    sign.position.copy(f2s(-0.03, cy, signTop + ds.signHeight / 2));
    // BoxGeometry face order is +x, -x, ...: +x faces the field, -x faces the drivers; both read correctly.
    tagPick(sign, { kind: "teamSign", station: name, label: `${name} team sign` });
    group.add(sign);

    // Team LED stack on the sign: two alliance-colored robot segments over the amber stop segment (top to bottom).
    const tower = towerLight({ colors: [0xf6a823, allianceColor(alliance), allianceColor(alliance)], radius: 0.04, segment: 0.062 });
    const stack = tower.group;
    const lamps = [tower.lamps[2], tower.lamps[1], tower.lamps[0]];
    stack.position.copy(f2s(-0.03, cy + signWidth / 2 - 0.08, signTop + ds.signHeight));
    group.add(stack);

    const station = { name, alliance, group, sign, front, rear, lamps, eStop, aStop, screenPanel, center: cy, width };
    parts.stations[name] = station;

    // Team display: not official FIRST hardware, but a fixture at Cheesy/Freezy events. A TV above the station,
    // facing the field, showing the arena's alliance station display (team, rank, match clock, field state).
    const tvWidth = Math.min(0.96, width - 0.25);
    const tvHeight = tvWidth * 9 / 16;
    const tvBottom = 2.72;
    const tv = monitor({ width: tvWidth, height: tvHeight, label: name, standHeight: 0, screenId: `as-${name}` });
    place(tv.group, -0.03, cy, tvBottom, Math.PI / 2);
    group.add(tv.group);
    const railTop = ds.baseHeight + ds.glassHeight + 0.04;
    for (const dy of [-tvWidth * 0.36, tvWidth * 0.36]) {
      const post = fieldBox(0.035, 0.035, tvBottom - railTop + 0.04, M.darkAlu());
      place(post, -0.06, cy + dy, (railTop + tvBottom) / 2);
      post.userData.keep = true; // (the team display is not official field hardware: not in the CAD)
      group.add(post);
    }
    parts.teamDisplays[name] = tv;

    if (number === 2) {
      const timerFront = new CanvasPanel(512, 160);
      timerFront.text("", { bg: "#050505" });
      const timerRear = new CanvasPanel(512, 160);
      timerRear.text("", { bg: "#050505" });
      const timer = new THREE.Mesh(new THREE.BoxGeometry(0.1, 0.26, 0.82), [
        new THREE.MeshBasicMaterial({ map: timerFront.texture }),
        new THREE.MeshBasicMaterial({ map: timerRear.texture }),
        M.black(), M.black(), M.black(), M.black(),
      ]);
      timer.position.copy(f2s(-0.03, cy, signTop + ds.signHeight + 0.17));
      tagPick(timer, { kind: "timer", alliance, label: `${alliance} driver station timer` });
      group.add(timer);
      parts.timers[alliance] = { mesh: timer, front: timerFront, rear: timerRear };
    }
    markStructure(group, [eStop.group, aStop.group, laptop, sign, stack, tv.group]);
    half.add(group);
  }

  // Tower wall segment between DS3 and DS2.
  const towerWall = fieldBox(0.05, towerY1 - towerY0, 2.0, M.diamond());
  place(towerWall, -0.025, (towerY0 + towerY1) / 2, 1.0);
  towerWall.userData.cad = true;
  half.add(towerWall);
}

function buildStopButton(capColor, baseColor, letter) {
  const group = new THREE.Group();
  const base = new THREE.Mesh(new THREE.BoxGeometry(0.11, 0.07, 0.11),
    new THREE.MeshStandardMaterial({ color: baseColor, roughness: 0.5 }));
  base.position.y = 0.035;
  base.castShadow = true;
  group.add(base);
  const stem = new THREE.Mesh(new THREE.CylinderGeometry(0.02, 0.02, 0.03, 12), M.black());
  stem.position.y = 0.085;
  group.add(stem);
  const cap = new THREE.Mesh(new THREE.CylinderGeometry(0.045, 0.04, 0.035, 24),
    new THREE.MeshStandardMaterial({ color: capColor, roughness: 0.35, emissive: capColor, emissiveIntensity: 0.05 }));
  cap.position.y = 0.112;
  cap.castShadow = true;
  group.add(cap);
  return { group, cap, letter, restY: 0.112 };
}

export { PIXELS };
