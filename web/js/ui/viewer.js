// Viewer mode: the match as a spectator sees it. The tool panels, logs and debug overlays go away; what is left is the
// 3D venue on the auto-broadcast camera with the arena's own audience overlay keyed over it (the real audience display
// with a transparent background, so it is exactly what the arena shows and follows any change to the arena), and a
// small control strip that appears when the mouse moves. For an arena without an audience overlay, "Simple" swaps it
// for the sim's own score bug with "up next" and result cards. Enter with the Viewer button or ?view=viewer; Esc (or
// Exit) goes back.
import { h, replace, fmtClock } from "../core/dom.js";
import { icon } from "../core/icons.js";
import { on, get, onConnection, isConnected } from "../core/store.js";
import { clock } from "../core/match.js";

const CAMERAS = [
  ["director", "Auto"],
  ["audience", "Wide"],
  ["overhead", "Overhead"],
  ["blueEnd", "Blue end"],
  ["redEnd", "Red end"],
];

const OVERLAY_STORAGE = "freezy-sim.viewerOverlay";

let active = false;
let previousCamera = null;
let chosenCamera = "director";
let overlay = "arena"; // "arena" (the arena's audience overlay) or "simple" (the sim's score bug)
try { if (localStorage.getItem(OVERLAY_STORAGE) === "simple") overlay = "simple"; } catch { /* ignore */ }
let root = null;
let refreshTimer = 0;

export function enterViewer() {
  if (active) return;
  active = true;
  ensureMounted();
  document.body.classList.add("viewer");
  const venue = window.venue;
  if (venue) {
    venue.viewerMode = true;
    venue.viewerOverlay = overlay;
    previousCamera = venue.cameras?.current?.() || null;
    venue.cameras?.go(chosenCamera);
  }
  setUrl(true);
  applyOverlay();
  refreshTimer = setInterval(renderClock, 250);
  pokeControls();
  window.dispatchEvent(new Event("resize")); // the 3D view now fills the window
}

function pickOverlay(mode) {
  overlay = mode;
  try { localStorage.setItem(OVERLAY_STORAGE, mode); } catch { /* ignore */ }
  applyOverlay();
}

function applyOverlay() {
  document.body.classList.toggle("viewer-simple", overlay === "simple");
  const venue = window.venue;
  if (venue) {
    venue.viewerOverlay = overlay;
    venue.cameras?.refreshOverlay?.();
  }
  for (const [mode, button] of el.overlayButtons || []) button.classList.toggle("active", mode === overlay);
  render();
}

export function exitViewer() {
  if (!active) return;
  active = false;
  document.body.classList.remove("viewer", "viewer-idle", "viewer-simple");
  clearInterval(refreshTimer);
  const venue = window.venue;
  if (venue) {
    venue.viewerMode = false;
    if (previousCamera && previousCamera !== "follow") venue.cameras?.go(previousCamera);
  }
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
  setUrl(false);
  window.dispatchEvent(new Event("resize"));
}

/** Called once the 3D venue exists, so a viewer link opened before it loaded still gets the broadcast camera. */
export function viewerVenueReady(venue) {
  if (!active) return;
  venue.viewerMode = true;
  venue.viewerOverlay = overlay;
  venue.cameras?.go(chosenCamera);
}

function setUrl(on) {
  try {
    const url = new URL(location.href);
    if (on) url.searchParams.set("view", "viewer");
    else url.searchParams.delete("view");
    history.replaceState(null, "", url);
  } catch { /* ignore */ }
}

// ---- Layout ---------------------------------------------------------------------------------------------------------

const el = {};

function ensureMounted() {
  if (root) return;
  const team = () => h("span.vb-team");
  el.redTeams = h("div.vb-teams.red", team(), team(), team());
  el.blueTeams = h("div.vb-teams.blue", team(), team(), team());
  el.redScore = h("div.vb-score.red", "0");
  el.blueScore = h("div.vb-score.blue", "0");
  el.period = h("div.vb-period", "Pre-Match");
  el.time = h("div.vb-time", "0:20");
  el.shift = h("div.vb-shift");
  el.match = h("div.vb-match");
  el.status = h("div.vb-status", { hidden: true });
  const bug = h("div.vb-bug",
    el.redTeams, el.redScore,
    h("div.vb-center", el.period, el.time, el.shift),
    el.blueScore, el.blueTeams);
  el.card = h("div.vb-card", { hidden: true });

  el.cameraButtons = new Map();
  el.overlayButtons = new Map();
  const strip = h("div.vb-controls", { role: "toolbar", "aria-label": "Viewer controls" },
    h("span.vb-controls-label", icon("video", 13), "Camera"),
    CAMERAS.map(([name, label]) => {
      const button = h("button.vb-btn", { onclick: () => pickCamera(name) }, label);
      el.cameraButtons.set(name, button);
      return button;
    }),
    h("span.vb-sep"),
    h("span.vb-controls-label", icon("tv", 13), "Overlay"),
    [["arena", "Arena", "The arena's own audience overlay, exactly as the arena shows it"],
      ["simple", "Simple", "The simulator's score bug, for an arena without an audience overlay"]].map(([mode, label, title]) => {
      const button = h("button.vb-btn", { title, onclick: () => pickOverlay(mode) }, label);
      el.overlayButtons.set(mode, button);
      return button;
    }),
    h("span.vb-sep"),
    h("button.vb-btn", { title: "Full screen (F)", onclick: toggleFullscreen }, icon("monitor", 13), "Full screen"),
    h("button.vb-btn.exit", { title: "Back to the full simulator (Esc)", onclick: exitViewer }, icon("x", 13), "Exit viewer"));

  root = h("div#viewer", { "aria-live": "polite" },
    h("div.vb-top", el.match, bug, el.status),
    el.card,
    strip);
  document.body.appendChild(root);

  // The camera strip shows while the mouse moves and fades out after a few seconds, like a video player.
  document.addEventListener("mousemove", () => { if (active) pokeControls(); });
  document.addEventListener("keydown", (e) => {
    if (!active || e.target.closest?.("input, textarea, select")) return;
    if (e.key === "Escape" && !document.fullscreenElement) exitViewer();
    else if (e.key === "f" || e.key === "F") toggleFullscreen();
  });

  on("arena.matchLoad", render);
  on("arena.realtimeScore", render);
  on("arena.arenaStatus", render);
  on("arena.sockets", render);
  onConnection(render);
}

