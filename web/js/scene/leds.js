// LED look for the hub DMX bars (sACN). Each bar is a real fixture: an aluminium channel, 8 LED pixels and a frosted
// diffuser lens. The lens is a small shader that blends neighbouring pixel colours into one continuous glow with a
// hotspot over each LED, in HDR so the bloom pass picks it up. Frames are interpolated so the arena's pulse and rainbow
// modes look fluid, and the bars throw coloured light onto the hub, carpet and robots (quality permitting).
import * as THREE from "three";

export const BAR_PIXELS = 8;
export const BARS = 8; // 4 sides × (bottom, top)
const COUNT = BAR_PIXELS * BARS;
const LED_GAIN = 4.5; // HDR brightness of a fully lit lens

const vertexShader = /* glsl */ `
  varying vec2 vUv;
  void main() {
    vUv = uv;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`;

const fragmentShader = /* glsl */ `
  uniform sampler2D uPixels;
  uniform float uBar;
  uniform float uGain;
  varying vec2 vUv;
  void main() {
    // The geometry's +x runs against the pixel order (see makeLens), so flip.
    float u = 1.0 - vUv.x;
    float pos = u * ${BAR_PIXELS.toFixed(1)};
    vec3 diffuse = vec3(0.0);
    vec3 hotspot = vec3(0.0);
    float weights = 0.0;
    for (int p = 0; p < ${BAR_PIXELS}; p++) {
      float d = pos - (float(p) + 0.5);
      vec3 c = texture2D(uPixels, vec2((uBar * ${BAR_PIXELS.toFixed(1)} + float(p) + 0.5) / ${COUNT.toFixed(1)}, 0.5)).rgb;
      float w = exp(-d * d * 1.6);
      diffuse += c * w;
      weights += w;
      hotspot += c * exp(-d * d * 18.0);
    }
    diffuse /= max(weights, 1e-4);
    // Brightest along the lens centre line, fading to its edges.
    float across = 1.0 - pow(abs(vUv.y - 0.5) * 2.0, 2.0);
    vec3 color = (diffuse * (0.55 + 0.45 * across) + hotspot * 0.6 * across) * uGain;
    // A dark lens still reads as frosted plastic.
    color = max(color, vec3(0.018, 0.019, 0.021));
    gl_FragColor = vec4(color, 1.0);
    #include <tonemapping_fragment>
    #include <colorspace_fragment>
  }
`;

/** Per-hub LED state: a pixel texture shared by the hub's lenses, target and displayed colours. */
export class HubLeds {
  constructor() {
    this.data = new Float32Array(COUNT * 4);
    this.target = new Float32Array(COUNT * 3);
    this.shown = new Float32Array(COUNT * 3);
    for (let i = 0; i < COUNT; i++) this.data[i * 4 + 3] = 1;
    this.texture = new THREE.DataTexture(this.data, COUNT, 1, THREE.RGBAFormat, THREE.FloatType);
    this.texture.magFilter = THREE.NearestFilter;
    this.texture.minFilter = THREE.NearestFilter;
    this.texture.needsUpdate = true;
    this.lenses = [];
    this.sideLights = [];
    this.hubLight = null;
    this.pixels = null; // the InstancedMesh of LED chips
  }

  /** A lens material for one bar. */
  material(bar) {
    return new THREE.ShaderMaterial({
      uniforms: { uPixels: { value: this.texture }, uBar: { value: bar }, uGain: { value: LED_GAIN } },
      vertexShader, fragmentShader, toneMapped: true,
    });
  }

  /** New colours from the arena (sRGB bytes, hex-encoded, fixture order). */
  setTarget(hex) {
    for (let i = 0; i < COUNT && i * 6 + 6 <= hex.length; i++) {
      for (let c = 0; c < 3; c++) {
        const srgb = parseInt(hex.substr(i * 6 + c * 2, 2), 16) / 255;
        this.target[i * 3 + c] = srgb <= 0.04045 ? srgb / 12.92 : Math.pow((srgb + 0.055) / 1.055, 2.4);
      }
    }
  }

  /** Eases the shown colours toward the latest frame and pushes them to the GPU, chips and lights. */
  update(dt, chipColor) {
    const k = Math.min(1, dt * 30);
    let changed = false;
    for (let i = 0; i < COUNT * 3; i++) {
      const delta = this.target[i] - this.shown[i];
      if (Math.abs(delta) > 1e-4) {
        this.shown[i] += delta * k;
        changed = true;
      }
    }
    if (!changed) return;
    for (let i = 0; i < COUNT; i++) {
      this.data[i * 4] = this.shown[i * 3];
      this.data[i * 4 + 1] = this.shown[i * 3 + 1];
      this.data[i * 4 + 2] = this.shown[i * 3 + 2];
      if (this.pixels) {
        chipColor.setRGB(Math.max(this.shown[i * 3], 0.03) * 1.6, Math.max(this.shown[i * 3 + 1], 0.03) * 1.6,
          Math.max(this.shown[i * 3 + 2], 0.035) * 1.6, THREE.LinearSRGBColorSpace);
        this.pixels.setColorAt(i, chipColor);
      }
    }
    this.texture.needsUpdate = true;
    if (this.pixels) this.pixels.instanceColor.needsUpdate = true;
    // Light spill: each side's average colour (both bars), or the whole hub's for the single point light.
    const total = [0, 0, 0];
    for (let side = 0; side < 4; side++) {
      const sum = [0, 0, 0];
      for (let i = side * 2 * BAR_PIXELS; i < (side + 1) * 2 * BAR_PIXELS; i++) {
        for (let c = 0; c < 3; c++) sum[c] += this.shown[i * 3 + c];
      }
      const light = this.sideLights[side];
      if (light) {
        const n = 2 * BAR_PIXELS;
        light.color.setRGB(sum[0] / n, sum[1] / n, sum[2] / n, THREE.LinearSRGBColorSpace);
        light.intensity = 4 * Math.max(sum[0], sum[1], sum[2]) / n;
      }
      for (let c = 0; c < 3; c++) total[c] += sum[c];
    }
    if (this.hubLight) {
      const peak = Math.max(...total) / COUNT;
      if (peak > 0) this.hubLight.color.setRGB(total[0] / COUNT / peak, total[1] / COUNT / peak, total[2] / COUNT / peak);
      this.hubLight.intensity = 3.5 * peak;
    }
  }
}

/**
 * A diffuser lens for one bar, centred at `center` (scene coords, hub-local), `length` long, facing `normal` with the
 * pixel order running along `along` (both hub-local field directions mapped to scene axes by the caller).
 */
export function makeLens(leds, bar, length, center, along3, normal3) {
  const geometry = new THREE.PlaneGeometry(length, 0.05);
  const lens = new THREE.Mesh(geometry, leds.material(bar));
  // Right-handed basis: x = −along, y = up, z = outward normal (the shader flips u back).
  const x = along3.clone().negate();
  const basis = new THREE.Matrix4().makeBasis(x, new THREE.Vector3(0, 1, 0), normal3);
  lens.quaternion.setFromRotationMatrix(basis);
  lens.position.copy(center);
  lens.userData.bloom = true;
  leds.lenses.push(lens);
  return lens;
}
