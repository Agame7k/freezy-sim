// Driver Stations panel: one card per station with link chain, robot state, battery, faults and network knobs.
import { h, replace, led, tag, toast, cmdButton } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";
import { icon } from "../core/icons.js";
import { popOutMockDs } from "../ui/mockds-link.js";

const TCP_TEXT = {
  idle: ["Idle", ""], connecting: ["Connecting", "warn"], handshake: ["Handshake", "warn"],
  connected: ["FMS connected", "good"], rejected: ["Rejected", "bad"], error: ["Error", "bad"],
  "ds-off": ["DS off", ""], unplugged: ["Unplugged", "bad"], "no-team": ["No team", ""],
};

function set(station, field, value) {
  return cmd("ds.set", { station, field, value }).catch((e) => toast(e.message, "error"));
}
function act(command, station) {
  return cmd(command, { station }).catch((e) => toast(e.message, "error"));
}

function makeCard(name) {
  const isRed = name[0] === "R";
  const el = {};
  el.team = h("input.mono", {
    type: "number", min: 0, max: 99999, style: { width: "76px" }, title: "Override team number (0 = follow arena)",
    onchange: (e) => set(name, "team", Number(e.target.value) || 0),
  });
  el.teamSource = h("span.small.muted");
  el.protocol = h("span.tag");
  el.tcp = h("span.tag");
  el.chain = h("div.row.wrap", { style: { gap: "10px", margin: "6px 0" } });
  el.mode = h("div", { style: { fontSize: "15px", fontWeight: 600 } });
  el.flags = h("div.row.wrap", { style: { gap: "6px" } });
  el.batteryText = h("span.mono");
  el.batteryBar = h("div", { style: { height: "6px", borderRadius: "3px", background: "var(--green)", width: "100%" } });
  el.batteryTrack = h("div", { style: { flex: "1", height: "6px", background: "#0007", borderRadius: "3px" } },
    el.batteryBar);
  el.load = h("span.small.muted.mono");
  el.stats = h("dl.kv", { style: { marginTop: "6px" } });
  el.loss = h("input", { type: "range", min: 0, max: 60, step: 1, style: { flex: "1" },
    oninput: (e) => { el.lossValue.textContent = `${e.target.value}%`; },
    onchange: (e) => set(name, "lossPct", Number(e.target.value)) });
  el.lossValue = h("span.mono.small", "0%");
  el.latency = h("input", { type: "range", min: 0, max: 250, step: 1, style: { flex: "1" },
    oninput: (e) => { el.latencyValue.textContent = `${e.target.value}ms`; },
    onchange: (e) => set(name, "latencyMs", Number(e.target.value)) });
  el.latencyValue = h("span.mono.small", "0ms");
  el.dsOn = h("button.btn.sm");
  el.ethernet = h("button.btn.sm");
  el.legacy = h("button.btn.sm");
  el.messages = h("div.log", { style: { height: "110px", fontSize: "11px" } });
  el.details = h("details", h("summary.small.muted", "DS messages & control packet"), el.messages,
    el.control = h("dl.kv.small", { style: { marginTop: "6px" } }));

  el.root = h("div.card", { style: { borderLeft: `3px solid var(${isRed ? "--red" : "--blue"})` } },
    h("h3", { style: { textTransform: "none", letterSpacing: 0, color: "var(--text)", fontSize: "13px" } },
      h("span.mono", { style: { color: isRed ? "#ffb1b3" : "#b7d2ff", fontWeight: 700 } }, name),
      el.team, el.teamSource, h("span.grow"), el.protocol, el.tcp,
      h("button.btn.sm.ghost.icon-only", { title: "Open this team's Driver Station screen", "aria-label": "Open Driver Station", onclick: () => popOutMockDs(name) },
        icon("monitor"))),
    el.chain,
    h("div.row", el.mode, h("span.grow"), el.flags),
    h("div.row", { style: { marginTop: "6px" } }, h("span.small.muted", "Battery"), el.batteryTrack, el.batteryText,
      el.load),
    h("div.row", { style: { marginTop: "6px" } }, h("span.small.muted", { style: { width: "52px" } }, "Loss"), el.loss,
      el.lossValue),
    h("div.row", h("span.small.muted", { style: { width: "52px" } }, "Latency"), el.latency, el.latencyValue),
    h("div.btn-row",
      el.dsOn, el.ethernet,
      h("button.btn.sm", { onclick: () => act("ds.rebootRio", name) }, "Reboot RIO"),
      h("button.btn.sm", { onclick: () => act("ds.rebootRadio", name) }, "Reboot radio"),
      h("button.btn.sm", { onclick: () => act("ds.crashCode", name) }, "Crash code"),
      h("button.btn.sm", { onclick: () => act("ds.restartCode", name) }, "Restart code"),
      h("button.btn.sm", { onclick: () => act("ds.powerCycle", name) }, "Power cycle"),
      h("button.btn.sm", { onclick: () => act("ds.freshBattery", name) }, "Fresh battery"),
      h("button.btn.sm", { onclick: () => set(name, "batteryFull", 12.15), title: "Swap in a tired battery" },
        "Weak battery"),
      el.legacy,
    ),
    el.stats,
    el.details,
  );
  el.lastMessageCount = -1;
  return el;
}

