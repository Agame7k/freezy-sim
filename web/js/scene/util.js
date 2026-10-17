// Shared 3D helpers: field↔scene transforms, procedural textures and materials.
import * as THREE from "three";
import { tagGrid } from "./apriltags.js";

export const ctx = { L: 16.541, W: 8.069 };

/** WPILib field coords (x toward red, y across, z up; origin blue-wall corner) → scene (y up, centered). */
export function f2s(x, y, z = 0) {
  return new THREE.Vector3(x - ctx.L / 2, z, -(y - ctx.W / 2));
}

/** Scene → field coords. */
export function s2f(v) {
  return { x: v.x + ctx.L / 2, y: -v.z + ctx.W / 2, z: v.y };
}

/** Places an object at field coords with a field yaw (radians, CCW from +x). */
export function place(obj, x, y, z = 0, yaw = 0) {
  obj.position.copy(f2s(x, y, z));
  obj.rotation.y = yaw;
  return obj;
}

/** Box sized in field axes: dx along field x, dy along field y, dz height. */
export function fieldBox(dx, dy, dz, material) {
  const mesh = new THREE.Mesh(new THREE.BoxGeometry(dx, dz, dy), material);
  mesh.castShadow = true;
  mesh.receiveShadow = true;
  return mesh;
}

export function yawFromQuat([w, , , z]) {
  return 2 * Math.atan2(z, w);
}

export const COLORS = {
  red: 0xd8262c,
  blue: 0x1f5fd6,
  redDim: 0x5a1416,
  blueDim: 0x142a55,
  alu: 0xb8bec6,
  darkAlu: 0x4a5058,
  steel: 0x2c3036,
  carpet: 0x4a4f57,
  fuel: 0xf3d21c,
};

export function allianceColor(alliance) {
  return alliance === "red" ? COLORS.red : COLORS.blue;
}

const materialCache = new Map();
export function mat(key, make) {
  if (!materialCache.has(key)) materialCache.set(key, make());
  return materialCache.get(key);
}

export const M = {
  alu: () => mat("alu", () => new THREE.MeshStandardMaterial({ color: COLORS.alu, metalness: 0.75, roughness: 0.35 })),
  darkAlu: () => mat("darkAlu", () => new THREE.MeshStandardMaterial({ color: COLORS.darkAlu, metalness: 0.6, roughness: 0.45 })),
  steel: () => mat("steel", () => new THREE.MeshStandardMaterial({ color: COLORS.steel, metalness: 0.5, roughness: 0.6 })),
  black: () => mat("black", () => new THREE.MeshStandardMaterial({ color: 0x111316, roughness: 0.8 })),
  white: () => mat("white", () => new THREE.MeshStandardMaterial({ color: 0xf2f2f2, roughness: 0.6 })),
  glass: () => mat("glass", () => new THREE.MeshPhysicalMaterial({
    color: 0xdfe9f5, metalness: 0, roughness: 0.05, transparent: true, opacity: 0.16, depthWrite: false,
    side: THREE.DoubleSide,
  })),
  alliance: (alliance) => mat("alliance-" + alliance, () => new THREE.MeshStandardMaterial({
    color: allianceColor(alliance), roughness: 0.45, metalness: 0.1,
  })),
  hdpe: (alliance) => mat("hdpe-" + alliance, () => new THREE.MeshStandardMaterial({
    color: allianceColor(alliance), roughness: 0.75, map: noiseTexture(64, 0.16), metalness: 0,
  })),
  diamond: () => mat("diamond", () => new THREE.MeshStandardMaterial({
    color: 0xc9cdd3, metalness: 0.85, roughness: 0.38, map: diamondPlateTexture(),
  })),
  fuel: () => mat("fuel", () => new THREE.MeshStandardMaterial({ map: fuelTexture(), roughness: 0.66 })),
};

function canvas(width, height) {
  const element = document.createElement("canvas");
  element.width = width;
  element.height = height;
  return element;
}

