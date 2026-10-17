// Camera presets (orbit, overhead, audience, scoring table, driver-station POVs, robot follow) with smooth moves.
import * as THREE from "three";
import { ctx, f2s } from "./util.js";
import { h } from "../core/dom.js";
import { displayById, displayUrl } from "../core/displays.js";
import { cameraSafety } from "./camerasafe.js";

export function setupCameras(venue, layout) {
  const { camera, controls, renderer } = venue;
  const { L, W } = ctx;
  const ortho = new THREE.OrthographicCamera(-10, 10, 6, -6, 0.1, 100);
  ortho.position.set(0, 40, 0);
  ortho.up.set(0, 0, -1);
  ortho.lookAt(0, 0, 0);

  const dsCenters = layout.driverStation.centers;
  const presets = {
    orbit: { label: "Orbit", pos: f2s(L / 2 - 3.5, -4.8, 8.6), target: f2s(L / 2, W / 2, 0.3) },
    audience: { label: "Audience", pos: f2s(L / 2, -11.5, 6.8), target: f2s(L / 2, W / 2 + 0.8, 1.0) },
    table: { label: "Scoring table", pos: f2s(L / 2, W + 2.4, 2.2), target: f2s(L / 2, W / 2 - 1, 0.4) },
    broadcast: { label: "Broadcast", pos: f2s(L / 2, -2.2, 5.2), target: f2s(L / 2, W / 2 + 0.4, 0.2), fov: 58 },
    director: { label: "Auto broadcast", pos: f2s(L / 2, -3.2, 6.2), target: f2s(L / 2, W / 2, 0.3), fov: 55 },
    blueEnd: { label: "Blue end", pos: f2s(-6, W / 2, 4.5), target: f2s(L * 0.35, W / 2, 0.4) },
    redEnd: { label: "Red end", pos: f2s(L + 6, W / 2, 4.5), target: f2s(L * 0.65, W / 2, 0.4) },
    overhead: { label: "Overhead", ortho: true },
  };
  for (const [station, y] of Object.entries(dsCenters)) {
    const blue = station[0] === "B";
    const x = blue ? -0.75 : L + 0.75;
    presets[`ds-${station}`] = {
      label: station, ds: true,
      pos: f2s(x, y, 1.7),
      target: f2s(blue ? L * 0.55 : L * 0.45, y + (W / 2 - y) * 0.35, 0.3),
      fov: 62,
    };
  }

  let current = "orbit";
  let tween = null;
  let follow = null;
  const followLast = new THREE.Vector3();
  let followYaw = 0;
  const safety = cameraSafety(venue);
  venue.cameraSafety = safety;

  function fitOrtho() {
    const width = renderer.domElement.clientWidth || 1;
    const height = renderer.domElement.clientHeight || 1;
    const aspect = width / height;
    const fieldAspect = (L + 1.5) / (W + 1.5);
    let halfW;
    let halfH;
    if (aspect > fieldAspect) {
      halfH = (W + 1.5) / 2;
      halfW = halfH * aspect;
    } else {
      halfW = (L + 1.5) / 2;
      halfH = halfW / aspect;
    }
    Object.assign(ortho, { left: -halfW, right: halfW, top: halfH, bottom: -halfH });
    ortho.updateProjectionMatrix();
  }

  // Broadcast mode: the arena's real audience overlay (transparent background) composited over the 3D view.
  let overlay = null;
  function setOverlay(on) {
    if (on && !overlay) {
      overlay = h("iframe", {
        src: displayUrl(displayById("audience"), { background: "transparent", displayId: "sim-broadcast", nickname: "Sim Broadcast Overlay" }),
        allow: "autoplay", allowtransparency: "true",
        style: { position: "absolute", inset: "0", width: "100%", height: "100%", border: "0", pointerEvents: "none",
          background: "transparent", colorScheme: "normal" },
      });
      venue.hud.prepend(overlay);
    } else if (!on && overlay) {
      overlay.remove();
      overlay = null;
    }
  }

  function go(name) {
    const preset = presets[name];
    if (!preset) return;
    current = name;
    follow = null;
    setOverlay(name === "broadcast" || name === "director");
    if (name === "director") {
      venue.activeCamera = camera;
      try { localStorage.setItem("freezy-sim.camera", name); } catch { /* ignore */ }
      updateButtons();
      venue.director?.start();
      return;
    }
    venue.director?.stop();
    if (preset.ortho) {
      fitOrtho();
      venue.activeCamera = ortho;
      controls.enabled = false;
    } else {
      venue.activeCamera = camera;
      controls.enabled = true;
      tween = {
        fromPos: camera.position.clone(), fromTarget: controls.target.clone(),
        toPos: preset.pos.clone(), toTarget: preset.target.clone(),
        fromFov: camera.fov, toFov: preset.fov || 50, t: 0,
      };
    }
    try { localStorage.setItem("freezy-sim.camera", name); } catch { /* ignore */ }
    updateButtons();
  }

  /** Follow a robot object (root group) from behind. */
  function followRobot(object, label) {
    follow = object;
    followLast.copy(object.position);
    followYaw = object.rotation.y;
    venue.director?.stop(); // (the auto-broadcast director would take the camera straight back)
    setOverlay(false);
    current = "follow";
    venue.activeCamera = camera;
    controls.enabled = true;
    hud.textContent = "";
    updateButtons(label);
  }

  venue.addUpdater((dt) => {
    if (tween) {
      tween.t = Math.min(1, tween.t + dt / 0.9);
      const k = tween.t < 0.5 ? 2 * tween.t * tween.t : 1 - Math.pow(-2 * tween.t + 2, 2) / 2;
      camera.position.lerpVectors(tween.fromPos, tween.toPos, k);
      controls.target.lerpVectors(tween.fromTarget, tween.toTarget, k);
      camera.fov = tween.fromFov + (tween.toFov - tween.fromFov) * k;
      camera.updateProjectionMatrix();
      if (tween.t >= 1) tween = null;
    }
    if (follow) {
      // Chase from behind the direction of travel (a swerve robot spinning in place must not whip the camera
      // around), rate-limited, slightly ahead of the robot; pulled in in front of anything solid in the way, and
      // kept low enough under the trench arms.
      const moved = follow.position.clone().sub(followLast);
      followLast.copy(follow.position);
      const speed = moved.length() / Math.max(dt, 1e-3);
      const travel = speed > 0.6 ? Math.atan2(-moved.z, moved.x) : follow.rotation.y;
      let diff = travel - followYaw;
      diff = Math.atan2(Math.sin(diff), Math.cos(diff));
      followYaw += Math.max(-dt * 1.6, Math.min(dt * 1.6, diff));
      const ahead = new THREE.Vector3(Math.cos(followYaw), 0, -Math.sin(followYaw));
      const target = follow.position.clone().add(new THREE.Vector3(0, 0.45, 0)).add(ahead.clone().multiplyScalar(0.8));
      const desired = follow.position.clone().add(ahead.clone().multiplyScalar(-2.8)).add(new THREE.Vector3(0, 1.35, 0));
      const safe = safety.clear(target, desired, 0.3).pos;
      if (safe.y < 0.35) safe.y = 0.35;
      camera.position.lerp(safe, Math.min(1, dt * (safe === desired ? 4 : 10))); // snap faster when something is in the way
      controls.target.lerp(target, Math.min(1, dt * 6));
    }
  });
  venue.onResize = () => fitOrtho();

  // HUD buttons.
  const bar = h("div.hud-bar");
  const hud = h("span");
  const buttons = new Map();
  const groups = [
    ["orbit", "director", "audience", "broadcast", "table", "overhead", "blueEnd", "redEnd"],
    ["ds-B1", "ds-B2", "ds-B3", "ds-R1", "ds-R2", "ds-R3"],
  ];
  for (const names of groups) {
    for (const name of names) {
      const button = h("button.btn.sm", { onclick: () => go(name), title: `Camera: ${presets[name].label}` },
        presets[name].ds ? `👁 ${presets[name].label}` : presets[name].label);
      buttons.set(name, button);
      bar.appendChild(button);
    }
    bar.appendChild(h("span", { style: { width: "8px" } }));
  }
  venue.hud.appendChild(bar);
  function updateButtons() {
    for (const [name, button] of buttons) button.classList.toggle("active", name === current);
  }

  let initial = "orbit";
  try { initial = localStorage.getItem("freezy-sim.camera") || "orbit"; } catch { /* ignore */ }
  camera.position.copy(presets.orbit.pos);
  controls.target.copy(presets.orbit.target);
  go(presets[initial] ? initial : "orbit");

  return { go, followRobot, presets, ortho, current: () => current };
}
