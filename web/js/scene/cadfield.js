// The official field CAD in the venue. It is FIRST's 2026 REBUILT playing field STEP
// (FE-2026-rev-rebuilt-playing-field.step) tessellated by tools/cad/export_field.py: fasteners, tape, FUEL, carpet and
// AprilTags left out, the rest merged into one mesh per element, material and alliance (node names
// "Element|material|side"). It is in meters with the field center at the origin and z up.
//
// The mesh is not shipped: each user makes their own from FIRST's STEP and imports the .glb (graphics menu, or copy it
// to run/models/field-cad.glb; docs/FIELD_MODEL.md). The server serves it at /models/field-cad.glb when present.
//
// When it is shown, it replaces the static structure the field builder draws (marked userData.cad in field.js); the
// live parts that the simulation drives (hub LEDs, glow, exit rollers, the chute door, FUEL in the chute and corral,
// signs, buttons, screens) stay. Structure that can be clicked becomes invisible but stays pickable.
import * as THREE from "three";
import { GLTFLoader } from "three/addons/loaders/GLTFLoader.js";
import { M, allianceColor, diamondPlateTexture } from "./util.js";
import { netTexture } from "./field.js";

const INVISIBLE = new THREE.MeshBasicMaterial({ colorWrite: false, depthWrite: false, transparent: true, opacity: 0 });

function materialFor(kind, side) {
  switch (kind) {
    case "alliance":
      return new THREE.MeshStandardMaterial({ color: allianceColor(side), roughness: 0.55, flatShading: true });
    case "clear":
      return new THREE.MeshStandardMaterial({ color: 0xa6b4c2, roughness: 0.08, metalness: 0, transparent: true,
        opacity: 0.16, depthWrite: false, side: THREE.DoubleSide, flatShading: true });
    case "hdpe":
      return new THREE.MeshStandardMaterial({ color: 0xeceeef, roughness: 0.62, flatShading: true });
    case "diamond": {
      const map = diamondPlateTexture().clone();
      map.wrapS = map.wrapT = THREE.RepeatWrapping;
      map.repeat.set(3, 3); // box-projected UVs are in meters
      map.needsUpdate = true;
      return new THREE.MeshStandardMaterial({ color: 0xc3c8ce, map, metalness: 0.75, roughness: 0.38 });
    }
    case "net": {
      const map = netTexture().clone();
      map.wrapS = map.wrapT = THREE.RepeatWrapping;
      map.repeat.set(4, 4);
      map.needsUpdate = true;
      return new THREE.MeshStandardMaterial({ map, transparent: true, side: THREE.DoubleSide, depthWrite: false, roughness: 0.9 });
    }
    case "rubber":
      return new THREE.MeshStandardMaterial({ color: 0x1b1c1e, roughness: 0.92, flatShading: true });
    case "dark":
      return new THREE.MeshStandardMaterial({ color: 0x2a2d33, metalness: 0.35, roughness: 0.55, flatShading: true });
    case "steel":
      return new THREE.MeshStandardMaterial({ color: 0x8d939b, metalness: 0.7, roughness: 0.45, flatShading: true });
    default: {
      const alu = M.alu().clone();
      alu.flatShading = true;
      return alu;
    }
  }
}

/** Box-projected UVs (meters) for meshes whose texture needs them: each triangle maps on the plane it faces most. */
function boxProjectUVs(geometry) {
  const g = geometry.index ? geometry.toNonIndexed() : geometry;
  const p = g.attributes.position;
  const uv = new Float32Array(p.count * 2);
  const a = new THREE.Vector3(), b = new THREE.Vector3(), c = new THREE.Vector3(), n = new THREE.Vector3();
  for (let i = 0; i < p.count; i += 3) {
    a.fromBufferAttribute(p, i);
    b.fromBufferAttribute(p, i + 1);
    c.fromBufferAttribute(p, i + 2);
    n.subVectors(b, a).cross(c.clone().sub(a));
    const ax = Math.abs(n.x), ay = Math.abs(n.y), az = Math.abs(n.z);
    for (let k = 0; k < 3; k++) {
      const v = [a, b, c][k];
      const [u, w] = az >= ax && az >= ay ? [v.x, v.y] : ax >= ay ? [v.y, v.z] : [v.x, v.z];
      uv[2 * (i + k)] = u;
      uv[2 * (i + k) + 1] = w;
    }
  }
  g.setAttribute("uv", new THREE.BufferAttribute(uv, 2));
  return g;
}

