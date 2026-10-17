// Mock FRC Driver Station screen for one station, driven entirely by the simulated DS state. Laid out like the real
// Driver Station's Operation tab: mode list, Elapsed Time, PC Battery and PC CPU, Enable/Disable and the Team Station on
// the left; team number, battery voltage and its graph, the Communications / Robot Code / Joysticks lights and the
// big status line in the middle; the Messages and Diagnostics tabs on the right.
import { connect, on } from "./core/store.js";
import { h, replace } from "./core/dom.js";

const station = new URLSearchParams(location.search).get("station") || "R1";
const root = document.getElementById("ds");
root.classList.add(station[0] === "R" ? "red" : "blue");
document.title = `Driver Station — ${station}`;
const NAMES = { R1: "Red 1", R2: "Red 2", R3: "Red 3", B1: "Blue 1", B2: "Blue 2", B3: "Blue 3" };

const bar = () => {
  const fill = h("div.ds-bar-fill");
  return { el: h("div.ds-bar", fill), set(fraction, warn) {
    fill.style.width = `${Math.round(Math.max(0, Math.min(1, fraction)) * 100)}%`;
    fill.style.background = warn ? "#d9534f" : "";
  } };
};

const el = {
  modes: ["TeleOperated", "Autonomous", "Practice", "Test"].map((m) => h("div.ds-mode", m)),
  enable: h("div.enable", "Enable"),
  disable: h("div.disable", "Disable"),
  fmsNote: h("div.ds-fms-note"),
  elapsed: h("b.ds-elapsed", "0:00.0"),
  pcBattery: bar(),
  pcCpu: bar(),
  teamStation: h("b", NAMES[station]),
  team: h("b", "—"),
  batt: h("b", "--.--"),
  canvas: h("canvas", { width: 300, height: 88 }),
  comms: h("i"),
  code: h("i"),
  joy: h("i.ok"),
  status: h("div.big", "No Robot Communication"),
  stationText: h("span.ds-station"),
  fms: h("span.ds-pill"),
  match: h("span.ds-pill"),
  gameData: h("span.ds-gamedata", "—"),
  time: h("b", "0"),
  trip: h("b", "—"),
  lost: h("b", "0"),
  auto: h("b.ds-auto", "—"),
  msgs: h("div.body"),
  diag: h("div.body.ds-diag"),
  tabMsgs: h("span.active", "Messages"),
  tabDiag: h("span", "Diagnostics"),
};

let tab = "msgs";
el.tabMsgs.onclick = () => { tab = "msgs"; showTab(); };
el.tabDiag.onclick = () => { tab = "diag"; showTab(); };
function showTab() {
  el.tabMsgs.classList.toggle("active", tab === "msgs");
  el.tabDiag.classList.toggle("active", tab === "diag");
  el.msgs.style.display = tab === "msgs" ? "" : "none";
  el.diag.style.display = tab === "diag" ? "" : "none";
}
showTab();

replace(root,
  h("div.ds-rail", h("span.active", "⚙"), h("span", "≡"), h("span", "⚒"), h("span", "⌁"), h("span", "⚡")),
  h("div.ds-modes", el.modes,
    h("div.ds-kv", h("span", "Elapsed Time"), el.elapsed),
    h("div.ds-kv", h("span", "PC Battery"), el.pcBattery.el),
    h("div.ds-kv", h("span", "PC CPU %"), el.pcCpu.el),
    h("div.ds-en", el.enable, el.disable), el.fmsNote,
    h("div.ds-kv", h("span", "Team Station"), el.teamStation)),
  h("div.ds-center",
    h("div",
      h("div.ds-team", "Team #", el.team),
      h("div.ds-batt", h("div.ds-team", "Battery"), el.batt, el.canvas),
    ),
    h("div.ds-status",
      h("div.ds-row", { style: { marginTop: 0 } }, el.stationText, el.fms, el.match),
      h("div.ds-ind",
        h("div", el.comms, "Communications"),
        h("div", el.code, "Robot Code"),
        h("div", el.joy, "Joysticks"),
      ),
      el.status,
      h("div.ds-row",
        h("span", "Game Data ", el.gameData),
        h("span", "Match time ", el.time),
        h("span", "Trip ", el.trip, " ms"),
        h("span", "Lost ", el.lost),
      ),
      h("div.ds-row", h("span", "Auto chooser ", el.auto)),
    ),
  ),
  h("div.ds-msgs", h("div.hdr.ds-tabs", el.tabMsgs, el.tabDiag), el.msgs, el.diag),
);

const history = [];
const ctx = el.canvas.getContext("2d");
function drawBattery() {
  const { width, height } = el.canvas;
  ctx.fillStyle = "#161616";
  ctx.fillRect(0, 0, width, height);
  ctx.strokeStyle = "#333";
  for (const v of [7, 10, 13]) {
    const y = height - ((v - 5) / 9) * height;
    ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(width, y); ctx.stroke();
  }
  ctx.strokeStyle = "#f5b301";
  ctx.lineWidth = 2;
  ctx.beginPath();
  history.forEach((v, i) => {
    const x = (i / (history.length - 1 || 1)) * width;
    const y = height - ((Math.max(5, Math.min(14, v)) - 5) / 9) * height;
    i ? ctx.lineTo(x, y) : ctx.moveTo(x, y);
  });
  ctx.stroke();
}

