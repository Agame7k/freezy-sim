// People: referees, drive teams, human players, the field crew and the crowd as rigged, animated humans
// (web/models/human.fbx, "Animated Human" by Quaternius, CC0). Every figure is first built from capsules (driveteams.js,
// referees.js, field.js) with pivots for its arms, neck and torso that the match logic animates; once the model has
// loaded, the capsules hide and a skinned human takes their place: it walks or runs when its figure moves, idles
// otherwise, and the pivots' rotations are layered onto its shoulder, head and spine bones, so every gesture the
// logic makes (pointing, flags overhead, E-stop slaps, cheering) still shows. Anything carried on a pivot (flags,
// cards, controllers) stays with the arm.
import * as THREE from "three";
import { FBXLoader } from "three/addons/loaders/FBXLoader.js";
import * as SkeletonUtils from "three/addons/utils/SkeletonUtils.js";

const MODEL_HEIGHT = 526; // model units, head to toe
const registry = new Set(); // capsule figures (upgraded once the model is loaded)
const INVISIBLE = new THREE.MeshBasicMaterial({ colorWrite: false, depthWrite: false, transparent: true, opacity: 0 });
let template = null;
let clips = {};

// The model's texture is a palette in five horizontal bands (top to bottom): skin, eyes, hair, shirt, pants.
const BANDS = [0, 0.187, 0.344, 0.532, 0.719, 1];
const HAIR = ["#2b1d14", "#4a3020", "#7b5a2e", "#151515", "#a87b4f", "#6b6b6b"];

function paletteTexture({ skin, hair, shirt, pants, stripes = false }) {
  const canvas = document.createElement("canvas");
  canvas.width = 64;
  canvas.height = 64;
  const g = canvas.getContext("2d");
  const band = (k, color) => {
    g.fillStyle = color;
    g.fillRect(0, BANDS[k] * 64, 64, (BANDS[k + 1] - BANDS[k]) * 64 + 1);
  };
  band(0, skin);
  band(1, "#1d1d1f");
  band(2, hair);
  // A referee's shirt is marked magenta; the material draws the stripes there (stripedShirt).
  band(3, stripes ? "#ff00ff" : shirt);
  band(4, pants);
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.magFilter = THREE.NearestFilter;
  return texture;
}

const css = (color) => "#" + new THREE.Color(color).getHexString();

// Referee stripes: the palette's shirt band holds one colour for the whole shirt, so the stripes are drawn in the shader,
// around the body (from the bind-pose position, so they move with it), wherever the palette is the magenta marker.
function stripedShirt(material) {
  material.onBeforeCompile = (shader) => {
    shader.vertexShader = shader.vertexShader
      .replace("#include <common>", "#include <common>\nvarying vec3 vBindPos;")
      .replace("#include <begin_vertex>", "#include <begin_vertex>\nvBindPos = position;");
    shader.fragmentShader = shader.fragmentShader
      .replace("#include <common>", "#include <common>\nvarying vec3 vBindPos;")
      .replace("#include <map_fragment>", `#include <map_fragment>
        if (diffuseColor.r > 0.8 && diffuseColor.g < 0.2 && diffuseColor.b > 0.8) {
          float around = atan(vBindPos.z, vBindPos.x) / 6.2831853 * 18.0;
          diffuseColor.rgb = fract(around) < 0.5 ? vec3(0.015) : vec3(0.85);
        }`);
  };
  material.customProgramCacheKey = () => "striped-shirt";
  return material;
}

/** Registers a capsule figure for the skinned model. figure: { root, right, left, neck?, torso?, height?, look }. */
export function registerFigure(figure) {
  registry.add(figure);
  if (template) upgrade(figure);
}

function findBone(model, name) {
  let found = null;
  model.traverse((o) => { if (!found && o.isBone && o.name.endsWith(name)) found = o; });
  return found;
}

