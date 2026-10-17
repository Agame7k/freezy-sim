// Catalog of every Freezy Arena display and control panel a real event uses, and helpers to open them.
import { get } from "./store.js";
import { isLocalPage, openWindow } from "./windows.js";

// kind "display" pages register with the arena (displayId); "panel" pages are operator tools.
export const DISPLAYS = [
  { id: "audience", title: "Audience Display", path: "/displays/audience", params: { background: "#000000" }, kind: "display", where: "Venue big screen" },
  { id: "audienceKey", title: "Audience Overlay (chroma)", path: "/displays/audience", params: { background: "#00ff00" }, kind: "display", where: "Broadcast keyer" },
  { id: "as-R1", title: "Alliance Station R1", path: "/displays/alliance_station", params: { station: "R1" }, kind: "display", where: "Red 1 station" },
  { id: "as-R2", title: "Alliance Station R2", path: "/displays/alliance_station", params: { station: "R2" }, kind: "display", where: "Red 2 station" },
  { id: "as-R3", title: "Alliance Station R3", path: "/displays/alliance_station", params: { station: "R3" }, kind: "display", where: "Red 3 station" },
  { id: "as-B1", title: "Alliance Station B1", path: "/displays/alliance_station", params: { station: "B1" }, kind: "display", where: "Blue 1 station" },
  { id: "as-B2", title: "Alliance Station B2", path: "/displays/alliance_station", params: { station: "B2" }, kind: "display", where: "Blue 2 station" },
  { id: "as-B3", title: "Alliance Station B3", path: "/displays/alliance_station", params: { station: "B3" }, kind: "display", where: "Blue 3 station" },
  { id: "fieldMonitor", title: "Field Monitor", path: "/displays/field_monitor", params: { fta: "false" }, kind: "display", where: "Field-facing monitor" },
  { id: "ftaMonitor", title: "FTA Field Monitor", path: "/displays/field_monitor", params: { fta: "true" }, kind: "display", where: "FTA laptop" },
  { id: "dsMonitor", title: "Driver-Facing Field Monitor", path: "/displays/field_monitor", params: { ds: "true" }, kind: "display", where: "Driver-facing" },
  { id: "dsMonitorBlue", title: "Blue Driver-Facing Monitor", path: "/displays/field_monitor", params: { ds: "true" }, kind: "display", where: "Blue end, scoring-table side" },
  { id: "dsMonitorRed", title: "Red Driver-Facing Monitor", path: "/displays/field_monitor", params: { ds: "true" }, kind: "display", where: "Red end, scoring-table side" },
  { id: "audience2", title: "Audience Display (teams side)", path: "/displays/audience", params: { background: "#000000" }, kind: "display", where: "Behind the stands" },
  { id: "fmsFieldMonitor", title: "FMS-Style Field Monitor", path: "/displays/fms_field_monitor", params: { fta: "true" }, kind: "display", where: "Scoring table" },
  { id: "announcer", title: "Announcer Display", path: "/displays/announcer", params: {}, kind: "display", where: "Announcer desk" },
  { id: "queueing", title: "Queueing Display", path: "/displays/queueing", params: {}, kind: "display", where: "Queue line" },
  { id: "rankings", title: "Rankings Display", path: "/displays/rankings", params: {}, kind: "display", where: "Pits / lobby" },
  { id: "wall", title: "Wall Display", path: "/displays/wall", params: {}, kind: "display", where: "Pits / lobby" },
  { id: "bracket", title: "Bracket Display", path: "/displays/bracket", params: {}, kind: "display", where: "Playoffs" },
  { id: "logo", title: "Logo Display", path: "/displays/logo", params: {}, kind: "display", where: "Idle screens" },
  { id: "unpicked", title: "Unpicked Teams", path: "/displays/unpicked", params: {}, kind: "display", where: "Alliance selection" },
  { id: "twitch", title: "Twitch Stream", path: "/displays/twitch", params: {}, kind: "display", where: "Pits" },
  { id: "placeholder", title: "Placeholder Display", path: "/display", params: {}, kind: "display", where: "Unassigned screen" },
  { id: "matchPlay", title: "Match Play (scorekeeper)", path: "/match_play", params: {}, kind: "panel", where: "Scorekeeper laptop" },
  { id: "referee", title: "Head Referee Panel", path: "/panels/referee", params: {}, kind: "panel", where: "Head ref touchscreen" },
  { id: "scoringRed", title: "Red Scoring Panel", path: "/panels/scoring/red", params: {}, kind: "panel", where: "Red ref tablet" },
  { id: "scoringBlue", title: "Blue Scoring Panel", path: "/panels/scoring/blue", params: {}, kind: "panel", where: "Blue ref tablet" },
  { id: "estopRed", title: "Red E-stop Control", path: "/panel/freezy/eStopControl/red", params: {}, kind: "panel", where: "Red alliance (Freezy)" },
  { id: "estopBlue", title: "Blue E-stop Control", path: "/panel/freezy/eStopControl/blue", params: {}, kind: "panel", where: "Blue alliance (Freezy)" },
  { id: "matchReview", title: "Match Review", path: "/match_review", params: {}, kind: "panel", where: "Scorekeeper" },
  { id: "allianceSelection", title: "Alliance Selection", path: "/alliance_selection", params: {}, kind: "panel", where: "Scorekeeper" },
  { id: "fieldTesting", title: "Field Testing", path: "/setup/field_testing", params: {}, kind: "panel", where: "FTA" },
  { id: "setupDisplays", title: "Display Setup", path: "/setup/displays", params: {}, kind: "panel", where: "A/V" },
  { id: "lowerThirds", title: "Lower Thirds", path: "/setup/lower_thirds", params: {}, kind: "panel", where: "A/V" },
  { id: "matchLogs", title: "Match Logs", path: "/match_logs", params: {}, kind: "panel", where: "CSA / FTA" },
  { id: "settings", title: "Arena Settings", path: "/setup/settings", params: {}, kind: "panel", where: "Admin" },
];