function chainItem(label, ok, title, onclick, extra = "") {
  return h("span.row", { style: { gap: "4px", cursor: onclick ? "pointer" : "default" }, title, onclick },
    led(ok ? "green" : "red"), h("span.small", label + extra));
}

function updateCard(el, s) {
  const name = s.station;
  if (document.activeElement !== el.team) el.team.value = s.settings.teamOverride || "";
  el.team.placeholder = s.team || "—";
  el.teamSource.textContent = s.settings.teamOverride ? "override" : s.team ? `${s.team} via arena` : "no team";
  el.protocol.textContent = s.protocol === "legacy" ? `legacy :${s.udpPort}` : `new DS :${s.udpPort}`;
  const [tcpText, tcpKind] = TCP_TEXT[s.tcpState] || [s.tcpState, ""];
  el.tcp.className = "tag " + tcpKind;
  el.tcp.textContent = s.assignStatus === 1 && s.tcpState === "connected" ? "Wrong station" : tcpText;
  el.tcp.title = [s.assignText, s.tcpError].filter(Boolean).join("\n");

  const r = s.robot;
  replace(el.chain,
    chainItem("FMS", s.fmsLinked, "Control packets arriving from the arena"),
    chainItem("Radio", s.radioLinked, "Click to toggle the radio", () => set(name, "radio", !r.radio),
      s.radioBootLeft > 0 ? ` (${Math.ceil(s.radioBootLeft)}s)` : ""),
    chainItem("RIO", s.rioLinked, "Click to toggle the roboRIO", () => set(name, "rio", !r.rio),
      s.rioBootLeft > 0 ? ` (${Math.ceil(s.rioBootLeft)}s)` : ""),
    chainItem("Comms", s.robotLinked, "DS ↔ robot communications"),
    chainItem("Code", s.codeRunning, "Click to toggle robot code", () => set(name, "code", !r.code)),
    chainItem("Power", r.powered, "Click to toggle robot main breaker", () => set(name, "robotPower", !r.powered)),
  );
  el.mode.textContent = s.modeText;
  el.mode.style.color = s.enabled ? "var(--green)" : s.eStopped ? "var(--red)" : "var(--text)";
  replace(el.flags,
    s.enabled ? tag("ENABLED", "good") : tag("disabled"),
    tag(s.auto ? "AUTO" : "TELEOP"),
    s.eStopped ? tag("E-STOP", "bad") : null,
    s.aStopped ? tag("A-STOP", "warn") : null,
    r.brownout ? tag("BROWNOUT", "bad") : null,
    s.gameData ? tag(`Game data: ${s.gameData}`, s.gameData === "R" ? "red" : "blue") : null,
  );
  const volts = r.voltage || 0;
  el.batteryText.textContent = `${volts.toFixed(2)} V`;
  el.batteryBar.style.width = `${Math.max(0, Math.min(100, ((volts - 6) / 7) * 100))}%`;
  el.batteryBar.style.background = volts < 7 ? "var(--red)" : volts < 11.5 ? "var(--amber)" : "var(--green)";
  el.load.textContent = `${r.loadAmps.toFixed(0)}A · ${(r.soc * 100).toFixed(0)}%`;
  if (document.activeElement !== el.loss) {
    el.loss.value = s.settings.lossPct;
    el.lossValue.textContent = `${s.settings.lossPct}%`;
  }
  if (document.activeElement !== el.latency) {
    el.latency.value = s.settings.latencyMs;
    el.latencyValue.textContent = `${s.settings.latencyMs}ms`;
  }
  el.dsOn.className = "btn sm " + (s.settings.dsOn ? "on" : "off");
  el.dsOn.textContent = s.settings.dsOn ? "DS on" : "DS off";
  el.dsOn.onclick = () => set(name, "dsOn", !s.settings.dsOn);
  el.ethernet.className = "btn sm " + (s.settings.ethernet ? "on" : "off");
  el.ethernet.textContent = s.settings.ethernet ? "Ethernet ✓" : "Unplugged";
  el.ethernet.onclick = () => set(name, "ethernet", !s.settings.ethernet);
  el.legacy.textContent = s.protocol === "legacy" ? "Use new DS" : "Use legacy DS";
  el.legacy.onclick = () => set(name, "legacy", s.protocol !== "legacy");

  replace(el.stats,
    h("dt", "Control"), h("dd", `${s.controlRateHz.toFixed(2)} Hz · last ${s.controlGapMs} ms · max gap ${s.maxControlGapMs} ms`),
    h("dt", "Status sent"), h("dd", `${s.statusSent} · lost ${s.lostPackets} · trip ${s.tripTimeMs} ms`),
    h("dt", "FMS says"), h("dd", `${s.control.station || "—"} · match ${s.control.matchType}/${s.control.matchNumber} · ${s.control.remainingSeconds}s left`),
    s.eventName ? [h("dt", "Event"), h("dd", s.eventName)] : null,
    s.statusSendErr ? [h("dt", "Error"), h("dd", { style: { color: "var(--red)" } }, s.statusSendErr)] : null,
  );

  if (el.details.open && s.messages.length !== el.lastMessageCount) {
    el.lastMessageCount = s.messages.length;
    replace(el.messages, s.messages.slice().reverse().map((m) => h("div.line", { class: m.kind === "bad" ? "lvl-error" : m.kind === "warn" ? "lvl-warn" : "" },
      h("span.t", new Date(m.time).toLocaleTimeString([], { hour12: false })), h("span.s", ""), h("span.m", m.text))));
  }
}

