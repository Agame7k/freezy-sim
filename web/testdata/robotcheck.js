// Draws every robot the simulator can build (the specs come from world.ModelSpecs via robots_test.go) and the stock
// designs, and checks each model the way an inspector would: built without errors, inside its frame and bumpers and
// under its height (and 30 in) when stowed, during a climb and with a turret turned; a deployed intake reaches no more
// than 12 in past the frame; nothing below the carpet. The mechanisms are animated and the team renumbered too, so a
// part those calls expect is never missing. Results go to window.robotCheck for the Go test to read.
import * as THREE from "three";
import { createRobot, ARCHETYPES, MAX_HEIGHT } from "/js/scene/robots.js";

const BUMPER = 0.085; // bumper depth past the frame (robots.js BUMPER_T)
const EXTENSION = 0.3048; // R105: 12 in past the frame perimeter

function bounds(root) {
  root.updateMatrixWorld(true);
  const box = new THREE.Box3();
  const part = new THREE.Box3();
  let nan = false;
  root.traverse((object) => {
    if (!object.isMesh || object.isInstancedMesh) return; // (the hopper FUEL is the simulator's)
    for (let p = object; p; p = p.parent) if (!p.visible) return;
    if (object.material.opacity === 0) return;
    object.geometry.computeBoundingBox();
    part.copy(object.geometry.boundingBox).applyMatrix4(object.matrixWorld);
    if ([part.min.x, part.min.y, part.min.z, part.max.x, part.max.y, part.max.z].some(Number.isNaN)) nan = true;
    else box.union(part);
  });
  return { box, nan };
}

const inches = (m) => (m * 39.37).toFixed(1);

function checkRobot(name, spec, team, problems) {
  const robot = createRobot({ team, alliance: team % 2 ? "red" : "blue", spec });
  const f = spec.frame;
  const top = Math.min(spec.height, MAX_HEIGHT);
  const outline = f / 2 + BUMPER + 0.004;
  const states = [
    ["stowed", () => {}],
    ["intake deployed", () => robot.setIntake(1)],
    ["climbing", () => { robot.setIntake(0); robot.setClimb(1); }],
  ];
  if (robot.parts.turret) {
    for (const angle of [0.8, 1.6, 2.4, 3.1]) {
      states.push([`turret at ${angle} rad`, () => { robot.setClimb(0); robot.parts.turret.rotation.y = angle; }]);
    }
  }
  for (const [state, apply] of states) {
    apply();
    const { box, nan } = bounds(robot.root);
    const at = `${name} (${state})`;
    if (nan) problems.push(`${at}: geometry has NaN positions`);
    if (box.max.y > top + 0.012) problems.push(`${at}: ${inches(box.max.y)} in tall, built to ${inches(top)} in`);
    if (box.max.y > MAX_HEIGHT + 0.005) problems.push(`${at}: taller than 30 in`);
    if (box.min.y < -0.002) problems.push(`${at}: ${inches(-box.min.y)} in below the carpet`);
    const deployed = state === "intake deployed";
    const front = deployed ? f / 2 + EXTENSION : outline;
    const side = deployed ? Math.max(outline, (f * spec.intake) / 2 + 0.03) : outline;
    if (box.max.x > front) problems.push(`${at}: front reaches ${inches(box.max.x - f / 2)} in past the frame`);
    if (-box.min.x > outline) problems.push(`${at}: back reaches ${inches(-box.min.x - f / 2)} in past the frame`);
    if (Math.max(box.max.z, -box.min.z) > side) {
      problems.push(`${at}: sides reach ${inches(Math.max(box.max.z, -box.min.z) - f / 2)} in past the frame`);
    }
  }
  robot.setClimb(0);
  robot.setIntake(0.5);
  for (let i = 0; i < 5; i++) {
    robot.animate(0.02, { forward: 1, left: 0.5 }, 1, { forward: 3, left: 30 }, { shooting: true, intake: true },
      robot.parts.turret ? 0.5 : null, i > 2, 1000 + i * 20);
  }
  robot.setTeam(team + 1);
  robot.setHopperFuel(spec.capacity || 10);
  robot.root.traverse((object) => {
    if (object.isMesh) object.geometry.dispose();
  });
}

async function run() {
  const specs = await (await fetch("/specs.json")).json();
  const problems = [];
  let built = 0;
  const all = [...Object.entries(ARCHETYPES).map(([name, spec]) => [`stock ${name}`, spec]),
    ...specs.map((spec) => [`${spec.drive} ${spec.frame} m frame, ${spec.height} m tall, intake ${spec.intake}, ` +
      `${spec.shooter} x${spec.lanes}, hopper ${spec.hopper}${spec.bin ? " bin" : ""}${spec.climber ? ", climber" : ""}`, spec])];
  for (const [name, spec] of all) {
    try {
      checkRobot(name, spec, 100 + built * 37, problems);
    } catch (err) {
      problems.push(`${name}: ${err.stack || err}`);
    }
    built++;
    if (built % 50 === 0) await new Promise((resolve) => setTimeout(resolve)); // let the test's polls through
  }
  return { built, specs: specs.length, problems };
}

run().then((result) => { window.robotCheck = result; },
  (err) => { window.robotCheck = { error: String(err.stack || err) }; });
