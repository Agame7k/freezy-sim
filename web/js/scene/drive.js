// Manual driving: keyboard (WASD/arrows, Q/E, Space, Shift, C) or an Xbox-style gamepad drives one robot.
// Motion is relative to the view: from a driver-station camera "up" drives away from that wall, like a real driver.
import * as THREE from "three";
import { h, toast } from "../core/dom.js";
import { cmd } from "../core/store.js";

const MAX_SPEED = 4.5;
const MAX_OMEGA = 6;

export function setupDriving(venue) {
  let station = null;
  const keys = new Set();
  const hud = h("div", {
    style: {
      position: "absolute", left: "50%", bottom: "38px", transform: "translateX(-50%)", display: "none",
      background: "#0b0e13e6", border: "1px solid var(--accent)", borderRadius: "8px", padding: "6px 12px",
      fontSize: "12px", textAlign: "center",
    },
  });
  venue.hud.appendChild(hud);

  function start(name) {
    station = name;
    hud.style.display = "block";
    hud.innerHTML = `<b style="color:var(--accent)">Driving ${name}</b> · WASD/arrows move · Q/E turn · Space shoot · Shift intake · C climb (hold) · Esc release` +
      `<div class="small muted">Robot only moves while the FMS enables its driver station. Gamepad: sticks drive, RT shoot, LT intake, A climb.</div>`;
    venue.renderer.domElement.focus();
  }

  function stop() {
    if (!station) return;
    cmd("world.robot", { station, manual: false }).catch(() => {});
    station = null;
    hud.style.display = "none";
    keys.clear();
  }

  window.addEventListener("keydown", (event) => {
    if (!station || event.target.closest?.("input,select,textarea")) return;
    if (event.key === "Escape") {
      stop();
      return;
    }
    keys.add(event.key.toLowerCase());
    if ([" ", "arrowup", "arrowdown", "arrowleft", "arrowright"].includes(event.key.toLowerCase())) event.preventDefault();
  });
  window.addEventListener("keyup", (event) => keys.delete(event.key.toLowerCase()));
  window.addEventListener("blur", () => keys.clear());

  // Field-frame directions for "forward" and "right" based on the current view.
  const forward = new THREE.Vector3();
  function viewAxes() {
    const camera = venue.activeCamera || venue.camera;
    if (camera.isOrthographicCamera) {
      return { fx: 0, fy: 1, rx: 1, ry: 0 }; // overhead: up = +y
    }
    camera.getWorldDirection(forward);
    // Scene (x, z) → field (x, y = -z)
    let fx = forward.x;
    let fy = -forward.z;
    const length = Math.hypot(fx, fy) || 1;
    fx /= length;
    fy /= length;
    return { fx, fy, rx: fy, ry: -fx };
  }

  let lastSend = 0;
  venue.addUpdater((dt, now) => {
    if (!station || now - lastSend < 40) return;
    lastSend = now;
    let moveForward = 0;
    let moveRight = 0;
    let turn = 0;
    let shoot = false;
    let intake = false;
    let climb = 0;
    if (keys.has("w") || keys.has("arrowup")) moveForward += 1;
    if (keys.has("s") || keys.has("arrowdown")) moveForward -= 1;
    if (keys.has("d") || keys.has("arrowright")) moveRight += 1;
    if (keys.has("a") || keys.has("arrowleft")) moveRight -= 1;
    if (keys.has("q")) turn += 1;
    if (keys.has("e")) turn -= 1;
    if (keys.has(" ")) shoot = true;
    if (keys.has("shift")) intake = true;
    if (keys.has("c")) climb = 3;

    const pad = [...(navigator.getGamepads?.() || [])].find(Boolean);
    if (pad) {
      const dead = (v) => (Math.abs(v) < 0.12 ? 0 : v);
      moveRight += dead(pad.axes[0] || 0);
      moveForward -= dead(pad.axes[1] || 0);
      turn -= dead(pad.axes[2] || 0);
      shoot ||= (pad.buttons[7]?.value || 0) > 0.4;
      intake ||= (pad.buttons[6]?.value || 0) > 0.4;
      if (pad.buttons[0]?.pressed) climb = 3;
    }
    const axes = viewAxes();
    const magnitude = Math.min(1, Math.hypot(moveForward, moveRight));
    const scale = magnitude > 0 ? MAX_SPEED * magnitude / Math.hypot(moveForward, moveRight) : 0;
    const vx = (axes.fx * moveForward + axes.rx * moveRight) * scale;
    const vy = (axes.fy * moveForward + axes.ry * moveRight) * scale;
    cmd("world.drive", { station, vx, vy, omega: turn * MAX_OMEGA, intake, shoot, climb }).catch(() => {});
  });

  venue.drive = { start, stop, station: () => station };
  return venue.drive;
}

export function startDriving(venue, station) {
  if (!venue?.drive) {
    toast("3D view is not ready yet", "warn");
    return;
  }
  venue.drive.start(station);
}
