// Always-visible top bar: connection, match, clock, scores, readiness and quick match control.
import { h, led, replace, fmtClock, toast, ask } from "../core/dom.js";
import { icon } from "../core/icons.js";
import { on, onConnection, cmd, get } from "../core/store.js";
import { clock } from "../core/match.js";
import { showPanel } from "./dock.js";
import { enterViewer } from "./viewer.js";

export function mountMatchbar(host) {
  const simLed = led("red");
  const arenaLed = led("red");
  const arenaText = h("span.mb-value.small", "offline");
  const matchName = h("span.mb-value", "—");
  const period = h("span.mb-value", "Pre-Match");
  const time = h("span.mb-time", "0:20");
  const shift = h("span.mb-value.small", "");
  const redScore = h("span.score-pill.red", "0");
  const blueScore = h("span.score-pill.blue", "0");
  const ready = h("span.tag", "—");
  const readyCell = h("div.mb-cell", { title: "" }, h("span.mb-label", "Field Ready"), ready);

  const startButton = h("button.btn.go", { onclick: () => run("match.start"), title: "Start the loaded match" },
    icon("play"), "Start");
  const abortButton = h("button.btn.danger", { onclick: () => run("match.abort"), title: "Abort the match in progress" },
    icon("stop"), "Abort");
  const commitButton = h("button.btn.primary", { onclick: () => run("match.commit"), title: "Commit the results and post the score" },
    icon("check"), "Commit");
  const testButton = h("button.btn", { onclick: () => run("match.quickTest"), title: "Load a test match with teams on the field" },
    icon("flask"), "Load test");
  const viewerButton = h("button.btn.mb-viewer", {
    title: "Watch as a spectator: just the match, on the broadcast camera, with the score (Esc to come back)",
    onclick: enterViewer,
  }, icon("tv"), "Viewer");
  const bundleButton = h("button.btn.ghost", {
    title: "Report a problem: saves a bug-report bundle (black box, seed, live state, logs, arena database and reports)",
    "aria-label": "Report a problem",
    onclick: async () => {
      const reason = await ask("Report a problem", {
        message: "Saves everything needed to reproduce it (the last 10 minutes, logs and the arena database) as a zip " +
          "in run/reports. Passwords and tokens are removed.",
        placeholder: "What went wrong? (optional)",
        okLabel: "Save report",
      });
      if (reason === null) return;
      try {
        const result = await cmd("bundle.write", { reason: reason || "saved from the GUI" });
        toast(`Bug report saved: ${result.path}`, "good", 12000);
      } catch (err) {
        toast(err.message, "error");
      }
    },
  }, icon("bug"), "Report");

  async function run(command) {
    try {
      await cmd(command);
    } catch (err) {
      toast(err.message, "error");
    }
  }

  const brandSub = h("div.small.muted", "2026 REBUILT");
  on("sim.info", (info) => {
    if (info?.version) brandSub.textContent = `2026 REBUILT · v${info.version}`;
  });

  replace(
    host,
    h("div.mb-cell.mb-brand", h("img.logo", { src: "/img/icon.svg", alt: "" }), h("div", h("b", "Freezy Sim"), brandSub)),
    h("div.mb-cell", h("span.mb-label", "Links"),
      h("div.row", simLed, h("span.small", "Sim"), arenaLed, arenaText)),
    h("div.mb-cell", h("span.mb-label", "Match"), matchName),
    h("div.mb-cell", h("span.mb-label", "Period"), period),
    h("div.mb-cell", h("span.mb-label", "Time"), time),
    h("div.mb-cell", h("span.mb-label", "Hub Shift"), shift),
    h("div.mb-cell.mb-score", redScore, blueScore),
    readyCell,
    h("div.mb-spacer"),
    h("div.mb-cell.mb-actions", testButton, startButton, abortButton, commitButton, viewerButton, bundleButton),
  );

  // Link-loss banner: says what is down and what is being done about it, so a dropped link never looks like a frozen
  // field.
  const banner = h("div.link-banner", { hidden: true });
  document.body.append(banner);
  let simUp = false;
  let arenaUp = false;
  let arenaSeen = false;
  function updateBanner() {
    let text = "";
    let action = null;
    const launcher = get("arena.launcher");
    const state = launcher?.state;
    if (!simUp) {
      text = "Lost connection to Freezy Sim — reconnecting…";
    } else if (!arenaUp && launcher?.needsSource) {
      // First run (or a moved folder): nothing works until the arena folder is chosen, so say so up front.
      text = "Freezy Sim needs to know where your Freezy Arena (or Cheesy Arena) folder is.";
      action = ["Choose arena folder", () => showPanel("arena")];
    } else if (!arenaUp && state === "error") {
      text = "Freezy Arena could not start: " + (launcher.detail || "see the Arena panel");
      action = ["Open the Arena panel", () => showPanel("arena")];
    } else if (arenaSeen && !arenaUp) {
      text = state === "exited" ? "Freezy Arena crashed — restarting it…"
        : state === "starting" || state === "building" || state === "preparing" ? "Freezy Arena is starting…"
          : state === "stopped" ? "Freezy Arena is stopped — start it from the Arena panel."
            : "Lost connection to Freezy Arena — reconnecting…";
    } else if (!arenaUp && (state === "building" || state === "copying" || state === "preparing")) {
      text = "Building Freezy Arena for the first time — this can take a minute or two…";
    }
    banner.hidden = !text;
    banner.classList.toggle("actionable", !!action);
    replace(banner, text, action ? h("button.btn.sm", { onclick: action[1] }, action[0]) : null);
  }

  onConnection((connected) => {
    simUp = connected;
    simLed.className = "led " + (connected ? "green" : "red");
    updateBanner();
  });

  on("arena.sockets", (sockets) => {
    const matchPlay = (sockets || []).find((s) => s.name === "matchPlay");
    const up = matchPlay?.connected;
    arenaUp = !!up;
    if (up) arenaSeen = true;
    arenaLed.className = "led " + (up ? "green" : "red");
    arenaText.textContent = up ? "Arena" : "Arena offline";
    updateBanner();
  });
  on("arena.launcher", updateBanner);

  on("arena.matchLoad", (load) => {
    matchName.textContent = load?.Match?.LongName || "—";
  });

  on("arena.realtimeScore", (score) => {
    redScore.textContent = score?.Red?.ScoreSummary?.Score ?? 0;
    blueScore.textContent = score?.Blue?.ScoreSummary?.Score ?? 0;
  });

  // Match buttons are enabled only for what the arena accepts now; with the arena offline, nothing.
  let lastStatus = null;
  function gateButtons() {
    const status = arenaUp ? lastStatus : null;
    const state = status?.MatchState;
    testButton.disabled = !arenaUp || (state >= 1 && state <= 4) || state === 6;
    startButton.disabled = !(status && state === 0 && status.CanStartMatch);
    abortButton.disabled = !status || (!(state >= 1 && state <= 4) && state !== 6);
    commitButton.disabled = !status || state !== 5;
  }
  gateButtons();
  on("arena.sockets", gateButtons);

  on("arena.arenaStatus", (status) => {
    if (!status) return;
    lastStatus = status;
    gateButtons();
    const okay = status.CanStartMatch;
    ready.className = "tag " + (okay ? "good" : "bad");
    ready.textContent = okay ? "READY" : `${(status.StartMatchConditions || []).length} blocker(s)`;
    readyCell.title = (status.StartMatchConditions || []).join("\n");
  });

  function tick() {
    const c = clock();
    period.textContent = c.period;
    time.textContent = fmtClock(c.remaining);
    shift.textContent = c.shift ? `${c.shift} · ${fmtClock(c.shiftRemaining)}` : "—";
    requestAnimationFrame(tick);
  }
  tick();
}