// The arena's address as this browser can reach it. The simulator knows it as a loopback address; a device
// connected remotely (Tailscale or LAN) reaches the arena at the same host it reached the simulator through (the
// arena listens on every interface).
export function arenaBase() {
  const configured = get("sim.info")?.arenaUrl || "http://127.0.0.1:8080";
  if (isLocalPage()) return configured;
  try {
    const url = new URL(configured);
    if (["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)) {
      url.hostname = location.hostname;
      return url.origin;
    }
  } catch {
    // keep the configured address
  }
  return configured;
}

export function displayById(id) {
  return DISPLAYS.find((d) => d.id === id);
}

export function displayUrl(display, extra = {}) {
  const params = new URLSearchParams();
  if (display.kind === "display") {
    params.set("displayId", `sim-${display.id}`);
    params.set("nickname", `Sim ${display.title}`);
  }
  for (const [key, value] of Object.entries({ ...display.params, ...extra })) params.set(key, value);
  const query = params.toString();
  return arenaBase() + display.path + (query ? `?${query}` : "");
}

export async function openDisplay(id, extra = {}) {
  const display = displayById(id);
  if (!display) return;
  await openWindow(displayUrl(display, extra), display.title, 1280, 760);
}

// Which display each 3D venue screen shows.
export const SCREEN_DISPLAYS = {
  audience: "audience",
  audience2: "audience2",
  bracket: "bracket",
  dsMonitorBlue: "dsMonitorBlue",
  dsMonitorRed: "dsMonitorRed",
  "as-R1": "as-R1",
  "as-R2": "as-R2",
  "as-R3": "as-R3",
  "as-B1": "as-B1",
  "as-B2": "as-B2",
  "as-B3": "as-B3",
  matchPlay: "matchPlay",
  fieldMonitor: "ftaMonitor",
  fmsFieldMonitor: "fmsFieldMonitor",
  referee: "referee",
  announcer: "announcer",
  scoringRed: "scoringRed",
  queueing: "queueing",
  rankings: "rankings",
};