const elapsedText = (seconds) => `${Math.floor(seconds / 60)}:${(seconds % 60).toFixed(1).padStart(4, "0")}`;
let lastMessages = -1;
let minVolts = Infinity;
on("ds.stations", (stations) => {
  const s = (stations || []).find((x) => x.station === station);
  if (!s) return;
  el.team.textContent = s.team || "—";
  el.stationText.textContent = NAMES[station];
  el.fms.className = "ds-pill " + (s.fmsLinked ? "ok" : s.tcpState === "rejected" ? "bad" : "warn");
  el.fms.textContent = s.fmsLinked ? "FMS Connected" : s.tcpState === "rejected" ? "FMS: not in match" : "No FMS";
  el.match.textContent = s.fmsLinked ? s.matchLabel || "" : "";
  el.match.style.display = s.fmsLinked && s.matchLabel ? "" : "none";
  const auto = s.auto;
  el.modes.forEach((m, i) => m.classList.toggle("sel", (i === 0 && !auto) || (i === 1 && auto)));
  el.enable.classList.toggle("lit", s.enabled);
  el.disable.classList.toggle("lit", !s.enabled);
  el.fmsNote.textContent = s.fmsLinked ? "Controlled by FMS" : "";
  el.elapsed.textContent = elapsedText(s.elapsedSeconds || 0);
  el.pcBattery.set(s.pcBattery || 0, (s.pcBattery || 0) < 0.2);
  el.pcCpu.set((s.pcCpu || 0) / 100, (s.pcCpu || 0) > 85);
  const volts = s.robotLinked ? s.robot.voltage : 0;
  el.batt.textContent = s.robotLinked ? volts.toFixed(2) : "--.--";
  el.batt.style.color = !s.robotLinked ? "#777" : volts < 7 ? "#ff6b6b" : volts < 11.5 ? "#ffd27a" : "#fff";
  history.push(volts);
  if (history.length > 150) history.shift();
  if (s.robotLinked && volts > 1) minVolts = Math.min(minVolts, volts);
  drawBattery();
  el.comms.className = s.robotLinked ? "ok" : "";
  el.code.className = s.codeRunning ? "ok" : "";
  el.status.textContent = s.modeText;
  el.status.className = "big " + (s.enabled ? "good" : /Stop|No Robot|Brownout/.test(s.modeText) ? "bad" : "");
  el.gameData.textContent = s.gameData || "—";
  el.time.textContent = s.fmsLinked ? String(s.control.remainingSeconds) : "—";
  el.trip.textContent = s.robotLinked ? s.tripTimeMs : "—";
  el.lost.textContent = s.lostPackets;
  if (s.messages.length !== lastMessages) {
    lastMessages = s.messages.length;
    replace(el.msgs, s.messages.slice().reverse().map((m) =>
      h("div", { class: m.kind }, h("span.t", new Date(m.time).toLocaleTimeString([], { hour12: false })), m.text)));
  }
  const row = (label, value, bad) => h("div.ds-diag-row", h("span", label), h("b", { style: { color: bad ? "#ff8080" : "" } }, value));
  replace(el.diag,
    row("Radio", s.radioLinked ? "linked" : s.radioBootLeft > 0 ? `booting (${Math.ceil(s.radioBootLeft)} s)` : "no link", !s.radioLinked),
    row("roboRIO", s.rioLinked ? "linked" : s.rioBootLeft > 0 ? `booting (${Math.ceil(s.rioBootLeft)} s)` : "no link", !s.rioLinked),
    row("Robot code", s.codeRunning ? "running" : "not running", !s.codeRunning),
    row("Trip time", s.robotLinked ? `${s.tripTimeMs} ms` : "—", s.tripTimeMs > 20),
    row("Lost packets", String(s.lostPackets), s.lostPackets > 50),
    row("CAN utilization", `${Math.round(s.canUtil || 0)} %`, (s.canUtil || 0) > 90),
    row("Brownouts", String(s.brownouts || 0), (s.brownouts || 0) > 0),
    row("Battery (now / low)", s.robotLinked ? `${volts.toFixed(2)} / ${Number.isFinite(minVolts) ? minVolts.toFixed(2) : "—"} V` : "—",
      Number.isFinite(minVolts) && minVolts < 7.5),
    row("Load", `${(s.robot.loadAmps || 0).toFixed(0)} A`, (s.robot.loadAmps || 0) > 200),
    row("FMS packets", `${(s.controlRateHz || 0).toFixed(1)} /s`, s.fmsLinked && (s.controlRateHz || 0) < 1.5));
});

// The dashboard's auto chooser: what the drive team selected for this match.
on("world.state", (world) => {
  const robot = ((world && world.robots) || []).find((r) => r.station === station);
  el.auto.textContent = robot && robot.autoName ? robot.autoName : "—";
  el.auto.style.color = robot && robot.autoName === "Do Nothing" ? "#ff6b6b" : "";
});

connect();
