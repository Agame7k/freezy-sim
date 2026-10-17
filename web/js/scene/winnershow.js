// Winner light show: the field PLC's own lighting sequence. The arena pulses the redWins/blueWins coils for 10 s the
// moment the audience display switches to the final score (both for a tie) and leaves the sequence, and any delay-on,
// to the PLC. The display first runs its screen transitions and the winner video, which only reveals the winner about
// 3.25 s in, so the sim's PLC (fieldio WinnerShow) holds the big moment until that reveal (revealMs after the coil).
// Times below are from the reveal (R):
//   until R-3.25  waiting: the display is still transitioning; house lights as normal
//   R-3.25..R     suspense, while the video's ice cube spins: house dims, faint white beams search the field
//   R..R+1.8      the reveal: three strobe hits in the winner's colour
//   R+1.8..R+3.8  colour wash, moving heads sweep the field on a 120 bpm beat, the winning hub pulses
//   R+3.8..R+7    the final score fades in: every beam converges on the winning hub (centre for a tie) and holds
//   R+7..R+10     crossfade back to white house light
// A tie alternates red and blue between neighbouring fixtures, swapping on every beat.
import * as THREE from "three";
import { on } from "../core/store.js";
import { ctx, f2s } from "./util.js";

const RED = new THREE.Color(0xff2a1a);
const BLUE = new THREE.Color(0x2a62ff);
const WHITE = new THREE.Color(0xfff4e0);
const HEMI_SKY = new THREE.Color(0xdfe7ff);
const BEAT = 0.5; // seconds (120 bpm)
const SUSPENSE = 3.25; // the winner video's run-up to the reveal

const clamp01 = (v) => Math.min(1, Math.max(0, v));
const ramp = (t, a, b) => clamp01((t - a) / (b - a));