let hideTimer = 0;
function pokeControls() {
  root?.classList.add("controls-visible");
  document.body.classList.remove("viewer-idle");
  clearTimeout(hideTimer);
  hideTimer = setTimeout(() => {
    root?.classList.remove("controls-visible");
    if (active) document.body.classList.add("viewer-idle"); // hide the mouse pointer too, like a video player
  }, 3000);
}

function pickCamera(name) {
  chosenCamera = name;
  window.venue?.cameras?.go(name);
  renderCameraButtons();
}

function toggleFullscreen() {
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
  else document.documentElement.requestFullscreen?.().catch(() => {});
}

// ---- Content --------------------------------------------------------------------------------------------------------

function teamsOf(load, alliance) {
  const teams = load?.Teams || {};
  return [1, 2, 3].map((n) => teams[`${alliance}${n}`]).map((t) => (t && t.Id ? t : null));
}

function render() {
  if (!active || !root) return;
  const load = get("arena.matchLoad");
  const score = get("arena.realtimeScore");
  const red = score?.Red?.ScoreSummary?.Score ?? 0;
  const blue = score?.Blue?.ScoreSummary?.Score ?? 0;
  el.redScore.textContent = red;
  el.blueScore.textContent = blue;
  for (const [alliance, box] of [["R", el.redTeams], ["B", el.blueTeams]]) {
    teamsOf(load, alliance).forEach((team, i) => {
      const cell = box.children[i];
      cell.textContent = team ? team.Id : "—";
      cell.title = team?.Nickname || "";
    });
  }
  el.match.textContent = load?.Match?.LongName || "";

  // When the feed is down, say so plainly instead of showing a frozen score.
  const arenaUp = (get("arena.sockets") || []).some((s) => s.name === "matchPlay" && s.connected);
  const status = !isConnected() ? "Reconnecting to the simulator…" : !arenaUp ? "Waiting for the match feed…" : "";
  el.status.hidden = !status;
  el.status.textContent = status;

  renderClock();
  renderCard(load, red, blue);
  renderCameraButtons();
}

function renderClock() {
  if (!active || !root) return;
  const c = clock();
  el.period.textContent = { "Pre-Match": "Up next", "Post-Match": "Final", Pause: "Auto complete" }[c.period] || c.period;
  el.time.textContent = c.state === 5 ? "0:00" : fmtClock(c.remaining);
  el.shift.textContent = c.state === 4 && c.shift ? `${c.shift} · ${fmtClock(c.shiftRemaining)}` : "";
  root.dataset.state = String(c.state);
}

function renderCard(load, red, blue) {
  const c = clock();
  const name = load?.Match?.LongName;
  if (c.state === 5) {
    const winner = red === blue ? null : red > blue ? "red" : "blue";
    el.card.hidden = false;
    el.card.className = "vb-card " + (winner || "tie");
    replace(el.card,
      h("div.vb-card-kicker", name ? `${name} · Final` : "Final"),
      h("div.vb-card-title", winner ? `${winner === "red" ? "Red" : "Blue"} alliance wins` : "It's a tie"),
      h("div.vb-card-score", h("span.red", red), h("span.dash", "–"), h("span.blue", blue)),
      h("div.vb-card-note", "Scores are final once the referees post them."));
  } else if (c.state === 0 && name && teamsOf(load, "R").some(Boolean)) {
    el.card.hidden = false;
    el.card.className = "vb-card upnext";
    const line = (alliance) => teamsOf(load, alliance).filter(Boolean).map((t) =>
      h("span.vb-card-team", h("b", t.Id), t.Nickname ? h("small", t.Nickname) : null));
    replace(el.card,
      h("div.vb-card-kicker", "Up next"),
      h("div.vb-card-title", name),
      h("div.vb-card-alliances",
        h("div.vb-card-alliance.red", h("div.vb-card-label", "Red alliance"), line("R")),
        h("div.vb-card-alliance.blue", h("div.vb-card-label", "Blue alliance"), line("B"))));
  } else {
    el.card.hidden = true;
  }
}

function renderCameraButtons() {
  for (const [name, button] of el.cameraButtons || []) button.classList.toggle("active", name === chosenCamera);
}
