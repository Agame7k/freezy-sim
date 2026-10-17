// The event venue around the field: floor, scoring table and its screens, audience screen, bleachers, truss, lights.
import * as THREE from "three";
import { ctx, f2s, place, fieldBox, M, CanvasPanel, tagPick, noiseTexture, towerLight } from "./util.js";

/** A monitor on a stand. Returns { group, screen, panel } with the screen facing the given field yaw. */
export function monitor({ width = 0.62, height = 0.36, label = "", standHeight = 0.3, kind = "screen", screenId = "" } = {}) {
  const group = new THREE.Group();
  const panel = new CanvasPanel(Math.round(width * 1000), Math.round(height * 1000));
  panel.text(label, { fg: "#8fa1b8", bg: "#0d1117", font: "bold 64px 'Segoe UI'" });
  const bezel = new THREE.Mesh(new THREE.BoxGeometry(width + 0.03, height + 0.03, 0.035), M.black());
  bezel.position.y = standHeight + height / 2;
  bezel.castShadow = true;
  const screen = new THREE.Mesh(new THREE.PlaneGeometry(width, height), new THREE.MeshBasicMaterial({ map: panel.texture }));
  screen.position.set(0, standHeight + height / 2, 0.019);
  group.add(bezel, screen);
  if (standHeight > 0) {
    const stand = new THREE.Mesh(new THREE.CylinderGeometry(0.02, 0.02, standHeight, 8), M.darkAlu());
    stand.position.y = standHeight / 2;
    const foot = new THREE.Mesh(new THREE.BoxGeometry(0.22, 0.015, 0.16), M.darkAlu());
    foot.position.y = 0.008;
    group.add(stand, foot);
  }
  tagPick(screen, { kind, screenId, label });
  return { group, screen, panel, width, height };
}

