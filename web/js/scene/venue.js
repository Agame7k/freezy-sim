// 3D venue bootstrap: renderer, the procedural 2026 field, the event venue, robots, fuel, cameras and picking.
import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { RoomEnvironment } from "three/addons/environments/RoomEnvironment.js";
import { h } from "../core/dom.js";
import { ctx, f2s } from "./util.js";
import { loadLayout, buildField } from "./field.js";
import { buildEnvironment } from "./environment.js";
import { FuelRenderer, stagedFuel } from "./fuel.js";
import { createRobot } from "./robots.js";
import { setupCameras } from "./cameras.js";
import { setupPicking } from "./picking.js";
import { setupQuality } from "./quality.js";
import { setupCadField } from "./cadfield.js";

export { f2s };

export async function createVenue({ host, cssHost, hud }) {
  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: "high-performance" });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = THREE.PCFSoftShadowMap;
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.35;
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  host.appendChild(renderer.domElement);

  const scene = new THREE.Scene();
  scene.background = new THREE.Color(0x07090c);
  scene.fog = new THREE.Fog(0x07090c, 35, 85);
  // A soft room environment for reflections: aluminum, polycarbonate and diamond plate pick up the arena around them
  // (kept low, so the venue's own lights still set the look).
  const pmrem = new THREE.PMREMGenerator(renderer);
  scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
  scene.environmentIntensity = 0.35;
  pmrem.dispose();

  const camera = new THREE.PerspectiveCamera(50, 1, 0.05, 300);
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.maxPolarAngle = Math.PI * 0.495;
  controls.minDistance = 0.5;
  controls.maxDistance = 60;

  const updaters = new Set();
  const venue = {
    THREE, scene, camera, renderer, controls, hud, cssHost, activeCamera: camera,
    addUpdater(fn) { updaters.add(fn); return () => updaters.delete(fn); },
  };

  const layout = await loadLayout();
  venue.layout = layout;
  const field = buildField(layout);
  scene.add(field.group);
  venue.field = field.parts;
  venue.fieldGroup = field.group;
  setupCadField(venue); // the official field CAD replaces the built structure (quality setting "Field model")
  venue.env = buildEnvironment(scene, layout);

  venue.fuel = new FuelRenderer(scene, layout.fuel.diameter);
  venue.fuel.setFieldPositions(stagedFuel(layout));

  // Six robots at their starting positions (driven by the world simulation once it runs).
  venue.robots = {};
  const starts = { 1: 6.55, 2: 4.035, 3: 1.55 };
  for (const station of ["R1", "R2", "R3", "B1", "B2", "B3"]) {
    const alliance = station[0] === "R" ? "red" : "blue";
    const robot = createRobot({ alliance, archetype: station.endsWith("2") ? "tall" : "scorer" });
    let x = layout.zones.allianceZoneDepth - 0.5;
    let y = starts[station[1]];
    let heading = 0;
    if (alliance === "red") {
      x = ctx.L - x;
      y = ctx.W - y;
      heading = Math.PI;
    }
    robot.root.position.copy(f2s(x, y, 0));
    robot.root.rotation.y = heading;
    robot.setHopperFuel(8);
    scene.add(robot.root);
    venue.robots[station] = robot;
  }

  venue.cameras = setupCameras(venue, layout);
  venue.picking = setupPicking(venue);

  function resize() {
    const width = host.clientWidth || 1;
    const height = host.clientHeight || 1;
    renderer.setSize(width, height, false);
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    camera.aspect = width / height;
    camera.updateProjectionMatrix();
    venue.quality?.resize(width, height);
    venue.onResize?.(width, height);
    venue.onResizeCss?.(width, height);
  }
  new ResizeObserver(resize).observe(host);
  resize();

  venue.quality = setupQuality(venue, hud);
  resize();

  const fps = h("div.hud-corner.mono", "— fps");
  hud.appendChild(fps);
  hud.appendChild(h("div.hud-help", "Drag: orbit · Right-drag: pan · Wheel: zoom · Click devices, screens and robots"));
  let frames = 0;
  let lastFps = performance.now();

  const clock = new THREE.Clock();
  renderer.setAnimationLoop(() => {
    const dt = Math.min(clock.getDelta(), 0.1);
    const now = performance.now();
    for (const fn of updaters) {
      // One broken feature must never stop the render loop (three.js stops scheduling frames after an exception).
      try {
        fn(dt, now);
      } catch (err) {
        if (!fn.failed) console.error("3D updater failed (disabled):", err);
        fn.failed = (fn.failed || 0) + 1;
        if (fn.failed > 30) updaters.delete(fn);
      }
    }
    if (venue.activeCamera === camera) controls.update();
    venue.quality.render(venue.activeCamera);
    venue.afterRender?.(venue.activeCamera);
    frames++;
    if (now - lastFps > 1000) {
      fps.textContent = `${Math.round((frames * 1000) / (now - lastFps))} fps`;
      frames = 0;
      lastFps = now;
    }
  });

  // Live devices are bound after the static scene exists.
  const { bindDevices } = await import("./devices.js");
  bindDevices(venue);
  const { bindWinnerShow } = await import("./winnershow.js");
  bindWinnerShow(venue); // after the devices, so the show's hub pulse wins over the plain hub-light coil
  const { connectWorldStream } = await import("./worldstream.js");
  connectWorldStream(venue);
  const { setupDriving } = await import("./drive.js");
  setupDriving(venue);
  const { bindAiOverlay } = await import("./aioverlay.js");
  bindAiOverlay(venue);
  const { setupDriveTeams } = await import("./driveteams.js");
  setupDriveTeams(venue, layout);
  const { setupBetweenMatch } = await import("./betweenmatch.js");
  setupBetweenMatch(venue);
  const { setupReferees } = await import("./referees.js");
  setupReferees(venue, layout);
  // Rigged, animated people in place of the capsule figures, and the crowd (people.js).
  const { setupPeople, replaceCrowd } = await import("./people.js");
  setupPeople(venue, () => replaceCrowd(venue.env.crowd));
  const { setupDirector } = await import("./director.js");
  setupDirector(venue, layout);
  renderer.domElement.tabIndex = 0;

  window.venue = venue;
  return venue;
}
