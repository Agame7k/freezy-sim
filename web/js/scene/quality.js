// Graphics quality: Low / Medium / High presets (picked automatically from the GPU the first time, then remembered),
// HDR bloom so LEDs, stack lights and signs glow, ambient occlusion and MSAA on High, LED light spill onto the field,
// and tone mapping (ACES or AgX) with an exposure slider. The composer keeps the canvas alpha so the live CSS3D screens
// still show through their "holes" in the WebGL image.
import * as THREE from "three";
import { EffectComposer } from "three/addons/postprocessing/EffectComposer.js";
import { RenderPass } from "three/addons/postprocessing/RenderPass.js";
import { UnrealBloomPass } from "three/addons/postprocessing/UnrealBloomPass.js";
import { GTAOPass } from "three/addons/postprocessing/GTAOPass.js";
import { OutputPass } from "three/addons/postprocessing/OutputPass.js";
import { Pass, FullScreenQuad } from "three/addons/postprocessing/Pass.js";
import { CopyShader } from "three/addons/shaders/CopyShader.js";
import { RectAreaLightUniformsLib } from "three/addons/lights/RectAreaLightUniformsLib.js";
import { h } from "../core/dom.js";

const STORAGE = "freezy-sim.graphics";
export const PRESETS = {
  low: { label: "Low", pixelRatio: 1, shadowMap: 2048, bloom: false, ao: false, msaa: 0, spill: "none", screens: 6 },
  medium: { label: "Medium", pixelRatio: 1.5, shadowMap: 4096, bloom: true, ao: false, msaa: 4, spill: "point", screens: 12 },
  high: { label: "High", pixelRatio: 2, shadowMap: 4096, bloom: true, ao: true, msaa: 4, spill: "area", screens: 24 },
};

function loadSettings() {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE) || "null");
    if (saved && PRESETS[saved.preset]) return saved;
  } catch { /* storage unavailable */ }
  return null;
}

function saveSettings(settings) {
  try { localStorage.setItem(STORAGE, JSON.stringify(settings)); } catch { /* ignore */ }
}

/** A first guess at what the GPU can handle. */
function autoPreset(renderer) {
  const gl = renderer.getContext();
  const info = gl.getExtension("WEBGL_debug_renderer_info");
  const name = info ? String(gl.getParameter(info.UNMASKED_RENDERER_WEBGL)) : "";
  if (/SwiftShader|llvmpipe|Microsoft Basic|Software/i.test(name) || /Mobi|Android/i.test(navigator.userAgent)) return "low";
  if (/Intel|Iris|UHD|Mali|Adreno|Apple GPU/i.test(name)) return "medium";
  return "high";
}

// The live CSS3D screens show through pixels the scene leaves transparent. Bloom and AO write opaque alpha, so the
// scene's own alpha is saved straight after it is rendered and put back before output.
class SaveAlphaPass extends Pass {
  constructor() {
    super();
    this.needsSwap = false;
    this.target = new THREE.WebGLRenderTarget(1, 1, { type: THREE.HalfFloatType });
    this.quad = new FullScreenQuad(new THREE.ShaderMaterial({
      uniforms: { tDiffuse: { value: null }, opacity: { value: 1 } },
      vertexShader: CopyShader.vertexShader, fragmentShader: CopyShader.fragmentShader,
      blending: THREE.NoBlending, depthTest: false, depthWrite: false,
    }));
  }
  setSize(width, height) { this.target.setSize(width, height); }
  render(renderer, writeBuffer, readBuffer) {
    this.quad.material.uniforms.tDiffuse.value = readBuffer.texture;
    renderer.setRenderTarget(this.target);
    this.quad.render(renderer);
  }
  dispose() { this.target.dispose(); this.quad.dispose(); }
}

class RestoreAlphaPass extends Pass {
  constructor(save) {
    super();
    this.save = save;
    this.quad = new FullScreenQuad(new THREE.ShaderMaterial({
      uniforms: { tDiffuse: { value: null }, tAlpha: { value: null } },
      vertexShader: CopyShader.vertexShader,
      fragmentShader: /* glsl */ `
        uniform sampler2D tDiffuse;
        uniform sampler2D tAlpha;
        varying vec2 vUv;
        void main() {
          float a = texture2D(tAlpha, vUv).a;
          gl_FragColor = vec4(texture2D(tDiffuse, vUv).rgb * a, a);
        }`,
      blending: THREE.NoBlending, depthTest: false, depthWrite: false,
    }));
  }
  render(renderer, writeBuffer, readBuffer) {
    this.quad.material.uniforms.tDiffuse.value = readBuffer.texture;
    this.quad.material.uniforms.tAlpha.value = this.save.target.texture;
    renderer.setRenderTarget(this.renderToScreen ? null : writeBuffer);
    this.quad.render(renderer);
  }
  dispose() { this.quad.dispose(); }
}

