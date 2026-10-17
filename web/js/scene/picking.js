// Mouse picking for the 3D venue: hover tooltips and click handlers keyed by object kind.
import * as THREE from "three";
import { h } from "../core/dom.js";

export function setupPicking(venue) {
  const raycaster = new THREE.Raycaster();
  const pointer = new THREE.Vector2();
  const handlers = new Map();
  const tip = h("div.hud-tip");
  venue.hud.appendChild(tip);
  const element = venue.renderer.domElement;
  let pending = null;
  let downAt = null;

  function pick(event) {
    const rect = element.getBoundingClientRect();
    pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
    pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
    raycaster.setFromCamera(pointer, venue.activeCamera || venue.camera);
    const hits = raycaster.intersectObjects(venue.scene.children, true);
    for (const hit of hits) {
      let object = hit.object;
      while (object && !object.userData.pick) object = object.parent;
      if (object && object.userData.pick && object.visible) {
        if (object.userData.pick.kind === "carpet") return { info: object.userData.pick, hit, object };
        return { info: object.userData.pick, hit, object };
      }
    }
    return null;
  }

  element.addEventListener("pointermove", (event) => {
    if (pending) return;
    pending = requestAnimationFrame(() => {
      pending = null;
      const result = pick(event);
      const clickable = result && handlers.has(result.info.kind);
      if (result && result.info.label) {
        tip.style.display = "block";
        tip.textContent = result.info.label + (clickable ? "  (click)" : "");
        const rect = element.getBoundingClientRect();
        tip.style.left = `${event.clientX - rect.left + 14}px`;
        tip.style.top = `${event.clientY - rect.top + 10}px`;
      } else {
        tip.style.display = "none";
      }
      element.style.cursor = clickable ? "pointer" : "";
    });
  });
  element.addEventListener("pointerleave", () => { tip.style.display = "none"; });
  element.addEventListener("pointerdown", (event) => { downAt = [event.clientX, event.clientY]; });
  element.addEventListener("pointerup", (event) => {
    if (!downAt || Math.hypot(event.clientX - downAt[0], event.clientY - downAt[1]) > 4) return;
    const result = pick(event);
    if (!result) return;
    const handler = handlers.get(result.info.kind);
    if (handler) handler(result.info, result, event);
  });

  return {
    on(kind, handler) { handlers.set(kind, handler); },
    pick,
  };
}