registerPanel({
  id: "ds",
  title: "Driver Stations",
  mount(el, { setBadge }) {
    const follow = h("input", { type: "checkbox", checked: true,
      onchange: (e) => cmd("ds.follow", { follow: e.target.checked }) });
    el.append(h("div.card",
      h("h3", "All stations"),
      h("div.btn-row",
        cmdButton("Quick test match", "match.quickTest", {}, "primary"),
        h("button.btn", { onclick: () => set("all", "dsOn", true) }, "All DS on"),
        h("button.btn", { onclick: () => set("all", "dsOn", false) }, "All DS off"),
        h("button.btn", { onclick: () => act("ds.freshBattery", "all") }, "Fresh batteries"),
        h("button.btn", { onclick: () => act("ds.resetStats", "all") }, "Reset stats"),
        h("label.check", follow, "Follow arena teams"),
      )));
    const grid = h("div", { style: { display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(380px, 1fr))", gap: "0 10px" } });
    el.append(grid);
    const cards = new Map();
    on("ds.stations", (stations) => {
      let problems = 0;
      for (const s of stations || []) {
        if (!cards.has(s.station)) {
          const card = makeCard(s.station);
          cards.set(s.station, card);
          grid.appendChild(card.root);
        }
        updateCard(cards.get(s.station), s);
        if (s.team && !s.robotLinked) problems++;
      }
      setBadge(problems ? String(problems) : "");
    });
  },
});
