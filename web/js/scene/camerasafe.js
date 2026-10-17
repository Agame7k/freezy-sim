// Camera line-of-sight helper: which field and venue structures stand between a camera and what it is filming, so
// cameras can pick angles that actually see the play and pull in in front of walls instead of clipping through them.
import * as THREE from "three";

export function cameraSafety(venue) {
  let occluders = null; // [{ mesh, opaque }]
  let collectedAt = 0;
  let cachedFor = null;
  let allMeshes = [];
  let opaqueMeshes = [];
  const ray = new THREE.Raycaster();
  const box = new THREE.Box3();
  const size = new THREE.Vector3();

  // Field elements and the venue's furniture (tables, stands, walls), but not floors or tape, the sky dome or
  // anything enormous, nor robots, FUEL and people (they move, and the cameras are there to watch them).
  function collect(now) {
    if (occluders && now - collectedAt < 15000) return occluders;
    collectedAt = now;
    occluders = [];
    const roots = [];
    venue.scene.traverse((object) => {
      if (object.parent === venue.scene && object !== venue.camera) roots.push(object);
    });
    const skip = new Set(Object.values(venue.robots || {}).map((robot) => robot.root));
    for (const root of roots) {
      if (skip.has(root) || /fuel|official|drive-?team|crew|ai-overlay|robot/i.test(root.name || "")) continue;
      root.traverse((object) => {
        if (!object.isMesh || object.isInstancedMesh || !object.visible) return;
        box.setFromObject(object);
        box.getSize(size);
        if (box.max.y < 0.08) return; // carpet, tape, floor plates
        if (size.x > 40 || size.z > 40 || size.y > 25) return; // the hall itself
        const material = Array.isArray(object.material) ? object.material[0] : object.material;
        const opaque = !(material && material.transparent && (material.opacity ?? 1) < 0.6);
        occluders.push({ mesh: object, opaque });
      });
    }
    return occluders;
  }

  // The first structure between from and to (only opaque ones when opaqueOnly), as the distance from `from`.
  function firstHit(from, to, opaqueOnly) {
    const list = collect(performance.now());
    const direction = to.clone().sub(from);
    const distance = direction.length();
    if (distance < 1e-3) return null;
    ray.set(from, direction.divideScalar(distance));
    ray.near = 0.15;
    ray.far = distance;
    if (list !== cachedFor) {
      cachedFor = list;
      allMeshes = list.map((o) => o.mesh);
      opaqueMeshes = list.filter((o) => o.opaque).map((o) => o.mesh);
    }
    const hits = ray.intersectObjects(opaqueOnly ? opaqueMeshes : allMeshes, false);
    return hits.length ? hits[0].distance : null;
  }

  return {
    /** Can a camera at `pos` see `target` (nothing opaque in between)? */
    sees(pos, target) {
      return firstHit(target, pos, true) === null;
    },
    /** Where a camera that wants to be at `desired` (looking at `target`) can actually stand: pulled in toward the
     * target to just in front of the first structure (see-through ones too: a camera inside a panel looks wrong). */
    clear(target, desired, margin = 0.25) {
      const distance = firstHit(target, desired, false);
      if (distance === null) return { pos: desired, blocked: false };
      const direction = desired.clone().sub(target).normalize();
      return { pos: target.clone().add(direction.multiplyScalar(Math.max(0.4, distance - margin))), blocked: true };
    },
    refresh() { occluders = null; },
  };
}