export function bindWinnerShow(venue) {
  const { env, field } = venue;
  const house = env.house;
  const movers = env.movers || [];
  if (!house) return;
  const { L, W } = ctx;

  // The show clock: anchored when a new show id arrives, so the 3D client stays in step with the PLC. The reveal
  // time can still change just after the coil rises (the display mode arrives on another link).
  let show = null; // { id, red, blue, startedAt (performance.now ms), revealMs, durationMs }
  on("field.io", (io) => {
    const s = io?.winnerShow;
    if (!s) {
      show = null;
      return;
    }
    if (!show || show.id !== s.id) show = { id: s.id, startedAt: performance.now() - s.elapsedMs };
    Object.assign(show, { red: s.red, blue: s.blue, revealMs: s.revealMs, durationMs: s.durationMs });
  });

  const color = new THREE.Color();
  const target = new THREE.Vector3();
  const headPos = new THREE.Vector3();
  const centre = f2s(L / 2, W / 2, 0);
  const hubSpots = {};
  const hubSpot = (alliance) => {
    const hub = field.hubs[alliance];
    if (!hub) return centre;
    return (hubSpots[alliance] ||= hub.group.getWorldPosition(new THREE.Vector3()));
  };
  // Smoothed light levels, so a show that is cut short (match start, PLC heartbeat lost) fades instead of snapping.
  const level = { house: 1, wash: 0, beams: 0, tint: 0 };
  let lastLead = RED;
  const allianceColor = (s, index, beat) => {
    if (s.red && s.blue) return (index + beat) % 2 === 0 ? RED : BLUE;
    return s.red ? RED : BLUE;
  };

  venue.addUpdater((dt) => {
    let want = { house: 1, wash: 0, beams: 0, tint: 0 };
    let u = -Infinity; // seconds from the reveal
    if (show) {
      const t = (performance.now() - show.startedAt) / 1000;
      if (t * 1000 >= show.durationMs) show = null;
      else u = t - show.revealMs / 1000;
    }
    const s = show;
    const revealed = s && u >= 0;
    const beat = revealed ? Math.floor(u / BEAT) : 0;
    const beatPhase = revealed ? (u % BEAT) / BEAT : 0;
    const pulse = Math.exp(-beatPhase * 4); // a kick on each beat
    const strobing = revealed && u < 1.8;
    if (s && u >= -SUSPENSE) {
      if (!revealed) {
        // Suspense: nothing gives the result away yet.
        const build = ramp(u, -SUSPENSE, 0);
        want = { house: 1 - 0.7 * ramp(u, -SUSPENSE, -SUSPENSE + 1.5), wash: 0, beams: 0.25 + 0.35 * build, tint: 0 };
      } else {
        const fadeOut = ramp(u, 7, 10);
        const strobe = (u % 0.6) < 0.22 ? 1 : 0;
        want = {
          house: Math.max(0.3, fadeOut),
          wash: strobing ? strobe : (1 - fadeOut) * (0.55 + 0.45 * pulse),
          beams: strobing ? strobe : (1 - fadeOut) * (0.6 + 0.4 * pulse),
          tint: 1 - fadeOut,
        };
      }
    }
    // Strobe hits snap; everything else eases.
    const k = strobing ? 1 : 1 - Math.exp(-dt * 6);
    for (const key of Object.keys(level)) level[key] += (want[key] - level[key]) * k;

    // House: dim the white rig, then tint it toward the winner.
    const base = house.base;
    const lead = revealed ? (lastLead = allianceColor(s, 0, beat)) : lastLead;
    house.hemi.intensity = base.hemi * level.house;
    house.hemi.color.copy(HEMI_SKY).lerp(lead, level.tint * 0.25);
    house.key.intensity = base.key * level.house;
    house.spots.forEach((spot, i) => {
      spot.intensity = base.spot * Math.max(level.house, level.tint * 0.75);
      spot.color.copy(WHITE).lerp(revealed ? allianceColor(s, i, beat) : lastLead, level.tint);
    });
    house.wash.intensity = 150 * level.wash;
    house.wash.color.copy(lead);

    // Moving heads.
    const finale = revealed ? ramp(u, 3.8, 4.8) : 0;
    const focus = revealed ? (s.red && s.blue ? centre : hubSpot(s.red ? "red" : "blue")) : centre;
    const time = performance.now() / 1000;
    movers.forEach((mover, i) => {
      const lit = level.beams > 0.01;
      mover.beam.visible = lit;
      mover.lens.material.emissiveIntensity = 3 * level.beams;
      if (!lit) return;
      if (s && !revealed) color.copy(WHITE);
      else color.copy(revealed ? allianceColor(s, i, beat) : lastLead);
      mover.beam.material.color.copy(color);
      mover.lens.material.emissive.copy(color);
      mover.beam.material.opacity = (s && !revealed ? 0.22 : 0.32) * level.beams;
      if (strobing) {
        target.copy(centre);
      } else {
        // Each head traces its own Lissajous figure across the carpet, mirrored side to side; slow while searching.
        const speed = revealed ? 1 : 0.35;
        const phase = i * 0.9;
        const fx = L / 2 + Math.sin(time * 1.3 * speed + phase) * L * 0.38;
        const fy = W / 2 + Math.sin(time * 0.9 * speed * mover.side + phase * 1.7) * W * 0.42;
        target.copy(f2s(fx, fy, 0)).lerp(focus, finale);
      }
      mover.head.getWorldPosition(headPos);
      mover.head.lookAt(target);
      mover.beam.scale.setScalar(headPos.distanceTo(target));
    });

    // The winning hub(s) pulse with the beat, over whatever the hub-light coil says.
    if (revealed && u < 7) {
      for (const alliance of ["red", "blue"]) {
        const hub = field.hubs[alliance];
        if (!hub || !(alliance === "red" ? s.red : s.blue)) continue;
        hub.glow.intensity = 6 + 10 * pulse;
        hub.glowDisc.material.opacity = 0.4 + 0.5 * pulse;
      }
    }
  });
}
