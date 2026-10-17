// FUEL rendering: one instanced mesh for every ball on the field. Positions come from the physics stream (or the
// static match-start staging until the world simulation is running). Each ball keeps its own orientation: on the
// carpet it rolls by exactly the distance it moved, in the air it keeps the spin it left the ground (or a shooter)
// with, so the foam texture shows the motion.
import * as THREE from "three";
import { f2s, M } from "./util.js";

export const MAX_FUEL = 640;
const MAX_IDS = 2048;

export class FuelRenderer {
  constructor(scene, diameter = 0.15) {
    const geometry = new THREE.SphereGeometry(diameter / 2, 16, 12);
    this.mesh = new THREE.InstancedMesh(geometry, M.fuel(), MAX_FUEL);
    this.mesh.castShadow = true;
    this.mesh.receiveShadow = true;
    this.mesh.count = 0;
    this.mesh.frustumCulled = false;
    this.mesh.name = "fuel";
    this.dummy = new THREE.Object3D();
    this.radius = diameter / 2;
    // Per-ball state by id: last field position, orientation, and spin (axis·rad per second, scene frame).
    this.last = new Float32Array(MAX_IDS * 3).fill(NaN);
    this.orientation = Array.from({ length: MAX_IDS }, () => new THREE.Quaternion());
    this.spin = new Float32Array(MAX_IDS * 3);
    this.lastTime = 0;
    this.axis = new THREE.Vector3();
    this.turn = new THREE.Quaternion();
    scene.add(this.mesh);
  }

  /** positions: flat array of field-frame x, y, z (meters); ids: the same ball's id each frame (or null). */
  setFieldPositions(positions, count = positions.length / 3, ids = null) {
    const n = Math.min(count, MAX_FUEL);
    const now = performance.now() / 1000;
    const dt = this.lastTime ? Math.min(0.1, now - this.lastTime) : 0;
    this.lastTime = now;
    const r = this.radius;
    for (let i = 0; i < n; i++) {
      const x = positions[3 * i];
      const y = positions[3 * i + 1];
      const z = positions[3 * i + 2];
      const id = ids ? ids[i] % MAX_IDS : i;
      const q = this.orientation[id];
      const lx = this.last[3 * id];
      if (ids && dt > 0 && !Number.isNaN(lx)) {
        const dx = x - lx;
        const dy = y - this.last[3 * id + 1];
        const moved = Math.hypot(dx, dy);
        if (moved < 0.6) {
          if (z < r + 0.02 && moved > 1e-5) {
            // Rolling: about the horizontal axis across the motion (field up × motion), by distance over radius.
            // Field (a, b, c) is scene (a, c, −b).
            const angle = moved / r;
            this.axis.set(-dy / moved, 0, -dx / moved);
            this.spin[3 * id] = this.axis.x * angle / dt;
            this.spin[3 * id + 1] = 0;
            this.spin[3 * id + 2] = this.axis.z * angle / dt;
            this.turn.setFromAxisAngle(this.axis, angle);
            q.premultiply(this.turn);
          } else if (z >= r + 0.02) {
            // Airborne: keep spinning.
            const sx = this.spin[3 * id], sy = this.spin[3 * id + 1], sz = this.spin[3 * id + 2];
            const rate = Math.hypot(sx, sy, sz);
            if (rate > 1e-3) {
              this.axis.set(sx / rate, sy / rate, sz / rate);
              this.turn.setFromAxisAngle(this.axis, rate * dt);
              q.premultiply(this.turn);
            }
          }
        }
      }
      this.last[3 * id] = x;
      this.last[3 * id + 1] = y;
      this.last[3 * id + 2] = z;
      this.dummy.position.copy(f2s(x, y, z));
      this.dummy.quaternion.copy(q);
      this.dummy.updateMatrix();
      this.mesh.setMatrixAt(i, this.dummy.matrix);
    }
    this.mesh.count = n;
    this.mesh.instanceMatrix.needsUpdate = true;
  }
}

/** Official match-start staging (neutral zone grid + depots); chute fuel and preloads are not on the carpet. */
export function stagedFuel(layout) {
  const positions = [];
  const r = layout.fuel.diameter / 2;
  const L = layout.field.length;
  const W = layout.field.width;
  const box = layout.fuel.neutralBox;
  const nx = 12;
  const ny = 30;
  for (let i = 0; i < nx; i++) {
    for (let j = 0; j < ny; j++) {
      positions.push(
        box.centerX - box.lengthX / 2 + (i + 0.5) * (box.lengthX / nx),
        box.centerY - box.widthY / 2 + (j + 0.5) * (box.widthY / ny),
        r,
      );
    }
  }
  const depot = layout.depot;
  for (const alliance of ["blue", "red"]) {
    for (let i = 0; i < 4; i++) {
      for (let j = 0; j < 6; j++) {
        let x = 0.09 + i * 0.165;
        let y = depot.centerY.blue - depot.width / 2 + 0.09 + j * 0.177;
        if (alliance === "red") {
          x = L - x;
          y = W - y;
        }
        positions.push(x, y, r);
      }
    }
  }
  return positions;
}
