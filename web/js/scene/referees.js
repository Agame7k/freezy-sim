// The referee crew on the field: four line referees outside the guardrails and the head referee between the field
// and the scoring table. They act out the officials' live calls from the simulator: the nearest referee turns to a
// PIN, points at the pinning robot and counts the seconds off on a raised hand; a foul gets the offending alliance's
// flag raised and waved with the call shown above; after the match the head referee holds up any CARDS.
import * as THREE from "three";
import { on } from "../core/store.js";
import { ctx, f2s, CanvasPanel, tagPick } from "./util.js";
import { registerFigure } from "./people.js";
import { person } from "./driveteams.js";

const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];
const FLAG_SEC = 2.6;
const CARD_SEC = 9;
const THUMBS_SEC = 3.5; // how long the FTA holds the thumbs up

function stripedShirt() {
  const c = document.createElement("canvas");
  c.width = 64;
  c.height = 64;
  const g = c.getContext("2d");
  for (let i = 0; i < 8; i++) {
    g.fillStyle = i % 2 ? "#111" : "#f4f4f4";
    g.fillRect(i * 8, 0, 8, 64);
  }
  const texture = new THREE.CanvasTexture(c);
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.repeat.set(3, 1);
  return texture;
}

function flagMesh(color) {
  const flag = new THREE.Group();
  const stick = new THREE.Mesh(new THREE.CylinderGeometry(0.008, 0.008, 0.42, 6),
    new THREE.MeshStandardMaterial({ color: 0x2a2a2a }));
  stick.position.y = 0.21;
  const cloth = new THREE.Mesh(new THREE.PlaneGeometry(0.26, 0.18),
    new THREE.MeshStandardMaterial({ color, side: THREE.DoubleSide, roughness: 0.9 }));
  cloth.position.set(0.13, 0.33, 0);
  flag.add(stick, cloth);
  flag.cloth = cloth;
  return flag;
}

function buildOfficial({ head = false } = {}) {
  const root = new THREE.Group();
  const shirt = head
    ? new THREE.MeshStandardMaterial({ color: 0xf2d21b, roughness: 0.8 })
    : new THREE.MeshStandardMaterial({ map: stripedShirt(), roughness: 0.85 });
  const skin = new THREE.MeshStandardMaterial({ color: 0xc89070, roughness: 0.7 });
  const pants = new THREE.MeshStandardMaterial({ color: 0x1d1f24, roughness: 0.9 });
  const legs = new THREE.Mesh(new THREE.CapsuleGeometry(0.12, 0.62, 4, 8), pants);
  legs.position.y = 0.44;
  const torso = new THREE.Mesh(new THREE.CapsuleGeometry(0.18, 0.36, 4, 10), shirt);
  torso.position.y = 1.16;
  const head3 = new THREE.Mesh(new THREE.SphereGeometry(0.105, 14, 10), skin);
  head3.position.y = 1.6;
  const cap = new THREE.Mesh(new THREE.SphereGeometry(0.11, 12, 6, 0, Math.PI * 2, 0, Math.PI / 2),
    new THREE.MeshStandardMaterial({ color: head ? 0xf2d21b : 0x111111 }));
  cap.position.y = 1.62;
  // Arms hang from shoulder pivots so they can point, raise flags and count.
  const arm = (side) => {
    const pivot = new THREE.Group();
    pivot.position.set(0, 1.38, side * 0.23);
    const limb = new THREE.Mesh(new THREE.CapsuleGeometry(0.045, 0.48, 4, 8), shirt);
    limb.position.y = -0.28;
    const hand = new THREE.Mesh(new THREE.SphereGeometry(0.05, 10, 8), skin);
    hand.position.y = -0.56;
    pivot.add(limb, hand);
    pivot.hand = hand;
    return pivot;
  };
  const right = arm(1);
  const left = arm(-1);
  root.add(legs, torso, head3, cap, right, left);
  root.traverse((child) => {
    if (!child.isMesh) return;
    child.castShadow = true;
    child.userData.body = true; // (the capsule body: hidden once the rigged human takes over, people.js)
  });

  const official = { root, right, left, head };
  registerFigure({ root, right, left, look: head ? { shirt: 0xf2d21b, skin: 0xc89070, pants: 0x1d1f24 }
    : { shirt: 0xf4f4f4, stripes: true, skin: 0xc89070, pants: 0x1d1f24 } });
  if (!head) {
    official.flags = { red: flagMesh(0xd8262c), blue: flagMesh(0x2361d8) };
    official.flags.red.position.y = -0.58;
    official.flags.blue.position.y = -0.58;
    official.flags.red.rotation.z = Math.PI; // carried pointing down when not in use
    official.flags.blue.rotation.z = Math.PI;
    right.add(official.flags.red);
    left.add(official.flags.blue);
  } else {
    official.card = new THREE.Mesh(new THREE.PlaneGeometry(0.12, 0.17),
      new THREE.MeshStandardMaterial({ color: 0xffd400, side: THREE.DoubleSide, emissive: 0x332a00 }));
    official.card.position.y = -0.64;
    official.card.visible = false;
    right.add(official.card);
  }
  // Caption above the official (the call, the count, the card).
  official.caption = new CanvasPanel(512, 128);
  official.sprite = new THREE.Sprite(new THREE.SpriteMaterial({ map: official.caption.texture, transparent: true,
    depthTest: false }));
  official.sprite.scale.set(1.4, 0.35, 1);
  official.sprite.position.y = 2.15;
  official.sprite.visible = false;
  official.sprite.renderOrder = 6;
  root.add(official.sprite);
  return official;
}