export function buildEnvironment(scene, layout) {
  const { L, W } = ctx;
  const parts = { screens: {}, stackLight: null };
  const group = new THREE.Group();
  group.name = "venue";

  // Floor and pipe-and-drape.
  const floor = new THREE.Mesh(new THREE.PlaneGeometry(80, 60),
    new THREE.MeshStandardMaterial({ color: 0x1b1d21, roughness: 0.92, map: noiseTexture(256, 0.25, "#5c5f66") }));
  floor.material.map.repeat.set(20, 15);
  floor.rotation.x = -Math.PI / 2;
  floor.position.y = -0.003;
  floor.receiveShadow = true;
  group.add(floor);
  const drape = new THREE.MeshStandardMaterial({ color: 0x0c0d10, roughness: 1 });
  for (const x of [-4.2, L + 4.2]) {
    const curtain = fieldBox(0.05, W + 9, 3.2, drape);
    place(curtain, x, W / 2, 1.6);
    curtain.castShadow = false;
    group.add(curtain);
  }

  // Scoring table along the high-y side.
  const tableY = W + 1.45;
  const table = new THREE.Group();
  const top = fieldBox(6.4, 0.85, 0.04, new THREE.MeshStandardMaterial({ color: 0x222428, roughness: 0.7 }));
  place(top, L / 2, tableY, 0.76);
  const skirt = fieldBox(6.4, 0.02, 0.72, new THREE.MeshStandardMaterial({ color: 0x0e0f12, roughness: 1 }));
  place(skirt, L / 2, tableY - 0.41, 0.38);
  table.add(top, skirt);
  const tableScreens = [
    ["matchPlay", "Match Play", -2.45],
    ["fieldMonitor", "Field Monitor (FTA)", -1.3],
    ["fmsFieldMonitor", "FMS Field Monitor", -0.35],
    ["referee", "Head Referee", 0.95],
    ["announcer", "Announcer", 2.0],
    ["scoringRed", "Red Scoring", 2.85],
  ];
  for (const [id, label, dx] of tableScreens) {
    const m = monitor({ label, kind: "screen", screenId: id, width: id === "referee" ? 0.5 : 0.62 });
    // The screen normal is the monitor's +z, which in scene space already points at the field (field −y).
    place(m.group, L / 2 + dx, tableY + 0.05, 0.78, 0);
    table.add(m.group);
    parts.screens[id] = m;
  }
  // Network rack under the table with link LEDs.
  const rack = fieldBox(0.6, 0.6, 0.62, M.black());
  place(rack, L / 2 + 0.3, tableY + 0.05, 0.31);
  table.add(rack);
  const rackLeds = [];
  for (let i = 0; i < 12; i++) {
    const ledMesh = new THREE.Mesh(new THREE.BoxGeometry(0.02, 0.015, 0.01),
      new THREE.MeshBasicMaterial({ color: 0x113311 }));
    ledMesh.position.copy(f2s(L / 2 + 0.06 + (i % 6) * 0.08, tableY - 0.255, 0.45 + Math.floor(i / 6) * 0.08));
    table.add(ledMesh);
    rackLeds.push(ledMesh);
  }
  tagPick(rack, { kind: "rack", label: "Field network rack (switch, AP, PLC)" });
  parts.rackLeds = rackLeds;

  // Field stack light on a pole at the scoring table: a four-segment tower light (red, blue, amber, green from the
  // top) with the buzzer on top.
  const stack = new THREE.Group();
  const pole = new THREE.Mesh(new THREE.CylinderGeometry(0.018, 0.018, 1.3, 10), M.darkAlu());
  pole.position.y = 0.65;
  stack.add(pole);
  const stackBracket = new THREE.Mesh(new THREE.BoxGeometry(0.16, 0.012, 0.16), M.darkAlu());
  stackBracket.position.y = 0.006;
  stack.add(stackBracket);
  const tower = towerLight({ colors: [0x35c47c, 0xf6a823, 0x3d8bff, 0xef4b4f], radius: 0.05, segment: 0.085, buzzer: true });
  tower.group.position.y = 1.3;
  stack.add(tower.group);
  const [green, orange, blue, red] = tower.lamps;
  const lamps = { red, blue, orange, green };
  const buzzer = tower.top;
  const stackGlow = new THREE.PointLight(0xffffff, 0, 3, 2);
  stackGlow.position.y = 1.55;
  stack.add(stackGlow);
  place(stack, L / 2 + 0.35, tableY - 0.2, 0.78);
  tagPick(lamps.green, { kind: "stackLight", label: "Field stack light" });
  table.add(stack);
  parts.stackLight = { lamps, buzzer, glow: stackGlow };

  // Field reset light (green beacon above the scoring table) and awards-mode light.
  const beacon = new THREE.Mesh(new THREE.SphereGeometry(0.09, 16, 12),
    new THREE.MeshStandardMaterial({ color: 0x35c47c, emissive: 0x35c47c, emissiveIntensity: 0.04 }));
  place(beacon, L / 2 - 0.5, tableY - 0.2, 2.25);
  tagPick(beacon, { kind: "resetLight", label: "Field reset light" });
  table.add(beacon);
  parts.resetLight = beacon;
  group.add(table);

  // Audience screen high above the scoring table, facing the stands (−y).
  const screenWidth = 7.2;
  const screenHeight = screenWidth * 9 / 16;
  const big = new THREE.Group();
  const bigPanel = new CanvasPanel(1280, 720);
  bigPanel.text("Audience Display", { fg: "#7d8ea6", bg: "#06080b", font: "bold 90px 'Segoe UI'" });
  const bigScreen = new THREE.Mesh(new THREE.PlaneGeometry(screenWidth, screenHeight), new THREE.MeshBasicMaterial({ map: bigPanel.texture }));
  const frame = new THREE.Mesh(new THREE.BoxGeometry(screenWidth + 0.2, screenHeight + 0.2, 0.15), M.black());
  frame.position.z = -0.08;
  big.add(frame, bigScreen);
  big.position.copy(f2s(L / 2, W + 5.5, 3.0 + screenHeight / 2));
  // Plane normal +z (scene) = field −y: the screen faces the stands across the field.
  tagPick(bigScreen, { kind: "screen", screenId: "audience", label: "Audience screen" });
  group.add(big);
  parts.screens.audience = { group: big, screen: bigScreen, panel: bigPanel, width: screenWidth, height: screenHeight };
  for (const dx of [-screenWidth / 2 - 0.3, screenWidth / 2 + 0.3]) {
    const leg = fieldBox(0.2, 0.2, 3.0 + screenHeight + 0.2, M.darkAlu());
    place(leg, L / 2 + dx, W + 5.55, (3.0 + screenHeight + 0.2) / 2);
    group.add(leg);
  }

  // Queueing and pit displays near the red end.
  const queue = monitor({ width: 1.2, height: 0.68, label: "Queueing", standHeight: 1.6, screenId: "queueing" });
  place(queue.group, L + 2.6, W + 1.2, 0);
  queue.group.lookAt(f2s(L / 2, W / 2, 0));
  group.add(queue.group);
  parts.screens.queueing = queue;
  const rankings = monitor({ width: 1.2, height: 0.68, label: "Rankings", standHeight: 1.6, screenId: "rankings" });
  place(rankings.group, -2.6, W + 1.2, 0);
  rankings.group.lookAt(f2s(L / 2, W / 2, 0));
  group.add(rankings.group);
  parts.screens.rankings = rankings;

  // Playoff bracket next to the rankings.
  const bracket = monitor({ width: 1.2, height: 0.68, label: "Bracket", standHeight: 1.6, screenId: "bracket" });
  place(bracket.group, -2.6, W + 2.7, 0);
  bracket.group.lookAt(f2s(L / 2, W / 2, 0));
  group.add(bracket.group);
  parts.screens.bracket = bracket;

  // Driver-facing field monitors: one at each end of the scoring-table side, angled at that alliance's drivers.
  for (const [alliance, x, driversX] of [["blue", 0.9, -0.8], ["red", L - 0.9, L + 0.8]]) {
    const ds = monitor({ width: 0.9, height: 0.506, label: `${alliance === "red" ? "Red" : "Blue"} field monitor`, standHeight: 1.55,
      screenId: alliance === "red" ? "dsMonitorRed" : "dsMonitorBlue" });
    place(ds.group, x, W + 0.75, 0);
    ds.group.lookAt(f2s(driversX, W / 2, 0));
    group.add(ds.group);
    parts.screens[alliance === "red" ? "dsMonitorRed" : "dsMonitorBlue"] = ds;
  }

  // A second audience screen behind the stands, facing the field, the teams and the scoring table.
  {
    const width2 = 6.0;
    const height2 = width2 * 9 / 16;
    const panel2 = new CanvasPanel(1280, 720);
    panel2.text("Audience Display", { fg: "#7d8ea6", bg: "#06080b", font: "bold 90px 'Segoe UI'" });
    const screen2 = new THREE.Mesh(new THREE.PlaneGeometry(width2, height2), new THREE.MeshBasicMaterial({ map: panel2.texture }));
    const frame2 = new THREE.Mesh(new THREE.BoxGeometry(width2 + 0.2, height2 + 0.2, 0.15), M.black());
    frame2.position.z = -0.08;
    const big2 = new THREE.Group();
    big2.add(frame2, screen2);
    big2.position.copy(f2s(L / 2, -9.6, 4.6 + height2 / 2));
    big2.rotation.y = Math.PI; // face +y (the field)
    tagPick(screen2, { kind: "screen", screenId: "audience2", label: "Audience screen (teams side)" });
    group.add(big2);
    parts.screens.audience2 = { group: big2, screen: screen2, panel: panel2, width: width2, height: height2 };
    for (const dx of [-width2 / 2 - 0.3, width2 / 2 + 0.3]) {
      const leg = fieldBox(0.2, 0.2, 4.6 + height2 + 0.2, M.darkAlu());
      place(leg, L / 2 + dx, -9.65, (4.6 + height2 + 0.2) / 2);
      group.add(leg);
    }
  }

  // Bleachers on the audience side (−y) with a crowd.
  const bleachers = new THREE.Group();
  const stepMaterial = new THREE.MeshStandardMaterial({ color: 0x2a2d33, roughness: 0.9 });
  const rows = 11;
  for (let r = 0; r < rows; r++) {
    const step = fieldBox(L + 6, 0.8, 0.42 * (r + 1), stepMaterial);
    step.castShadow = false;
    place(step, L / 2, -2.6 - r * 0.8, (0.42 * (r + 1)) / 2);
    bleachers.add(step);
  }
  const crowdCount = 420;
  const person = new THREE.CapsuleGeometry(0.17, 0.5, 4, 8);
  const crowd = new THREE.InstancedMesh(person, new THREE.MeshStandardMaterial({ roughness: 0.8 }), crowdCount);
  const dummy = new THREE.Object3D();
  const shirt = new THREE.Color();
  for (let i = 0; i < crowdCount; i++) {
    const r = Math.floor(Math.random() * rows);
    const x = -2.5 + Math.random() * (L + 5);
    dummy.position.copy(f2s(x, -2.6 - r * 0.8, 0.42 * (r + 1) + 0.42));
    dummy.rotation.set(0, (Math.random() - 0.5) * 0.6, 0);
    dummy.updateMatrix();
    crowd.setMatrixAt(i, dummy.matrix);
    const palette = [0xd8262c, 0x1f5fd6, 0xf3d21c, 0x2b2b2b, 0xe8e8e8, 0x2e8b57, 0x8a4fff, 0xff8c1a];
    shirt.setHex(palette[Math.floor(Math.random() * palette.length)]);
    crowd.setColorAt(i, shirt);
  }
  bleachers.add(crowd);
  parts.crowd = crowd;
  group.add(bleachers);

  // Overhead truss with a few fixtures.
  const truss = new THREE.MeshStandardMaterial({ color: 0x9aa0a8, metalness: 0.8, roughness: 0.3 });
  for (const y of [-1.2, W + 1.2]) {
    const beam = fieldBox(L + 7, 0.3, 0.3, truss);
    beam.castShadow = false;
    place(beam, L / 2, y, 7.5);
    group.add(beam);
  }
  scene.add(group);

  // Lighting.
  const hemi = new THREE.HemisphereLight(0xdfe7ff, 0x2a2d35, 1.1);
  scene.add(hemi);
  const key = new THREE.DirectionalLight(0xffffff, 2.1);
  key.position.copy(f2s(L / 2 - 3, -4, 18));
  key.target.position.copy(f2s(L / 2, W / 2, 0));
  key.castShadow = true;
  key.shadow.mapSize.set(4096, 4096);
  Object.assign(key.shadow.camera, { left: -12, right: 12, top: 9, bottom: -9, near: 2, far: 45 });
  key.shadow.bias = -0.0004;
  key.shadow.normalBias = 0.02;
  scene.add(key, key.target);
  const spots = [];
  for (const [x, y] of [[L * 0.2, W + 1.2], [L * 0.8, W + 1.2], [L * 0.5, -1.2]]) {
    const spot = new THREE.SpotLight(0xfff4e0, 120, 30, 0.6, 0.6, 1.6);
    spot.position.copy(f2s(x, y, 7.3));
    spot.target.position.copy(f2s(L / 2, W / 2, 0));
    scene.add(spot, spot.target);
    spots.push(spot);
  }
  // Show wash: one overhead colour light, dark until the PLC's winner sequence runs (added up front so the light
  // count, and the compiled shaders, never change mid-show).
  const wash = new THREE.SpotLight(0xffffff, 0, 34, 0.95, 0.8, 1.2);
  wash.position.copy(f2s(L / 2, W / 2, 9));
  wash.target.position.copy(f2s(L / 2, W / 2, 0));
  scene.add(wash, wash.target);
  parts.house = { hemi, key, spots, wash, base: { hemi: hemi.intensity, key: key.intensity, spot: spots[0].intensity } };
  parts.movers = buildMovers(scene, truss);
  return parts;
}