export function setupCadField(venue) {
  let cad = null; // the loaded CAD scene
  let loading = null;
  let shown = false;
  const hidden = []; // [object, how] the procedural structure we replaced
  // Whether a mesh has been imported (GET /api/field-model); installed is null until the first check.
  const status = { installed: null, bytes: 0, path: "run/models/field-cad.glb" };
  venue.fieldModelStatus = status;

  async function refreshStatus() {
    try {
      const response = await fetch("/api/field-model", { cache: "no-store" });
      if (response.ok) Object.assign(status, await response.json());
      else status.installed = false;
    } catch {
      status.installed = false;
    }
    venue.onFieldModelStatus?.(status);
    return status;
  }

  function unload() {
    cad?.parent?.remove(cad);
    cad?.traverse((object) => {
      object.geometry?.dispose();
      object.material?.map?.dispose();
      object.material?.dispose?.();
    });
    cad = null;
    loading = null;
  }

  function load() {
    loading ??= new GLTFLoader().loadAsync("/models/field-cad.glb").then((gltf) => {
      const root = gltf.scene;
      root.name = "field-cad";
      root.rotation.x = -Math.PI / 2; // CAD z-up, field center origin → scene y-up (same as f2s)
      root.traverse((object) => {
        if (!object.isMesh) return;
        const [element, kind, side] = object.name.split("|");
        if (kind === "diamond" || kind === "net") object.geometry = boxProjectUVs(object.geometry);
        // The export has no normals; the colour pass shades flat anyway, but the AO pass reads normals (NaN without).
        if (!object.geometry.attributes.normal) object.geometry.computeVertexNormals();
        object.material = materialFor(kind, side);
        const clear = kind === "clear" || kind === "net";
        object.castShadow = !clear;
        object.receiveShadow = true;
        object.userData.cadElement = element;
      });
      cad = root;
      return root;
    });
    return loading;
  }

  function setStructureVisible(visible) {
    if (!visible) {
      venue.fieldGroup.traverse((object) => {
        if (!object.userData.cad) return;
        object.traverse((part) => {
          if (!part.isMesh && !part.isLineSegments) return;
          let picked = false;
          for (let o = part; o && !picked; o = o.parent) picked = Boolean(o.userData.pick);
          if (picked) {
            hidden.push([part, part.material]);
            part.material = INVISIBLE; // still clickable
          } else if (part.visible) {
            hidden.push([part, null]);
            part.visible = false;
          }
        });
      });
    } else {
      for (const [part, material] of hidden) {
        if (material) part.material = material;
        else part.visible = true;
      }
      hidden.length = 0;
    }
  }

  venue.setFieldModel = async (model) => {
    const wantCad = model === "cad";
    if (wantCad === shown) return;
    shown = wantCad;
    if (wantCad) {
      if (!status.installed) await refreshStatus(); // also notices a file copied into run/models by hand
      if (!status.installed) { // nothing imported yet: the simple field stays (the menu offers the import)
        shown = false;
        return;
      }
      try {
        const root = await load();
        if (!shown) return; // switched back while loading
        venue.scene.add(root);
        setStructureVisible(false);
      } catch (error) {
        console.error("Field CAD could not be loaded; keeping the simple field", error);
        loading = null; // let a later import or retry load again
        shown = false;
      }
    } else {
      cad?.parent?.remove(cad);
      setStructureVisible(true);
    }
  };

  /** Uploads a .glb made by tools/cad/export_field.py and shows it if the CAD field is selected. */
  venue.importFieldModel = async (file, model) => {
    const response = await fetch("/api/field-model", { method: "PUT", body: file });
    const result = await response.json().catch(() => ({ error: response.statusText }));
    if (!response.ok) throw new Error(result.error || `import failed (${response.status})`);
    const wasShown = shown;
    if (wasShown) await venue.setFieldModel("simple");
    unload(); // the next show loads the new file
    Object.assign(status, result);
    venue.onFieldModelStatus?.(status);
    if (wasShown || model === "cad") await venue.setFieldModel("cad");
    return status;
  };

  venue.removeFieldModel = async () => {
    await venue.setFieldModel("simple");
    unload();
    await fetch("/api/field-model", { method: "DELETE" });
    await refreshStatus();
  };

  refreshStatus();
}
