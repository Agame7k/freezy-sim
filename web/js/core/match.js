// Derived match clock: turns the arena's matchTime/matchTiming topics into period, countdown and hub shift.
import { clockRate, get, on } from "./store.js";

const DEFAULT_TIMING = {
  AutoDurationSec: 20, PauseDurationSec: 3, TransitionShiftDurationSec: 10, ShiftDurationSec: 25,
  EndgameDurationSec: 30, TimeoutDurationSec: 0,
};

let lastTime = { MatchState: 0, MatchTimeSec: 0 };
let lastTimeAt = performance.now();

on("arena.matchTime", (data) => {
  lastTime = data || lastTime;
  lastTimeAt = performance.now();
});

export function timing() {
  return { ...DEFAULT_TIMING, ...(get("arena.matchTiming") || {}) };
}

export function teleopDuration(t = timing()) {
  return t.TransitionShiftDurationSec + 4 * t.ShiftDurationSec + t.EndgameDurationSec;
}

/** Returns a snapshot of the match clock interpolated to now. */
export function clock() {
  const t = timing();
  const state = lastTime.MatchState;
  let elapsed = lastTime.MatchTimeSec;
  if (state === 2 || state === 3 || state === 4 || state === 6) {
    const rate = clockRate();
    elapsed += Math.min(1.2 * rate, (performance.now() - lastTimeAt) / 1000 * rate);
  }
  const autoEnd = t.AutoDurationSec;
  const teleopStart = autoEnd + t.PauseDurationSec;
  const teleopEnd = teleopStart + teleopDuration(t);

  const result = { state, elapsed, period: "Pre-Match", remaining: t.AutoDurationSec, shift: "", shiftRemaining: 0 };
  switch (state) {
    case 0:
      result.period = "Pre-Match";
      result.remaining = t.AutoDurationSec;
      break;
    case 1:
    case 2:
      result.period = "Auto";
      result.remaining = autoEnd - elapsed;
      result.shift = "Auto";
      result.shiftRemaining = result.remaining;
      break;
    case 3:
      result.period = "Pause";
      result.remaining = teleopDuration(t);
      break;
    case 4: {
      result.period = "Teleop";
      result.remaining = teleopEnd - elapsed;
      const intoTeleop = elapsed - teleopStart;
      if (intoTeleop < t.TransitionShiftDurationSec) {
        result.shift = "Transition";
        result.shiftRemaining = t.TransitionShiftDurationSec - intoTeleop;
      } else if (intoTeleop < t.TransitionShiftDurationSec + 4 * t.ShiftDurationSec) {
        const index = Math.floor((intoTeleop - t.TransitionShiftDurationSec) / t.ShiftDurationSec);
        result.shift = `Shift ${index + 1}`;
        result.shiftRemaining = t.ShiftDurationSec - ((intoTeleop - t.TransitionShiftDurationSec) % t.ShiftDurationSec);
      } else {
        result.shift = "End Game";
        result.shiftRemaining = teleopEnd - elapsed;
      }
      break;
    }
    case 5:
      result.period = "Post-Match";
      result.remaining = 0;
      break;
    case 6:
      result.period = "Timeout";
      result.remaining = t.TimeoutDurationSec - elapsed;
      break;
    case 7:
      result.period = "Post-Timeout";
      result.remaining = 0;
      break;
  }
  return result;
}