// Moving-head fixtures hung under both truss beams. Their beams are additive haze cones (no real lights, so they
// cost nothing until the show runs); winnershow.js aims and colours them.
function buildMovers(scene, trussMaterial) {
  const { L, W } = ctx;
  const fade = document.createElement("canvas");
  fade.width = 4;
  fade.height = 64;
  const g = fade.getContext("2d");
  const gradient = g.createLinearGradient(0, 0, 0, 64);
  gradient.addColorStop(0, "#fff"); // apex (cone top, v = 1)
  gradient.addColorStop(0.5, "#555");
  gradient.addColorStop(1, "#000");
  g.fillStyle = gradient;
  g.fillRect(0, 0, 4, 64);
  const alphaMap = new THREE.CanvasTexture(fade);
  // Unit-length cone: apex at the pivot, opening along +z (Object3D.lookAt points +z at the target).
  const beamGeometry = new THREE.ConeGeometry(0.11, 1, 24, 1, true).translate(0, -0.5, 0).rotateX(-Math.PI / 2);
  const bodyMaterial = new THREE.MeshStandardMaterial({ color: 0x1a1c20, metalness: 0.5, roughness: 0.5 });
  const movers = [];
  for (const y of [-1.2, W + 1.2]) {
    for (const fx of [0.12, 0.31, 0.5, 0.69, 0.88]) {
      const mount = new THREE.Group();
      mount.position.copy(f2s(L * fx, y, 7.32));
      const yoke = new THREE.Mesh(new THREE.BoxGeometry(0.26, 0.08, 0.2), trussMaterial);
      yoke.position.y = 0.04;
      mount.add(yoke);
      const head = new THREE.Group();
      head.position.y = -0.14;
      const body = new THREE.Mesh(new THREE.CylinderGeometry(0.11, 0.13, 0.26, 16).rotateX(Math.PI / 2), bodyMaterial);
      body.position.z = -0.05;
      head.add(body);
      const lens = new THREE.Mesh(new THREE.CircleGeometry(0.09, 16),
        new THREE.MeshStandardMaterial({ color: 0x111111, emissive: 0xffffff, emissiveIntensity: 0 }));
      lens.position.z = 0.081;
      head.add(lens);
      const beam = new THREE.Mesh(beamGeometry, new THREE.MeshBasicMaterial({
        color: 0xffffff, alphaMap, transparent: true, opacity: 0, blending: THREE.AdditiveBlending,
        depthWrite: false, side: THREE.DoubleSide,
      }));
      beam.visible = false;
      beam.renderOrder = 5;
      head.add(beam);
      mount.add(head);
      scene.add(mount);
      // Rest pose: pointing straight down at the floor below.
      mount.updateMatrixWorld(true);
      head.lookAt(f2s(L * fx, y, 0));
      movers.push({ head, beam, lens, fx, side: y < 0 ? -1 : 1 });
    }
  }
  return movers;
}
