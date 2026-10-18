// Live arena displays inside the 3D venue. Each venue screen gets an iframe of the real arena page placed in 3D with
// CSS3DRenderer; the WebGL screen mesh is swapped for a transparent "hole" material so the iframe shows through and
// still gets occluded correctly by anything in front of it.
import * as THREE from "three";
import { CSS3DRenderer, CSS3DObject } from "three/addons/renderers/CSS3DRenderer.js";
import { h, replace } from "../core/dom.js";
import { icon } from "../core/icons.js";
import { displayById, displayUrl, SCREEN_DISPLAYS } from "../core/displays.js";
import { on } from "../core/store.js";

const PIXEL_WIDTH = { audience: 1920, default: 1280 };

export function setupLiveScreens(venue) {
  const cssRenderer = new CSS3DRenderer({ element: venue.cssHost });
  const cssScene = new THREE.Scene();
  const holeMaterial = new THREE.MeshBasicMaterial({ color: 0x000000, opacity: 0, transparent: false, blending: THREE.NoBlending, side: THREE.DoubleSide });
  const screens = [];
  let enabled = false;
  try { enabled = localStorage.getItem("freezy-sim.liveScreens") !== "off"; } catch { enabled = true; }

  const allScreens = { ...venue.env.screens };
  for (const [station, part] of Object.entries(venue.field.teamDisplays || {})) allScreens[`as-${station}`] = part;
  for (const [screenId, part] of Object.entries(allScreens)) {
    const displayId = SCREEN_DISPLAYS[screenId];
    if (!displayId || !part?.screen) continue;
    const pixelWidth = PIXEL_WIDTH[screenId] || PIXEL_WIDTH.default;
    const pixelHeight = Math.round(pixelWidth * (part.height / part.width));
    screens.push({ screenId, displayId, part, pixelWidth, pixelHeight, object: null, originalMaterial: part.screen.material,
      wanted: false, hiddenSince: 0 });
  }

  function attach(screen) {
    const display = displayById(screen.displayId);
    if (!display) return;
    const iframe = h("iframe.css3d-screen", {
      src: displayUrl(display),
      style: {
        width: `${screen.pixelWidth}px`, height: `${screen.pixelHeight}px`, border: "0", background: "#000",
        pointerEvents: "none",
      },
      allow: "autoplay",
    });
    const object = new CSS3DObject(iframe);
    screen.object = object;
    cssScene.add(object);
    screen.part.screen.material = holeMaterial;
  }

  function detach(screen) {
    if (!screen.object) return;
    cssScene.remove(screen.object);
    screen.object.element.remove();
    screen.object = null;
    screen.part.screen.material = screen.originalMaterial;
  }

  function apply() {
    // Live pages are attached on demand (see the screen budget in afterRender); turning the feature off drops them all.
    if (!enabled) for (const screen of screens) detach(screen);
    button.classList.toggle("active", enabled);
    replace(button, icon("tv", 13), enabled ? "Live screens on" : "Live screens off");
  }

  const button = h("button.btn.sm", {
    style: { position: "absolute", right: "10px", top: "46px" },
    title: "Show the real arena pages on the venue screens",
    onclick: () => {
      enabled = !enabled;
      try { localStorage.setItem("freezy-sim.liveScreens", enabled ? "on" : "off"); } catch { /* ignore */ }
      apply();
    },
  });
  venue.hud.appendChild(button);

  // Iframes need the arena URL from sim.info; attach once it is known.
  let applied = false;
  on("sim.info", () => {
    if (!applied) {
      applied = true;
      apply();
    }
  });

  const position = new THREE.Vector3();
  const quaternion = new THREE.Quaternion();
  const scale = new THREE.Vector3();
  const toCamera = new THREE.Vector3();
  const normal = new THREE.Vector3();
  venue.onResizeCss = (width, height) => cssRenderer.setSize(width, height);
  // Screen budget: every live page is a full arena display (scripts and websockets), so only the nearest screens the
  // camera can see run live (how many depends on the graphics quality). A screen out of view for a while is unloaded
  // and shows its placeholder until it is wanted again.
  const frustum = new THREE.Frustum();
  const viewProjection = new THREE.Matrix4();
  let budgetAt = 0;
  function chooseLive(camera, now) {
    viewProjection.multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse);
    frustum.setFromProjectionMatrix(viewProjection);
    const candidates = [];
    for (const screen of screens) {
      const mesh = screen.part.screen;
      mesh.updateWorldMatrix(true, false);
      mesh.matrixWorld.decompose(position, quaternion, scale);
      normal.set(0, 0, 1).applyQuaternion(quaternion);
      toCamera.copy(camera.position).sub(position);
      const distance = toCamera.length();
      // Facing the camera, inside the view, close enough that a person could read it.
      const readable = distance < 6 + 14 * screen.part.width;
      const inView = normal.dot(toCamera) > 0 && readable && frustum.intersectsObject(mesh);
      candidates.push({ screen, inView, distance: distance / Math.max(0.3, screen.part.width) });
    }
    candidates.sort((a, b) => (a.inView === b.inView ? a.distance - b.distance : a.inView ? -1 : 1));
    const budget = venue.screenBudget || 12;
    candidates.forEach(({ screen, inView }, rank) => {
      screen.wanted = inView && rank < budget;
      if (screen.wanted) {
        screen.hiddenSince = 0;
        if (!screen.object) attach(screen);
      } else if (screen.object) {
        if (!screen.hiddenSince) screen.hiddenSince = now;
        if (now - screen.hiddenSince > 15000) detach(screen);
      }
    });
  }

  venue.afterRender = (camera) => {
    if (!enabled) return;
    const now = performance.now();
    if (now - budgetAt > 250) {
      budgetAt = now;
      chooseLive(camera, now);
    }
    for (const screen of screens) {
      if (!screen.object) continue;
      const mesh = screen.part.screen;
      mesh.updateWorldMatrix(true, false);
      mesh.matrixWorld.decompose(position, quaternion, scale);
      const visible = screen.wanted;
      screen.object.visible = visible;
      screen.object.element.style.display = visible ? "" : "none";
      // A hidden live screen shows its placeholder in the 3D image rather than a hole.
      screen.part.screen.material = visible ? holeMaterial : screen.originalMaterial;
      screen.object.position.copy(position);
      screen.object.quaternion.copy(quaternion);
      const s = screen.part.width / screen.pixelWidth;
      screen.object.scale.set(s, s, s);
    }
    cssRenderer.render(cssScene, camera);
  };
  const host = venue.renderer.domElement.parentElement;
  cssRenderer.setSize(host.clientWidth, host.clientHeight);
}
