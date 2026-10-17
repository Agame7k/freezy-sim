// Auto broadcast: an AI camera director. It scores what is happening on the field twice a second (volleys and
// shooting runs, defenders shoving shooters, PIN counts, foul calls, end-game climbs, human-player throws, the start
// and end of the match) and works like a broadcast crew: it holds a shot for a few seconds, then cuts to another
// camera position on the best play, panning and slowly pushing in on the action within a shot, with a lower-third
// caption saying what the viewer is watching.
import * as THREE from "three";
import { on, get } from "../core/store.js";
import { clock } from "../core/match.js";
import { h } from "../core/dom.js";
import { ctx, f2s } from "./util.js";

const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];
const MIN_SHOT = 3.2; // seconds before the director may cut away
const MAX_SHOT = 9;
const CUT_MARGIN = 1.35; // a new play must be this much more interesting to cut away early

export function setupDirector(venue, layout) {
  const { camera, controls } = venue;
  const { L, W } = ctx;
  const tableHigh = (layout.venue?.scoringTableSide || "high") === "high";
  const audienceSide = tableHigh ? -1 : 1; // the main camera platform faces the scoring table from the audience
  let active = false;
  let shot = null;
  let shotStarted = 0;
  let lastScore = 0;
  let ai = {};
  let officials = {};
  let lastThink = 0;
  let wobble = 0;
  const safety = venue.cameraSafety;

  on("world.ai", (data) => {
    ai = {};
    for (const robot of data?.robots || []) ai[robot.station] = robot;
  });
  on("world.state", (state) => { officials = state?.officials || {}; });

  // Lower third and the LIVE bug.
  const lower = h("div.director-lower", { style: { display: "none" } }, h("span.director-tag", "LIVE"), h("span.director-text"));
  venue.hud.appendChild(lower);
  const caption = lower.querySelector(".director-text");

  const robotPos = (station) => {
    const robot = venue.robots?.[station];
    if (!robot || !robot.root.visible) return null;
    return robot.root.position;
  };
  const flags = (station) => venue.robots?.[station]?.flags || {};
  const label = (station) => {
    const robot = venue.robots?.[station];
    return robot ? `${station} ${robot.team}` : station;
  };

  // Candidate plays, each with an interest score, a subject and a camera style.
  function plays() {
    const list = [];
    const c = clock();
    const state = c.state;
    if (state === 0) list.push({ score: 1, kind: "wide", text: "Teams ready on the field" });
    if (state === 1 || (state === 2 && c.elapsed < 2)) list.push({ score: 9, kind: "wide", text: "AUTO underway" });
    if (state === 5) list.push({ score: 3, kind: "wide", text: "Final whistle: scoring assessment" });
    // Head referee presenting a card.
    const cardUntil = venue.officials?.headRef?.cardUntil || 0;
    if (cardUntil > performance.now()) {
      list.push({ score: 12, kind: "official", official: venue.officials.headRef, text: "Head referee presents a card" });
    }
    // Foul calls (flags in the air) and PIN counts.
    for (const ref of venue.officials?.refs || []) {
      if ((ref.flagUntil || 0) > performance.now()) {
        list.push({ score: 7, kind: "official", official: ref, text: "Foul called" });
      }
    }
    for (const pin of officials.pins || []) {
      const p = robotPos(pin.pinner);
      if (p) list.push({ score: 6 + Math.min(4, pin.seconds), kind: "action", at: p.clone(), subject: pin.pinner,
        text: `Pin count on ${label(pin.pinned)}: ${pin.seconds.toFixed(0)} s` });
    }
    for (const station of STATIONS) {
      const p = robotPos(station);
      if (!p) continue;
      const f = flags(station);
      const thinking = ai[station] || {};
      const intent = thinking.intent || "";
      const robot = venue.robots[station];
      if (f.shooting) {
        const wide = robot.parts.spec.shooter === "wide";
        list.push({ score: (wide ? 5.5 : 3.5) + Math.min(3, (f.hopper || 0) / 15), kind: "action", at: p.clone(), subject: station,
          text: wide ? `${label(station)} unloads 4-wide volleys` : `${label(station)} ${intent || "shooting"}` });
      }
      if (intent.startsWith("shoving")) {
        list.push({ score: 6.5, kind: "action", at: p.clone(), subject: station, text: `${label(station)} ${intent}` });
      } else if (intent.startsWith("guarding")) {
        list.push({ score: 3, kind: "action", at: p.clone(), subject: station, text: `${label(station)} ${intent}` });
      }
      if (f.climbing || f.climbLevel > 0) {
        list.push({ score: 5 + (f.climbLevel || 0) * 1.5, kind: "tower", at: p.clone(), subject: station,
          text: f.climbLevel > 0 ? `${label(station)} hangs at LEVEL ${f.climbLevel}` : `${label(station)} climbing the tower` });
      }
      if (intent.includes("shooting on the move")) {
        list.push({ score: 4.5, kind: "action", at: p.clone(), subject: station, text: `${label(station)} shooting on the move` });
      }
      if (f.manual) list.push({ score: 4, kind: "action", at: p.clone(), subject: station, text: `${label(station)} (driven by you)` });
    }
    // Human player throws.
    for (const alliance of ["red", "blue"]) {
      const outpost = venue.field.outposts?.[alliance];
      if (outpost && performance.now() - (outpost.human?.throwAt || -1e9) < 900) {
        list.push({ score: 3.5, kind: "human", outpost, text: `${alliance === "red" ? "Red" : "Blue"} human player throws for the hub` });
      }
    }
    if (c.state === 4 && c.remaining < 20) {
      for (const play of list) if (play.kind === "tower") play.score += 3;
    }
    list.push({ score: 1.5, kind: "wide", text: c.shift ? `${c.period} — ${c.shift}` : c.period });
    return list.sort((a, b) => b.score - a.score);
  }

  // Camera positions for a play (the cut chooses a fresh angle for each shot).
  const V = (x, y, z) => f2s(x, y, z);
  function frame(play) {
    const angle = Math.random();
    switch (play.kind) {
      case "wide":
        return angle < 0.5
          ? { pos: V(L / 2, audienceSide < 0 ? -3.2 : W + 3.2, 6.2), target: V(L / 2, W / 2, 0.3), fov: 55 }
          : { pos: V(L / 2 + (Math.random() - 0.5) * 6, audienceSide < 0 ? -1.4 : W + 1.4, 3.6), target: V(L / 2, W / 2, 0.4), fov: 62 };
      case "official": {
        const o = play.official.root.position;
        const side = o.z > 0 ? 1 : -1;
        return { pos: o.clone().add(new THREE.Vector3((Math.random() - 0.5) * 2, 1.7, -side * 3.2)), target: o.clone().add(new THREE.Vector3(0, 1.3, 0)), fov: 42, follow: null };
      }
      case "human": {
        const o = play.outpost.human.root.getWorldPosition(new THREE.Vector3());
        return { pos: o.clone().add(new THREE.Vector3(o.x > 0 ? -3.2 : 3.2, 2.2, 1.6)), target: o.clone().add(new THREE.Vector3(0, 1.2, 0)), fov: 48 };
      }
      case "tower":
      case "action":
      default: {
        // Sideline rail cameras at guardrail level (they slide along the rail with the play), high reverse angles,
        // end-wall cameras and a high corner: try them in a random order and take the first that can actually see the
        // subject from a sensible distance (a hub, tower or trench in the way is a wasted shot).
        const p = play.at;
        const target = p.clone().add(new THREE.Vector3(0, play.kind === "tower" ? 0.8 : 0.35, 0));
        const fx = p.x + L / 2;
        const fy = -p.z + W / 2;
        const near = audienceSide < 0 ? -1.6 : W + 1.6;
        const far = audienceSide < 0 ? W + 1.9 : -1.9;
        const options = [
          { pos: V(fx + (Math.random() - 0.5) * 2.5, near, 2.4), fov: 40, rail: "near" },
          { pos: V(fx + (fx < L / 2 ? 3.5 : -3.5), audienceSide < 0 ? -2.4 : W + 2.4, 4.4), fov: 46, rail: "near" },
          { pos: V(fx + (Math.random() - 0.5) * 2, far, 3.4), fov: 42, rail: "far" },
          { pos: V(fx < L / 2 ? -1.2 : L + 1.2, fy + (Math.random() - 0.5) * 2, 3.0), fov: 44 },
          { pos: V(fx + (fx < L / 2 ? -4 : 4), fy < W / 2 ? -2.2 : W + 2.2, 6.0), fov: 40 },
        ];
        for (let i = options.length - 1; i > 0; i--) { // (shuffle so cuts vary)
          const j = Math.floor(Math.random() * (i + 1));
          [options[i], options[j]] = [options[j], options[i]];
        }
        const usable = (option) => {
          const distance = option.pos.distanceTo(target);
          return distance > 2.2 && distance < 9.5 && (!safety || safety.sees(option.pos, target));
        };
        const choice = options.find(usable) || options.find((o) => !safety || safety.sees(o.pos, target)) || options[0];
        return { ...choice, target, follow: play.subject, offset: choice.pos.clone().sub(target) };
      }
    }
  }

  function cut(play, now) {
    const framing = frame(play);
    if (safety && framing.target) framing.pos = safety.clear(framing.target, framing.pos, 0.35).pos; // never inside a wall
    shot = { play, ...framing, push: 0 };
    shotStarted = now;
    lastScore = play.score;
    camera.position.copy(framing.pos);
    controls.target.copy(framing.target);
    camera.fov = framing.fov;
    camera.updateProjectionMatrix();
    caption.textContent = play.text;
  }

  venue.addUpdater((dt) => {
    if (!active) return;
    const now = performance.now() / 1000;
    if (now - lastThink > 0.5) {
      lastThink = now;
      const best = plays()[0];
      const held = now - shotStarted;
      const same = shot && best.kind === shot.play.kind && best.subject === shot.play.subject && best.official === shot.play.official;
      if (!shot || (held > MIN_SHOT && !same && (best.score > lastScore * CUT_MARGIN || held > MAX_SHOT)) ||
        (held > MAX_SHOT * 1.6)) {
        cut(best, now);
      } else if (same) {
        lastScore = best.score;
        caption.textContent = best.text;
        if (best.at) shot.play.at = best.at;
      }
    }
    if (!shot) return;
    // Within a shot: the operator pans to keep the subject framed, a rail camera dollies along the guardrail to keep
    // up, pushes in slowly (less on a fast-moving subject), with a little hand-held drift. If the subject leaves the
    // frame or goes behind something, the director re-frames instead of holding a shot of nothing.
    const subject = shot.play.subject && robotPos(shot.play.subject);
    let speed = 0;
    if (subject) {
      const goal = subject.clone().add(new THREE.Vector3(0, shot.play.kind === "tower" ? 0.8 : 0.35, 0));
      speed = shot.last ? goal.distanceTo(shot.last) / Math.max(dt, 1e-3) : 0;
      shot.last = goal.clone();
      controls.target.lerp(goal, Math.min(1, dt * 2.6));
      if (shot.rail) {
        const along = camera.position.clone();
        along.x += (goal.x + shot.offset.x - along.x) * Math.min(1, dt * 0.9);
        camera.position.copy(along);
      }
      if (now - (shot.checkedAt || 0) > 0.4) {
        shot.checkedAt = now;
        const view = goal.clone().project(camera);
        const offFrame = Math.abs(view.x) > 0.92 || Math.abs(view.y) > 0.92 || view.z > 1;
        const hidden = safety && !safety.sees(camera.position, goal);
        shot.lostFor = offFrame || hidden || camera.position.distanceTo(goal) > 11 ? (shot.lostFor || 0) + 0.4 : 0;
        if (shot.lostFor >= 0.8) {
          const play = { ...shot.play, at: subject.clone() };
          cut(play, now); // a fresh angle on the same play
          return;
        }
      }
    }
    shot.push += dt;
    const pushRate = speed > 1.5 ? 0.15 : 0.6;
    camera.fov = Math.max(shot.fov * 0.82, camera.fov - dt * pushRate);
    wobble += dt;
    const drift = new THREE.Vector3(Math.sin(wobble * 0.7) * 0.02, Math.sin(wobble * 0.9) * 0.015, 0);
    camera.updateProjectionMatrix();
    camera.lookAt(controls.target.clone().add(drift));
  });

  const director = {
    start() {
      active = true;
      shot = null;
      venue.activeCamera = camera;
      controls.enabled = false;
      lower.style.display = "";
    },
    stop() {
      active = false;
      lower.style.display = "none";
    },
    get active() { return active; },
  };
  venue.director = director;
  if (venue.cameras?.current?.() === "director") director.start();
  return director;
}