function upgrade(figure) {
  if (figure.human) return;
  const look = figure.look || {};
  const seed = figure.seed ?? Math.floor(Math.random() * 1000);
  const model = SkeletonUtils.clone(template);
  const scale = (1.74 * (figure.height || 1)) / MODEL_HEIGHT;
  model.scale.setScalar(scale);
  model.rotation.y = Math.PI / 2; // the model faces +z; the figures face +x
  const material = new THREE.MeshStandardMaterial({ roughness: 0.8, map: paletteTexture({
    skin: css(look.skin ?? 0xd9a07a), hair: look.hair ?? HAIR[seed % HAIR.length], shirt: css(look.shirt ?? 0x2a2d33),
    pants: css(look.pants ?? 0x23262c), stripes: look.stripes }) });
  if (look.stripes) stripedShirt(material);
  model.traverse((o) => {
    if (o.isSkinnedMesh) {
      o.material = material;
      o.castShadow = true;
      o.frustumCulled = false;
    }
  });
  // Hide the capsule body (pivots stay: they carry the gestures and anything held); a clickable part stays clickable.
  figure.root.traverse((o) => {
    if (!o.userData.body) return;
    if (o.userData.pick) o.material = INVISIBLE;
    else o.visible = false;
  });
  figure.root.add(model);
  const mixer = new THREE.AnimationMixer(model);
  const actions = {};
  for (const [name, clip] of Object.entries(clips)) actions[name] = mixer.clipAction(clip);
  const human = {
    model, mixer, actions, current: null, speed: 0, last: null,
    bones: { rightArm: findBone(model, "RightArm"), leftArm: findBone(model, "LeftArm"), head: findBone(model, "Head"),
      spine: findBone(model, "Spine1"), rightForeArm: findBone(model, "RightForeArm"), leftForeArm: findBone(model, "LeftForeArm"),
      rightHand: findBone(model, "RightHand"),
      // Finger and thumb chains, base to tip (4 joints each; the last is the tip end).
      // (This low-poly hand is a mitten: the index chain carries all four fingers; other rigs have each finger.)
      rightFingers: ["Index", "Middle", "Ring", "Pinky"].map((f) => [1, 2, 3, 4].map((k) => findBone(model, `RightHand${f}${k}`)))
        .filter((chain) => chain.every(Boolean)),
      rightThumb: [1, 2, 3, 4].map((k) => findBone(model, `RightHandThumb${k}`)) },
  };
  figure.human = human;
  play(human, "Idle", 0);
  mixer.update(Math.random() * 5); // not everyone in step
}

function play(human, name, fade = 0.3) {
  const next = human.actions[name] || human.actions.Idle;
  if (!next || human.current === next) return;
  next.reset().play();
  if (human.current) human.current.crossFadeTo(next, fade, false);
  human.current = next;
}

// Rotates a bone by angle about a world axis (after the animation has posed it).
const qa = new THREE.Quaternion(), qp = new THREE.Quaternion(), qb = new THREE.Quaternion();
function turnBone(bone, axis, angle) {
  if (!bone || Math.abs(angle) < 1e-4) return;
  bone.parent.getWorldQuaternion(qp);
  bone.getWorldQuaternion(qb);
  qa.setFromAxisAngle(axis, angle).multiply(qb);
  bone.quaternion.copy(qp.invert().multiply(qa));
  bone.updateMatrixWorld(true);
}

// Turns a bone so the segment to its child points along dir (world), blended by weight (0 keeps the animation's pose).
const from = new THREE.Vector3(), to = new THREE.Vector3(), qi = new THREE.Quaternion(), qt = new THREE.Quaternion();
function aimBone(bone, child, dir, weight) {
  if (!bone || !child || weight <= 0) return;
  bone.getWorldPosition(from);
  child.getWorldPosition(to);
  to.sub(from).normalize();
  qt.setFromUnitVectors(to, dir);
  qi.identity().slerp(qt, Math.min(1, weight));
  bone.parent.getWorldQuaternion(qp);
  bone.getWorldQuaternion(qb);
  qa.copy(qi).multiply(qb);
  bone.quaternion.copy(qp.invert().multiply(qa));
  bone.updateMatrixWorld(true);
}

