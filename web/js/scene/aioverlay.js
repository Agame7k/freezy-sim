// AI overlay for the 3D view: each robot's planned path, its current target, and a tether from defenders to the robot
// they are marking. Toggled from the AI panel (remembered per viewer).
import * as THREE from "three";
import { on } from "../core/store.js";
import { f2s } from "./util.js";

const STORAGE_KEY = "freezy-sim.aiOverlay";
let enabled = false;
try { enabled = localStorage.getItem(STORAGE_KEY) === "1"; } catch { /* storage unavailable */ }
const listeners = new Set();

export function aiOverlayEnabled() {
  return enabled;
}

export function setAiOverlay(value) {
  enabled = !!value;
  try { localStorage.setItem(STORAGE_KEY, enabled ? "1" : "0"); } catch { /* ignore */ }
  for (const fn of listeners) fn(enabled);
}

const COLORS = { R: 0xff6b70, B: 0x5fa2ff };

/** Sets a line's points. three.js r170 updates an existing position buffer in place, so a different point count needs
 * a new geometry. */
function setLinePoints(line, points) {
  const attribute = line.geometry.getAttribute("position");
  if (!attribute || attribute.count !== points.length) {
    line.geometry.dispose();
    line.geometry = new THREE.BufferGeometry().setFromPoints(points);
  } else {
    line.geometry.setFromPoints(points);
  }
}

/** Builds the overlay in the venue scene. */
export function bindAiOverlay(venue) {
  const group = new THREE.Group();
  group.name = "ai-overlay";
  group.visible = enabled;
  venue.scene.add(group);
  listeners.add((value) => { group.visible = value; });

  const robots = new Map(); // station → { path, target, tether }
  function parts(station) {
    if (robots.has(station)) return robots.get(station);
    const color = COLORS[station[0]] || 0xffffff;
    const lineMaterial = new THREE.LineDashedMaterial({ color, dashSize: 0.18, gapSize: 0.1, transparent: true, opacity: 0.85, depthTest: false });
    const path = new THREE.Line(new THREE.BufferGeometry(), lineMaterial);
    path.renderOrder = 10;
    const target = new THREE.Mesh(new THREE.RingGeometry(0.16, 0.24, 24),
      new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.9, side: THREE.DoubleSide, depthTest: false }));
    target.rotation.x = -Math.PI / 2;
    target.renderOrder = 10;
    const tether = new THREE.Line(new THREE.BufferGeometry(),
      new THREE.LineBasicMaterial({ color: 0xffd34d, transparent: true, opacity: 0.9, depthTest: false }));
    tether.renderOrder = 10;
    // The selected AUTO routine drawn on the carpet (AdvantageScope style), and a ghost where the robot's own pose
    // estimate thinks it is (odometry + vision): the gap between robot and ghost is the odometry error.
    const autoPath = new THREE.Line(new THREE.BufferGeometry(),
      new THREE.LineBasicMaterial({ color, transparent: true, opacity: 0.45, depthTest: false }));
    autoPath.renderOrder = 9;
    const ghost = new THREE.Group();
    const half = 0.42;
    const outline = [[-half, -half], [half, -half], [half, half], [-half, half], [-half, -half]]
      .map(([x, z]) => new THREE.Vector3(x, 0, z));
    outline.push(new THREE.Vector3(half, 0, -half), new THREE.Vector3(half + 0.25, 0, 0), new THREE.Vector3(half, 0, half));
    ghost.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(outline),
      new THREE.LineBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.7, depthTest: false })));
    ghost.renderOrder = 11;
    group.add(path, target, tether, autoPath, ghost);
    const entry = { path, target, tether, autoPath, ghost, autoKey: "" };
    robots.set(station, entry);
    return entry;
  }

  let latest = null;
  on("world.ai", (ai) => { latest = ai; });

  const at = (station, height) => {
    const robot = venue.robots[station];
    return robot && robot.root.visible ? robot.root.position.clone().setY(height) : null;
  };
  venue.addUpdater((dt, now) => {
    if (!enabled || !latest) return;
    for (const r of latest.robots) {
      const p = parts(r.station);
      const start = at(r.station, 0.06);
      const active = !!(r.team && start);
      // Path: from the robot through the planned waypoints to the target.
      const points = [];
      if (active) points.push(start);
      for (const [x, y] of r.path || []) points.push(f2s(x, y, 0.06));
      if (r.target) points.push(f2s(r.target[0], r.target[1], 0.06));
      p.path.visible = active && points.length >= 2;
      if (p.path.visible) {
        setLinePoints(p.path, points);
        p.path.computeLineDistances();
      }
      p.target.visible = !!(active && r.target);
      if (p.target.visible) {
        p.target.position.copy(f2s(r.target[0], r.target[1], 0.04));
        const pulse = 1 + 0.15 * Math.sin(now / 180);
        p.target.scale.set(pulse, pulse, pulse);
      }
      const mark = r.mark && at(r.mark, 0.5);
      p.tether.visible = !!(active && mark);
      if (p.tether.visible) {
        setLinePoints(p.tether, [at(r.station, 0.5), mark]);
      }
      const autoPoints = (r.auto && r.auto.path) || [];
      const showAuto = active && autoPoints.length >= 2 && /^auto/.test(r.state || "");
      p.autoPath.visible = showAuto;
      if (showAuto) {
        const key = autoPoints.map((q) => q.join(",")).join(";");
        if (key !== p.autoKey) {
          p.autoKey = key;
          setLinePoints(p.autoPath, autoPoints.map(([x, y]) => f2s(x, y, 0.03)));
        }
      }
      p.ghost.visible = !!(active && r.est && r.odoError > 0.04);
      if (p.ghost.visible) {
        p.ghost.position.copy(f2s(r.est[0], r.est[1], 0.05));
        p.ghost.rotation.y = r.est[2];
      }
    }
  });
}