export function noiseTexture(size = 128, amount = 0.12, base = "#ffffff") {
  const c = canvas(size, size);
  const g = c.getContext("2d");
  g.fillStyle = base;
  g.fillRect(0, 0, size, size);
  const image = g.getImageData(0, 0, size, size);
  for (let i = 0; i < image.data.length; i += 4) {
    const n = 1 - Math.random() * amount;
    image.data[i] *= n;
    image.data[i + 1] *= n;
    image.data[i + 2] *= n;
  }
  g.putImageData(image, 0, 0);
  const texture = new THREE.CanvasTexture(c);
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

export function carpetTexture() {
  const size = 512;
  const c = canvas(size, size);
  const g = c.getContext("2d");
  g.fillStyle = "#585e67";
  g.fillRect(0, 0, size, size);
  const image = g.getImageData(0, 0, size, size);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const i = (y * size + x) * 4;
      const loop = ((x + (y % 2) * 2) % 4 < 2 ? 0.96 : 1.0) * (1 - Math.random() * 0.13);
      image.data[i] *= loop;
      image.data[i + 1] *= loop;
      image.data[i + 2] *= loop;
    }
  }
  g.putImageData(image, 0, 0);
  const texture = new THREE.CanvasTexture(c);
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.repeat.set(24, 12);
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.anisotropy = 8;
  return texture;
}