// A thumbs up on the right hand (weight 0..1): the hand carries on along the forearm, the fingers close into a fist
// (knuckles forward, each segment folding back toward the palm) and the thumb points straight up.
const forearmDir = new THREE.Vector3(), worldUp = new THREE.Vector3(0, 1, 0), segment = new THREE.Vector3();
const elbowAt = new THREE.Vector3(), wristAt = new THREE.Vector3();
function thumbsUp(b, weight) {
  if (!b.rightHand || !b.rightForeArm) return;
  b.rightForeArm.getWorldPosition(elbowAt);
  b.rightHand.getWorldPosition(wristAt);
  forearmDir.subVectors(wristAt, elbowAt).normalize();
  // Up, made square to the forearm (a forearm pointing a little up still gets a thumb pointing up).
  const thumbUp = worldUp.clone().addScaledVector(forearmDir, -worldUp.dot(forearmDir)).normalize();
  aimBone(b.rightHand, b.rightFingers[0]?.[0], forearmDir, weight);
  const folds = [
    segment.clone().copy(forearmDir).multiplyScalar(0.35).addScaledVector(thumbUp, -1).normalize(), // down from the knuckles
    forearmDir.clone().multiplyScalar(-1).addScaledVector(thumbUp, -0.25).normalize(), // back toward the wrist
    forearmDir.clone().multiplyScalar(-0.3).addScaledVector(thumbUp, 1).normalize(), // up into the palm
  ];
  for (const chain of b.rightFingers) {
    for (let k = 0; k < 3; k++) aimBone(chain[k], chain[k + 1], folds[k], weight);
  }
  const thumbDir = thumbUp.clone().addScaledVector(forearmDir, 0.15).normalize();
  for (let k = 0; k < 3; k++) aimBone(b.rightThumb[k], b.rightThumb[k + 1], thumbDir, weight);
}

// The direction an arm points for a figure pivot's swing (forward/up, about the side axis) and splay (out to the side).
const aim = new THREE.Vector3();
function armDirection(swing, splay, side) {
  return aim.set(0, 0, 0).addScaledVector(forward, Math.sin(swing)).addScaledVector(up, -Math.cos(swing))
    .addScaledVector(lateral, side * 0.12 - Math.sin(splay)).normalize(); // (a positive splay leans toward the figure's left, as the capsule pivots do)
}

const position = new THREE.Vector3(), lateral = new THREE.Vector3(), forward = new THREE.Vector3(), up = new THREE.Vector3();
const rootQ = new THREE.Quaternion();
function animate(figure, dt) {
  const human = figure.human;
  if (!human || !figure.root.visible) return;
  // Locomotion from how fast the figure is moving across the floor.
  figure.root.getWorldPosition(position);
  if (human.last && dt > 0) {
    const speed = Math.hypot(position.x - human.last.x, position.z - human.last.z) / dt;
    human.speed += (Math.min(speed, 8) - human.speed) * Math.min(1, dt * 6);
  }
  human.last = (human.last || new THREE.Vector3()).copy(position);
  if (human.speed > 2.4) play(human, "Run");
  else if (human.speed > 0.3) play(human, "Walk");
  else play(human, figure.working ? "Working" : "Idle");
  if (human.current === human.actions.Walk) human.current.timeScale = THREE.MathUtils.clamp(human.speed / 1.3, 0.5, 1.6);
  human.mixer.update(dt);
  human.model.updateMatrixWorld(true);
  // The figure's gestures on top: arms swing forward/up about the shoulders' side axis (and splay out), the head
  // turns, the torso leans.
  figure.root.getWorldQuaternion(rootQ);
  lateral.set(0, 0, 1).applyQuaternion(rootQ);
  forward.set(1, 0, 0).applyQuaternion(rootQ);
  up.set(0, 1, 0).applyQuaternion(rootQ);
  if (figure.torso) turnBone(human.bones.spine, lateral, figure.torso.rotation.z);
  // Arms: aimed where the pivot points, blended in with the size of the gesture (a near-rest pivot keeps the idle).
  const b = human.bones;
  for (const [pivot, arm, fore, side] of [[figure.right, b.rightArm, b.rightForeArm, 1], [figure.left, b.leftArm, b.leftForeArm, -1]]) {
    if (!pivot) continue;
    const swing = pivot.rotation.z, splay = pivot.rotation.x;
    const weight = Math.min(1, (Math.abs(swing) + Math.abs(splay)) / 0.45);
    aimBone(arm, fore, armDirection(swing, splay, side), weight);
    // The forearm follows straight on, or bends up at the elbow (right arm: figure.rightElbow).
    const elbow = side > 0 ? figure.rightElbow || 0 : 0;
    aimBone(fore, fore.children.find((c) => c.isBone), armDirection(swing + elbow, splay, side), weight * 0.85 + Math.min(1, elbow));
  }
  if (figure.neck) turnBone(human.bones.head, up, figure.neck.rotation.y);
  if (figure.rightFist > 0.01) thumbsUp(human.bones, figure.rightFist);
}