export function setupQuality(venue, hud) {
  const { renderer, scene } = venue;
  const settings = loadSettings() || { preset: autoPreset(renderer), exposure: 1.3, toneMapping: "aces", auto: true };
  // The official field CAD (about a million triangles) on Medium and High; the simple built field on Low.
  settings.field ??= settings.preset === "low" ? "simple" : "cad";
  let composer = null;
  let renderPass = null;
  let aoPass = null;
  let spillLights = [];
  let areaLibReady = false;
  const keyLight = scene.children.find((o) => o.isDirectionalLight && o.castShadow);

  function size() {
    const canvas = renderer.domElement;
    return { width: canvas.clientWidth || 1, height: canvas.clientHeight || 1 };
  }

  function buildComposer(preset) {
    composer?.dispose?.();
    composer = null;
    renderPass = null;
    aoPass = null;
    if (!preset.bloom && !preset.ao) return;
    const { width, height } = size();
    const ratio = renderer.getPixelRatio();
    const target = new THREE.WebGLRenderTarget(width * ratio, height * ratio, {
      type: THREE.HalfFloatType, samples: preset.msaa,
    });
    composer = new EffectComposer(renderer, target);
    renderPass = new RenderPass(scene, venue.activeCamera);
    composer.addPass(renderPass);
    const saveAlpha = new SaveAlphaPass();
    composer.addPass(saveAlpha);
    if (preset.ao) {
      aoPass = new GTAOPass(scene, venue.activeCamera, width, height);
      aoPass.blendIntensity = 0.85;
      composer.addPass(aoPass);
    }
    if (preset.bloom) {
      // Only HDR highlights bloom: LED lenses, lit lamps, sign digits.
      const bloom = new UnrealBloomPass(new THREE.Vector2(width / 2, height / 2), 0.55, 0.38, 3.0);
      composer.addPass(bloom);
    }
    composer.addPass(new RestoreAlphaPass(saveAlpha));
    composer.addPass(new OutputPass());
    composer.setSize(width, height);
  }

  function buildSpill(preset) {
    for (const light of spillLights) light.parent?.remove(light);
    spillLights = [];
    for (const alliance of ["red", "blue"]) {
      const leds = venue.field.hubs[alliance]?.leds;
      if (!leds) continue;
      leds.sideLights = [];
      leds.hubLight = null;
      if (preset.spill === "area") {
        if (!areaLibReady) {
          RectAreaLightUniformsLib.init();
          areaLibReady = true;
        }
        for (const side of leds.sides) {
          const light = new THREE.RectAreaLight(0x000000, 0, side.width, 0.45);
          light.position.copy(side.center);
          light.lookAt(side.center.clone().add(side.normal));
          leds.group.add(light);
          leds.sideLights.push(light);
          spillLights.push(light);
        }
      } else if (preset.spill === "point") {
        const light = new THREE.PointLight(0x000000, 0, 6, 1.6);
        light.position.copy(leds.sides[0].center.clone().setX(0).setZ(0));
        light.position.y = 1.5;
        leds.group.add(light);
        leds.hubLight = light;
        spillLights.push(light);
      }
      leds.shown.fill(-1); // force the next update to push colours to the new lights
    }
  }

  function apply() {
    const preset = PRESETS[settings.preset];
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, preset.pixelRatio));
    renderer.toneMapping = settings.toneMapping === "agx" ? THREE.AgXToneMapping : THREE.ACESFilmicToneMapping;
    renderer.toneMappingExposure = settings.exposure;
    if (keyLight) {
      keyLight.shadow.mapSize.set(preset.shadowMap, preset.shadowMap);
      keyLight.shadow.map?.dispose();
      keyLight.shadow.map = null;
    }
    renderer.shadowMap.type = preset.shadowMap > 2048 ? THREE.PCFSoftShadowMap : THREE.PCFShadowMap;
    // Materials must recompile for the tone mapping / light changes.
    scene.traverse((object) => {
      if (object.material) (Array.isArray(object.material) ? object.material : [object.material]).forEach((m) => { m.needsUpdate = true; });
    });
    buildSpill(preset);
    buildComposer(preset);
    venue.screenBudget = preset.screens;
    venue.onQualityChange?.(preset);
    venue.setFieldModel?.(settings.field);
    saveSettings(settings);
    renderControls();
  }

  // HUD control: preset, tone mapping and exposure.
  const panel = h("div.hud-graphics");
  hud.appendChild(panel);
  let open = false;
  let importState = ""; // "", "importing", or the last import error
  venue.onFieldModelStatus = () => { if (open) renderControls(); };

  // The official CAD is not shipped (FIRST's field model): say so, and offer to import a .glb made with FreeCAD.
  function fieldModelControls() {
    const status = venue.fieldModelStatus;
    if (!status || status.installed === null) return null;
    const picker = h("input", { type: "file", accept: ".glb,model/gltf-binary", style: { display: "none" },
      onchange: async (event) => {
        const file = event.target.files?.[0];
        if (!file) return;
        importState = "importing";
        renderControls();
        try {
          settings.field = "cad";
          saveSettings(settings);
          await venue.importFieldModel(file, settings.field);
          importState = "";
        } catch (error) {
          importState = error.message;
        }
        renderControls();
      } });
    const busy = importState === "importing";
    return h("div.hud-field-model",
      h("div.small.muted", status.installed
        ? `Official CAD imported (${(status.bytes / 1e6).toFixed(1)} MB)`
        : "The official CAD isn't included. Convert FIRST's field STEP with FreeCAD (docs/FIELD_MODEL.md), then import the .glb."),
      importState && !busy ? h("div.small.hud-field-model-error", importState) : null,
      h("div.row",
        h("button.btn.sm.ghost", { onclick: () => picker.click(), disabled: busy },
          busy ? "Importing…" : status.installed ? "Replace .glb…" : "Import .glb…"),
        status.installed && !busy ? h("button.btn.sm.ghost", { title: `Delete ${status.path}`,
          onclick: async () => { await venue.removeFieldModel(); renderControls(); } }, "Remove") : null,
        picker));
  }
  function renderControls() {
    const children = [
      h("button.btn.sm.ghost", { title: "Graphics quality", onclick: () => { open = !open; renderControls(); } },
        `⚙ ${PRESETS[settings.preset].label}`),
      open ? h("div.hud-graphics-menu",
        h("div.small.muted", "Quality"),
        h("div.row", Object.entries(PRESETS).map(([id, p]) => h(`button.btn.sm${settings.preset === id ? ".primary" : ""}`, {
          onclick: () => { settings.preset = id; settings.auto = false; apply(); },
        }, p.label))),
        h("div.small.muted", "Tone mapping"),
        h("div.row", [["aces", "ACES"], ["agx", "AgX"]].map(([id, label]) => h(`button.btn.sm${settings.toneMapping === id ? ".primary" : ""}`, {
          onclick: () => { settings.toneMapping = id; apply(); },
        }, label))),
        h("div.small.muted", "Field model"),
        h("div.row", [["cad", "Official CAD"], ["simple", "Simple"]].map(([id, label]) => h(`button.btn.sm${settings.field === id ? ".primary" : ""}`, {
          onclick: () => { settings.field = id; apply(); },
        }, label))),
        fieldModelControls(),
        h("div.small.muted", `Exposure ${settings.exposure.toFixed(2)}`),
        h("input", { type: "range", min: 0.6, max: 2.2, step: 0.05, value: settings.exposure, style: { width: "160px" },
          oninput: (e) => { settings.exposure = Number(e.target.value); renderer.toneMappingExposure = settings.exposure; },
          onchange: () => { saveSettings(settings); renderControls(); } }),
        settings.auto ? h("div.small.muted", "Picked automatically for this GPU") : null) : null];
    panel.replaceChildren(...children.filter(Boolean));
  }

  apply();
  return {
    render(camera) {
      if (!composer) {
        renderer.render(scene, camera);
        return;
      }
      renderPass.camera = camera;
      if (aoPass) aoPass.camera = camera;
      composer.render();
    },
    resize(width, height) {
      composer?.setSize(width, height);
      aoPass?.setSize?.(width, height);
    },
    settings,
  };
}