export function diamondPlateTexture() {
  const size = 128;
  const c = canvas(size, size);
  const g = c.getContext("2d");
  g.fillStyle = "#b9bdc3";
  g.fillRect(0, 0, size, size);
  g.strokeStyle = "#e8ebee";
  g.lineWidth = 5;
  g.lineCap = "round";
  for (let y = 0; y < size; y += 32) {
    for (let x = 0; x < size; x += 32) {
      g.beginPath();
      g.moveTo(x + 6, y + 6);
      g.lineTo(x + 14, y + 14);
      g.moveTo(x + 22, y + 22);
      g.lineTo(x + 30, y + 14);
      g.stroke();
    }
  }
  const texture = new THREE.CanvasTexture(c);
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.repeat.set(6, 4);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/** A canvas-backed texture whose content can be redrawn (team signs, timers, labels). */
export class CanvasPanel {
  constructor(width, height) {
    this.canvas = canvas(width, height);
    this.g = this.canvas.getContext("2d");
    this.texture = new THREE.CanvasTexture(this.canvas);
    this.texture.colorSpace = THREE.SRGBColorSpace;
    this.texture.anisotropy = 4;
    this.last = null;
  }

  /** Draws centered text; skips work when nothing changed. */
  text(text, { fg = "#fff", bg = "#000", font = "bold 120px 'Segoe UI', sans-serif", sub = "", subFont = "", subFg = "" } = {}) {
    const key = [text, fg, bg, font, sub].join("|");
    if (key === this.last) return;
    this.last = key;
    const { g, canvas: c } = this;
    g.fillStyle = bg;
    g.fillRect(0, 0, c.width, c.height);
    g.fillStyle = fg;
    g.textAlign = "center";
    g.textBaseline = "middle";
    g.font = font;
    g.fillText(text, c.width / 2, sub ? c.height * 0.42 : c.height / 2, c.width * 0.94);
    if (sub) {
      g.font = subFont || "bold 40px 'Segoe UI', sans-serif";
      g.fillStyle = subFg || fg;
      g.fillText(sub, c.width / 2, c.height * 0.82, c.width * 0.94);
    }
    this.texture.needsUpdate = true;
  }
}

export function labelSprite(text, { color = "#e7ecf2", bg = "rgba(11,14,19,0.75)", height = 0.18 } = {}) {
  const panel = new CanvasPanel(512, 128);
  panel.text(text, { fg: color, bg, font: "bold 64px 'Segoe UI', sans-serif" });
  const material = new THREE.SpriteMaterial({ map: panel.texture, depthTest: false, transparent: true });
  const sprite = new THREE.Sprite(material);
  sprite.scale.set(height * 4, height, 1);
  sprite.renderOrder = 10;
  return sprite;
}

/** Deterministic AprilTag-like 8x8 pattern (6x6 data cells inside a black border) for a tag id. */
/** The official tag36h11 image for a 2026 field tag (white margin, black border, 6x6 data cells). */
export function aprilTagTexture(id) {
  const px = 20;
  const c = canvas(10 * px, 10 * px);
  const g = c.getContext("2d");
  g.fillStyle = "#fff";
  g.fillRect(0, 0, c.width, c.height);
  g.fillStyle = "#000";
  const grid = tagGrid(id);
  for (let y = 0; y < 8; y++) {
    for (let x = 0; x < 8; x++) {
      if (grid[y][x]) g.fillRect((1 + x) * px, (1 + y) * px, px, px);
    }
  }
  const texture = new THREE.CanvasTexture(c);
  texture.magFilter = THREE.NearestFilter;
  texture.minFilter = THREE.LinearMipmapLinearFilter;
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

export function tagPick(mesh, info) {
  mesh.userData.pick = info;
  return mesh;
}

// A segmented tower light (the Banner/Patlite style field stack lights): each segment is an LED core inside a
// ribbed, tinted lens, with dark separator rings, a mounting base and a cap or buzzer grille on top. The returned
// lamps are the cores, so callers light a segment by raising its material's emissiveIntensity; the lens follows.
export function towerLight({ colors, radius = 0.05, segment = 0.075, buzzer = false } = {}) {
  const group = new THREE.Group();
  const dark = new THREE.MeshStandardMaterial({ color: 0x1b1d21, roughness: 0.5, metalness: 0.3 });
  const base = new THREE.Mesh(new THREE.CylinderGeometry(radius * 1.02, radius * 1.15, segment * 0.55, 24), dark);
  base.position.y = segment * 0.275;
  group.add(base);
  // Ribbed lens profile (a lathe): a slight fresnel ripple around the segment.
  const profile = [];
  const steps = 12;
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    profile.push(new THREE.Vector2(radius * (0.97 + 0.03 * Math.cos(t * Math.PI * 6)), t * segment * 0.94));
  }
  const lensGeometry = new THREE.LatheGeometry(profile, 28);
  const lamps = [];
  let y = segment * 0.55;
  for (const color of colors) {
    const tint = new THREE.Color(color);
    const core = new THREE.Mesh(new THREE.CylinderGeometry(radius * 0.55, radius * 0.55, segment * 0.8, 16),
      new THREE.MeshStandardMaterial({ color: tint.clone().multiplyScalar(0.35), emissive: tint, emissiveIntensity: 0.04,
        roughness: 0.4 }));
    core.position.y = y + segment * 0.47;
    const lensMaterial = new THREE.MeshStandardMaterial({ color: tint.clone().lerp(new THREE.Color(0xffffff), 0.25),
      emissive: tint, emissiveIntensity: 0.02, transparent: true, opacity: 0.55, roughness: 0.25, depthWrite: false,
      side: THREE.DoubleSide });
    const lens = new THREE.Mesh(lensGeometry, lensMaterial);
    lens.position.y = -segment * 0.47;
    // The lens glows with the core it covers.
    lens.onBeforeRender = () => { lensMaterial.emissiveIntensity = core.material.emissiveIntensity * 0.32; };
    core.add(lens);
    group.add(core);
    lamps.push(core);
    const ring = new THREE.Mesh(new THREE.CylinderGeometry(radius * 1.01, radius * 1.01, segment * 0.06, 24), dark);
    ring.position.y = y + segment * 0.97;
    group.add(ring);
    y += segment;
  }
  let top;
  if (buzzer) {
    top = new THREE.Mesh(new THREE.CylinderGeometry(radius * 0.98, radius, segment * 0.7, 24), dark);
    top.position.y = y + segment * 0.35;
    const grille = new THREE.Mesh(new THREE.CylinderGeometry(radius * 0.6, radius * 0.6, 0.004, 20),
      new THREE.MeshStandardMaterial({ color: 0x050505, roughness: 0.9 }));
    grille.position.y = segment * 0.36;
    top.add(grille);
    y += segment * 0.7;
  } else {
    top = new THREE.Mesh(new THREE.SphereGeometry(radius * 0.98, 20, 8, 0, Math.PI * 2, 0, Math.PI / 2), dark);
    top.scale.y = 0.35;
    top.position.y = y;
    y += radius * 0.35;
  }
  group.add(top);
  group.traverse((child) => { if (child.isMesh) child.castShadow = true; });
  return { group, lamps, top, height: y };
}
/** A high-density foam ball: yellow with darker pores and a molding seam, so a rolling ball reads as rolling. */
export function fuelTexture() {
  const canvas = document.createElement("canvas");
  canvas.width = 256;
  canvas.height = 128;
  const g = canvas.getContext("2d");
  g.fillStyle = "#f3d21c";
  g.fillRect(0, 0, 256, 128);
  let seed = 7;
  const random = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
  for (let i = 0; i < 2600; i++) {
    const shade = 150 + Math.floor(random() * 70);
    g.fillStyle = `rgba(${shade}, ${Math.floor(shade * 0.78)}, 0, ${0.12 + random() * 0.2})`;
    g.fillRect(random() * 256, random() * 128, 1 + random() * 2, 1 + random() * 2);
  }
  // Molding seams: the equator and one meridian ring.
  g.strokeStyle = "rgba(150, 110, 0, 0.55)";
  g.lineWidth = 2;
  g.beginPath();
  g.moveTo(0, 64);
  g.lineTo(256, 64);
  g.moveTo(64, 0);
  g.lineTo(64, 128);
  g.moveTo(192, 0);
  g.lineTo(192, 128);
  g.stroke();
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.anisotropy = 4;
  return texture;
}

