// Always-visible top bar: connection, match, clock, scores, readiness and quick match control.
import { h, led, replace, fmtClock, toast } from "../core/dom.js";
import { on, onConnection, cmd, get } from "../core/store.js";
import { clock } from "../core/match.js";

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

  const startButton = h("button.btn.go", { onclick: () => run("match.start") }, "▶ Start");
  const abortButton = h("button.btn.danger", { onclick: () => run("match.abort") }, "■ Abort");
  const commitButton = h("button.btn.primary", { onclick: () => run("match.commit") }, "Commit");
  const testButton = h("button.btn", { onclick: () => run("match.quickTest"), title: "Load a test match with teams on the field" }, "Load Test");
  const bundleButton = h("button.btn", {
    title: "Save a bug-report bundle: black box, seed, live state, event + arena logs, arena database and reports",
    onclick: async () => {
      const reason = prompt("What went wrong? (saved in the bundle)", "");
      if (reason === null) return;
      try {
        const result = await cmd("bundle.write", { reason: reason || "saved from the GUI" });
        toast(`Bug report saved: ${result.path}`, "info", 12000);
      } catch (err) {
        toast(err.message, "error");
      }
    },
  }, "🐞 Report");

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
    h("div.mb-cell.mb-brand", h("span.logo"), h("div", h("b", "Freezy Sim"), brandSub)),
    h("div.mb-cell", h("span.mb-label", "Links"),
      h("div.row", simLed, h("span.small", "Sim"), arenaLed, arenaText)),
    h("div.mb-cell", h("span.mb-label", "Match"), matchName),
    h("div.mb-cell", h("span.mb-label", "Period"), period),
    h("div.mb-cell", h("span.mb-label", "Time"), time),
    h("div.mb-cell", h("span.mb-label", "Hub Shift"), shift),
    h("div.mb-cell.mb-score", redScore, blueScore),
    readyCell,
    h("div.mb-spacer"),
    h("div.mb-cell.mb-actions", testButton, startButton, abortButton, commitButton, bundleButton),
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
    if (!simUp) {
      text = "Lost connection to Freezy Sim — reconnecting…";
    } else if (arenaSeen && !arenaUp) {
      const launcher = get("arena.launcher");
      const state = launcher?.state;
      text = state === "exited" ? "Freezy Arena crashed — restarting it…"
        : state === "starting" || state === "building" || state === "preparing" ? "Freezy Arena is starting…"
          : state === "stopped" ? "Freezy Arena is stopped — start it from the Arena panel."
            : "Lost connection to Freezy Arena — reconnecting…";
    }
    banner.hidden = !text;
    banner.textContent = text;
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

  on("arena.arenaStatus", (status) => {
    if (!status) return;
    const okay = status.CanStartMatch;
    ready.className = "tag " + (okay ? "good" : "bad");
    ready.textContent = okay ? "READY" : `${(status.StartMatchConditions || []).length} blocker(s)`;
    readyCell.title = (status.StartMatchConditions || []).join("\n");
    const state = status.MatchState;
    startButton.disabled = !(state === 0 && okay);
    abortButton.disabled = !(state >= 1 && state <= 4) && state !== 6;
    commitButton.disabled = state !== 5;
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