export function setupReferees(venue, layout) {
  const { L, W } = ctx;
  const tableHigh = (layout.venue?.scoringTableSide || "high") === "high";
  const group = new THREE.Group();
  group.name = "officials";
  venue.scene.add(group);

  // Line referees: one at each corner outside the guardrail, each covering its quarter of the field.
  const posts = [
    { x: L * 0.27, y: -0.75 }, { x: L * 0.73, y: -0.75 }, { x: L * 0.27, y: W + 0.75 }, { x: L * 0.73, y: W + 0.75 },
  ];
  const refs = posts.map((post) => {
    const official = buildOfficial();
    official.home = post;
    official.at = { ...post };
    official.root.position.copy(f2s(post.x, post.y, 0));
    group.add(official.root);
    return official;
  });
  const headRef = buildOfficial({ head: true });
  headRef.home = { x: L / 2, y: tableHigh ? W + 0.8 : -0.8 };
  headRef.at = { ...headRef.home };
  headRef.root.position.copy(f2s(headRef.home.x, headRef.home.y, 0));
  group.add(headRef.root);

  // The FTA (field technical advisor), at the scoring table end beside the head referee: works the field systems
  // between matches and, when the arena reports the field ready to start, turns to the head referee and gives the
  // thumbs up (the head referee then clears the field for the scorekeeper).
  const fta = person(41, { shirt: 0x34383f });
  fta.home = { x: L / 2 + 2.1, y: tableHigh ? W + 0.9 : -0.9 };
  fta.at = { ...fta.home };
  fta.root.position.copy(f2s(fta.home.x, fta.home.y, 0));
  const lanyard = new THREE.Mesh(new THREE.PlaneGeometry(0.07, 0.1), new THREE.MeshStandardMaterial({ color: 0xf2f2f2,
    emissive: 0x202020 }));
  lanyard.position.set(0.19, 1.15 * fta.height, 0);
  lanyard.rotation.y = Math.PI / 2;
  fta.root.add(lanyard);
  fta.caption = new CanvasPanel(512, 128);
  fta.sprite = new THREE.Sprite(new THREE.SpriteMaterial({ map: fta.caption.texture, transparent: true, depthTest: false }));
  fta.sprite.scale.set(1.4, 0.35, 1);
  fta.sprite.position.y = 2.15;
  fta.sprite.visible = false;
  fta.sprite.renderOrder = 6;
  fta.root.add(fta.sprite);
  fta.caption.text("FIELD READY", { fg: "#fff", bg: "rgba(33,160,90,0.92)", font: "bold 64px 'Segoe UI'", sub: "FTA 👍",
    subFont: "bold 40px 'Segoe UI'" });
  tagPick(fta.torso, { kind: "fta", label: "FTA (field technical advisor)" });
  group.add(fta.root);
  let fieldWasReady = false;
  on("arena.arenaStatus", (status) => {
    // MatchState 0 is pre-match: the moment the last start blocker clears, the FTA signals.
    const ready = Boolean(status?.CanStartMatch) && status?.MatchState === 0;
    if (ready && !fieldWasReady) fta.thumbsUntil = performance.now() + THUMBS_SEC * 1000;
    fieldWasReady = ready;
    fta.ready = ready;
    fta.between = status?.MatchState === 0 || status?.MatchState === 5;
  });
  venue.officials = { refs, headRef, fta };

  const robotAt = (station) => {
    const robot = venue.robots?.[station];
    if (!robot || !robot.root.visible) return null;
    const p = robot.root.position;
    return { x: p.x + L / 2, y: -p.z + W / 2 };
  };
  const nearest = (point) => {
    let best = refs[0];
    let bestDistance = Infinity;
    for (const ref of refs) {
      const d = Math.hypot(ref.home.x - point.x, ref.home.y - point.y);
      if (d < bestDistance) {
        best = ref;
        bestDistance = d;
      }
    }
    return best;
  };

  // Live officials state from the world summary (10 Hz).
  let pins = [];
  const seen = new Set();
  on("world.state", (state) => {
    const officials = state?.officials;
    if (!officials) return;
    pins = officials.pins || [];
    for (const call of officials.recent || []) {
      const key = `${call.at.toFixed(2)}|${call.rule}|${call.station}`;
      if (seen.has(key)) continue;
      seen.add(key);
      if (seen.size > 400) seen.clear();
      const where = robotAt(call.station) || robotAt(call.victim) || { x: call.alliance === 0 ? L * 0.75 : L * 0.25, y: W / 2 };
      const ref = nearest(where);
      ref.flagUntil = performance.now() + FLAG_SEC * 1000;
      ref.flagColor = call.alliance === 0 ? "red" : "blue";
      ref.lookAt = where;
      ref.caption.text(`${call.rule} ${call.major ? "MAJOR" : "MINOR"}`, {
        fg: "#fff", bg: call.alliance === 0 ? "rgba(184,29,34,0.9)" : "rgba(26,79,184,0.9)", font: "bold 64px 'Segoe UI'",
        sub: call.station || (call.alliance === 0 ? "RED" : "BLUE"), subFont: "bold 40px 'Segoe UI'",
      });
      ref.captionUntil = ref.flagUntil + 600;
    }
  });
  on("officials.headRef", (state) => {
    const presented = state?.presented || [];
    if (!presented.length) return;
    const latest = presented[presented.length - 1];
    if (latest.at === headRef.cardAt) return;
    headRef.cardAt = latest.at;
    headRef.cardUntil = performance.now() + CARD_SEC * 1000;
    const red = presented.some((c) => c.card === "red");
    headRef.card.material.color.setHex(red ? 0xd8262c : 0xffd400);
    headRef.caption.text(`${red ? "RED" : "YELLOW"} CARD`, {
      fg: red ? "#fff" : "#111", bg: red ? "rgba(216,38,44,0.92)" : "rgba(255,212,0,0.92)", font: "bold 60px 'Segoe UI'",
      sub: presented.map((c) => c.team).join("  "), subFont: "bold 42px 'Segoe UI'",
    });
  });

  const face = (official, point, dt) => {
    // Turn the body toward the action (the official's local +z is its right side, so face along +x).
    const here = official.at;
    const target = Math.atan2(-(point.y - here.y), point.x - here.x);
    let delta = target - official.root.rotation.y;
    delta = Math.atan2(Math.sin(delta), Math.cos(delta));
    official.root.rotation.y += delta * Math.min(1, dt * 5);
  };
  const ease = (pivot, x, z, dt) => {
    pivot.rotation.x += (x - pivot.rotation.x) * Math.min(1, dt * 8);
    pivot.rotation.z += (z - pivot.rotation.z) * Math.min(1, dt * 8);
  };

  venue.addUpdater((dt) => {
    const now = performance.now();
    // Which referee owns which PIN: the one nearest the pinned robot.
    const counting = new Map();
    for (const pin of pins) {
      const where = robotAt(pin.pinner);
      if (!where) continue;
      const ref = nearest(where);
      if (!counting.has(ref) || pin.seconds > counting.get(ref).pin.seconds) counting.set(ref, { pin, where });
    }
    for (const ref of refs) {
      const job = counting.get(ref);
      const flagging = ref.flagUntil > now;
      // Walk along the guardrail to stay level with the play (within their stretch of the field).
      const watch = job?.where || (flagging ? ref.lookAt : null);
      const goalX = watch ? Math.min(Math.max(watch.x, ref.home.x - 2.2), ref.home.x + 2.2) : ref.home.x;
      ref.at.x += (goalX - ref.at.x) * Math.min(1, dt * 0.8);
      ref.root.position.copy(f2s(ref.at.x, ref.at.y, 0));
      // Idle: watch the field center; otherwise the action.
      face(ref, watch || { x: L / 2, y: W / 2 }, dt);
      const t = now / 1000;
      if (flagging) {
        // Raise the offending alliance's flag overhead and wave it.
        const wave = Math.sin(t * 10) * 0.35;
        const raised = ref.flagColor === "red" ? ref.right : ref.left;
        const other = ref.flagColor === "red" ? ref.left : ref.right;
        ease(raised, wave, (ref.flagColor === "red" ? 1 : -1) * 0.25 + Math.PI, dt);
        ease(other, 0, 0, dt);
        ref.flags[ref.flagColor].rotation.z = 0;
        ref.flags[ref.flagColor === "red" ? "blue" : "red"].rotation.z = Math.PI;
      } else if (job) {
        // Point at the pinning robot with the right arm; count the seconds on the raised left hand.
        ease(ref.right, 0, Math.PI / 2, dt);
        const beat = (t % 1) < 0.25 ? 0.25 : 0;
        ease(ref.left, 0, Math.PI * 0.85 + beat, dt);
        ref.flags.red.rotation.z = Math.PI;
        ref.flags.blue.rotation.z = Math.PI;
        const count = Math.min(job.pin.seconds, 99).toFixed(0);
        ref.caption.text(`PIN ${count}`, { fg: "#111", bg: "rgba(255,255,255,0.92)", font: "bold 72px 'Segoe UI'",
          sub: `${job.pin.pinner} on ${job.pin.pinned}${job.pin.paused ? " (paused)" : ""}`, subFont: "bold 38px 'Segoe UI'" });
        ref.captionUntil = now + 400;
      } else {
        // At rest, a slight sway.
        ease(ref.right, Math.sin(t * 1.3) * 0.04, 0.06, dt);
        ease(ref.left, -Math.sin(t * 1.3) * 0.04, -0.06, dt);
        ref.flags.red.rotation.z = Math.PI;
        ref.flags.blue.rotation.z = Math.PI;
      }
      if (flagging) {
        const cloth = ref.flags[ref.flagColor].cloth;
        cloth.rotation.y = Math.sin(t * 14) * 0.5;
      }
      ref.sprite.visible = (ref.captionUntil || 0) > now;
    }
    // Head referee: watches the field; holds the CARD up after the match.
    const showing = (headRef.cardUntil || 0) > now;
    face(headRef, { x: L / 2, y: W / 2 }, dt);
    headRef.card.visible = showing;
    ease(headRef.right, 0, showing ? Math.PI * 0.92 : 0.06, dt);
    ease(headRef.left, 0, -0.06, dt);
    headRef.sprite.visible = showing;

    // FTA: thumbs up to the head referee when the field turns ready; otherwise at work on the field systems between
    // matches (until ready), watching the field during one.
    const thumbs = (fta.thumbsUntil || 0) > now;
    face(fta, thumbs ? headRef.at : { x: L / 2, y: W / 2 }, dt);
    fta.working = fta.between && !fta.ready && !thumbs;
    const k = Math.min(1, dt * 7);
    fta.right.rotation.z += ((thumbs ? 0.85 : 0.06) - fta.right.rotation.z) * k; // upper arm forward, a little down
    fta.rightElbow = (fta.rightElbow || 0) + ((thumbs ? 0.95 : 0) - (fta.rightElbow || 0)) * k; // forearm out toward the head ref, just above level
    fta.rightFist = (fta.rightFist || 0) + ((thumbs ? 1 : 0) - (fta.rightFist || 0)) * k;
    fta.left.rotation.z += (0.06 - fta.left.rotation.z) * k;
    fta.sprite.visible = thumbs;
  });
  return venue.officials;
}