/** Bakes the idle pose into a static mesh (for the instanced crowd), with the given look. */
export function bakedPerson(look, height = 1) {
  if (!template) return null;
  const model = SkeletonUtils.clone(template);
  const mixer = new THREE.AnimationMixer(model);
  if (clips.Idle) {
    mixer.clipAction(clips.Idle).play();
    mixer.update(2.5);
  }
  model.updateMatrixWorld(true);
  let skinned = null;
  model.traverse((o) => { if (o.isSkinnedMesh && !skinned) skinned = o; });
  const source = skinned.geometry;
  const geometry = new THREE.BufferGeometry();
  const count = source.attributes.position.count;
  const positions = new Float32Array(count * 3);
  const v = new THREE.Vector3();
  const scale = (1.74 * height) / MODEL_HEIGHT;
  const toModel = new THREE.Matrix4().copy(model.matrixWorld).invert();
  for (let i = 0; i < count; i++) {
    skinned.getVertexPosition(i, v); // skinned, in the mesh's space
    v.applyMatrix4(skinned.matrixWorld).applyMatrix4(toModel).multiplyScalar(scale);
    positions.set([v.z, v.y, -v.x], 3 * i); // face +x like the figures
  }
  geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
  geometry.setAttribute("uv", source.attributes.uv);
  if (source.index) geometry.setIndex(source.index);
  geometry.computeVertexNormals();
  const material = new THREE.MeshStandardMaterial({ roughness: 0.85, map: paletteTexture({
    skin: css(look.skin ?? 0xd9a07a), hair: look.hair ?? HAIR[0], shirt: css(look.shirt ?? 0x2a2d33),
    pants: css(look.pants ?? 0x23262c) }) });
  return { geometry, material };
}

/** Loads the model; upgrades every registered figure and keeps them animating. onReady runs once it is loaded. */
export function setupPeople(venue, onReady) {
  new FBXLoader().loadAsync("/models/human.fbx").then((model) => {
    template = model;
    for (const clip of model.animations) clips[clip.name.split("|").pop()] = clip;
    for (const figure of registry) upgrade(figure);
    venue.addUpdater((dt) => {
      for (const figure of registry) animate(figure, Math.min(dt, 0.1));
    });
    onReady?.();
  }).catch((error) => console.error("People model could not be loaded; keeping the simple figures", error));
}

/** Replaces the capsule crowd (an instanced mesh) with people in six looks, standing where the capsules were. */
export function replaceCrowd(crowd) {
  if (!crowd || !template) return;
  const SHIRTS = [0xd8262c, 0x1f5fd6, 0xf3d21c, 0x2b2b2b, 0xe8e8e8, 0x2e8b57, 0x8a4fff, 0xff8c1a];
  const SKINS = [0xf1c7a5, 0xd9a07a, 0xb57a55, 0x8d5a3b, 0x5f3b26];
  const looks = 6;
  const count = crowd.count;
  const groups = [];
  for (let k = 0; k < looks; k++) {
    const baked = bakedPerson({ shirt: SHIRTS[(k * 3) % SHIRTS.length], skin: SKINS[k % SKINS.length],
      pants: k % 2 ? 0x2b3140 : 0x23262c, hair: HAIR[k % HAIR.length] }, 0.95 + 0.02 * k);
    if (!baked) return;
    const mesh = new THREE.InstancedMesh(baked.geometry, baked.material, Math.ceil(count / looks));
    mesh.castShadow = false;
    mesh.receiveShadow = true;
    mesh.count = 0;
    groups.push(mesh);
  }
  const matrix = new THREE.Matrix4(), place = new THREE.Matrix4();
  const p = new THREE.Vector3(), q = new THREE.Quaternion(), s = new THREE.Vector3();
  for (let i = 0; i < count; i++) {
    crowd.getMatrixAt(i, matrix);
    matrix.decompose(p, q, s);
    p.y -= 0.42; // the capsule's center to the person's feet
    // Face the field (the stands are on the audience side), each a little differently.
    q.setFromAxisAngle(new THREE.Vector3(0, 1, 0), Math.PI / 2 + Math.sin(i * 12.9898) * 0.35);
    place.compose(p, q, s.set(1, 1, 1));
    const mesh = groups[i % looks];
    mesh.setMatrixAt(mesh.count++, place);
  }
  for (const mesh of groups) {
    mesh.instanceMatrix.needsUpdate = true;
    crowd.parent.add(mesh);
  }
  crowd.visible = false;
}
