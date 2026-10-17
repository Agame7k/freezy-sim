// Between matches: robots ride carts on and off the field pushed by their drive teams, and the field reset crew
// (volunteers in field staff shirts with buckets) walks the field collecting and restaging FUEL. Positions come
// from the simulator's world stream; this module draws them.
import * as THREE from "three";
import { f2s } from "./util.js";
import { person } from "./driveteams.js";

const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];

function cartMesh() {
  // A robot cart: a plywood deck on a steel frame with four casters and a push handle.
  const cart = new THREE.Group();
  const deck = new THREE.Mesh(new THREE.BoxGeometry(1.0, 0.03, 0.85), new THREE.MeshStandardMaterial({ color: 0xb48a5a, roughness: 0.8 }));
  deck.position.y = 0.29;
  cart.add(deck);
  const steel = new THREE.MeshStandardMaterial({ color: 0x3a3d43, metalness: 0.6, roughness: 0.4 });
  for (const [x, z] of [[0.42, 0.36], [0.42, -0.36], [-0.42, 0.36], [-0.42, -0.36]]) {
    const leg = new THREE.Mesh(new THREE.BoxGeometry(0.04, 0.2, 0.04), steel);
    leg.position.set(x, 0.18, z);
    const caster = new THREE.Mesh(new THREE.CylinderGeometry(0.05, 0.05, 0.035, 12), new THREE.MeshStandardMaterial({ color: 0x111111 }));
    caster.rotation.x = Math.PI / 2;
    caster.position.set(x, 0.05, z);
    cart.add(leg, caster);
  }
  const handle = new THREE.Mesh(new THREE.BoxGeometry(0.03, 0.75, 0.75), steel);
  handle.position.set(-0.52, 0.65, 0);
  cart.add(handle);
  cart.traverse((child) => { if (child.isMesh) child.castShadow = true; });
  return cart;
}

function volunteerMesh(seed) {
  const crew = person(seed, { shirt: 0x14161a });
  // "FIELD RESET" style staff shirt with a bright stripe, and a bucket in the right hand.
  const stripe = new THREE.Mesh(new THREE.BoxGeometry(0.36, 0.06, 0.36), new THREE.MeshStandardMaterial({ color: 0x36d07f }));
  stripe.position.y = 1.2 * crew.height;
  crew.root.add(stripe);
  const bucket = new THREE.Group();
  const shell = new THREE.Mesh(new THREE.CylinderGeometry(0.15, 0.12, 0.3, 16, 1, true),
    new THREE.MeshStandardMaterial({ color: 0xff7a1a, roughness: 0.6, side: THREE.DoubleSide }));
  shell.position.y = -0.15;
  bucket.add(shell);
  const fill = new THREE.Mesh(new THREE.CylinderGeometry(0.135, 0.12, 0.01, 14),
    new THREE.MeshStandardMaterial({ color: 0xf5d020, roughness: 0.6 }));
  bucket.add(fill);
  bucket.position.y = -0.56;
  crew.right.add(bucket);
  crew.fill = fill;
  crew.bucket = bucket;
  return crew;
}

export function setupBetweenMatch(venue) {
  const carts = {};
  const pushers = {};
  const crew = [];
  let seed = 101;
  const group = new THREE.Group();
  group.name = "between-match";
  venue.scene.add(group);
  venue.betweenMatch = { carting: new Set() };

  const last = {};
  venue.addUpdater((dt) => {
    const frame = venue.world?.lastFrame;
    if (!frame) return;
    const t = performance.now() / 1000;
    const carting = venue.betweenMatch.carting;
    carting.clear();
    for (const r of frame.robots) {
      const station = STATIONS[r.index];
      const robot = venue.robots?.[station];
      const onCart = r.cart === 1 || r.cart === 3;
      if (!carts[station]) {
        carts[station] = cartMesh();
        group.add(carts[station]);
        pushers[station] = [person(seed++), person(seed++)];
        for (const p of pushers[station]) group.add(p.root);
      }
      const cart = carts[station];
      cart.visible = onCart && !!robot;
      for (const p of pushers[station]) p.root.visible = cart.visible;
      if (!cart.visible) continue;
      carting.add(station);
      // The cart rides under the robot (the robot sits on its deck); the drive team pushes from behind.
      const position = robot.root.position;
      const heading = robot.root.rotation.y;
      cart.position.set(position.x, 0, position.z);
      cart.rotation.y = heading;
      const moving = last[station] ? Math.hypot(position.x - last[station].x, position.z - last[station].z) > 0.0005 : false;
      last[station] = { x: position.x, z: position.z };
      const back = new THREE.Vector3(-Math.cos(heading), 0, Math.sin(heading));
      const side = new THREE.Vector3(Math.sin(heading), 0, Math.cos(heading));
      pushers[station].forEach((p, k) => {
        p.root.position.copy(position).addScaledVector(back, 0.95).addScaledVector(side, k ? 0.28 : -0.28);
        p.root.position.y = moving ? Math.abs(Math.sin(t * 7 + k)) * 0.03 : 0;
        p.root.rotation.y = heading;
        p.right.rotation.z = 1.25; // hands on the push handle
        p.left.rotation.z = 1.25;
      });
    }
    // Field reset volunteers.
    const volunteers = frame.crew || [];
    while (crew.length < volunteers.length) {
      const v = volunteerMesh(seed++);
      v.previous = null;
      group.add(v.root);
      crew.push(v);
    }
    crew.forEach((v, i) => {
      const data = volunteers[i];
      v.root.visible = !!data;
      if (!data) return;
      const at = f2s(data.x, data.y, 0);
      const moved = v.previous ? at.distanceTo(v.previous) : 0;
      if (moved > 0.002) v.root.rotation.y = Math.atan2(-(at.z - v.previous.z), at.x - v.previous.x);
      v.previous = at.clone();
      v.root.position.copy(at);
      const walking = moved > 0.002;
      const working = !walking && (data.mode === 1 || data.mode === 2);
      // Walking bob, or bent over scooping / setting FUEL out on the marks.
      v.root.position.y = walking ? Math.abs(Math.sin(t * 8 + i)) * 0.035 : 0;
      v.torso.rotation.z = working ? -0.55 : 0;
      v.neck.rotation.z = working ? -0.4 : 0;
      v.left.rotation.z = working ? 0.9 + Math.sin(t * 9 + i) * 0.3 : Math.sin(t * 8 + i) * 0.25;
      v.right.rotation.z = working ? 0.5 : -Math.sin(t * 8 + i) * 0.15;
      v.fill.position.y = -0.29 + Math.min(1, data.bucket / 28) * 0.27;
      v.fill.visible = data.bucket > 0;
    });
  });
  return venue.betweenMatch;
}
