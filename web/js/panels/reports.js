// Match reports: the auto-referee's live log of the current match (climbs, fouls, faults) and every saved report.
import { h, replace, tag, toast, led } from "../core/dom.js";
import { on, cmd, get } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

const run = (command, args) => cmd(command, args).catch((e) => toast(e.message, "error"));
const KIND_COLOR = { climb: "#7ee2a8", foul: "#ffd27a", chaos: "#ffb27a", world: "#9fc6ff", match: "#e7ecf2" };

function eventRows(events) {
  return events.map((e) => h("div.line",
    h("span.t", e.matchTime ? `${e.matchTime.toFixed(0)}s` : e.wall),
    h("span.s", { style: { color: KIND_COLOR[e.kind] || "var(--accent)" } }, e.kind),
    h("span.m", e.text)));
}

function reportDetail(report) {
  const teams = report.teams || [];
  return h("div",
    h("div.row", { style: { gap: "12px", margin: "6px 0" } },
      h("span.score-pill.red", report.redScore), h("span.score-pill.blue", report.blueScore),
      h("span.small.muted", `Red plan: ${report.plans?.[0] || "—"} · Blue plan: ${report.plans?.[1] || "—"}`)),
    h("table.tbl",
      h("tr", h("th", "Stn"), h("th", "Team"), h("th", "Role"), h("th", "Scored"), h("th", "Passes"), h("th", "Auto L1"), h("th", "End climb")),
      (report.robots || []).map((r, i) => h("tr",
        h("td.mono", r.station), h("td.mono", teams[i] || r.team),
        h("td.small", `${r.plan?.strategy || ""} / ${r.plan?.auto || ""}`),
        h("td.mono", r.scored), h("td.mono", r.passes),
        h("td", report.autoTower?.[i] ? "✓" : ""), h("td.mono", report.endTower?.[i] ? `L${report.endTower[i]}` : "")))),
    h("div.log", { style: { height: "240px", marginTop: "8px" } }, eventRows(report.events || [])));
}

registerPanel({
  id: "reports",
  title: "Match Log",
  mount(el) {
    const refCard = h("div.card");
    const currentCard = h("div.card");
    const listCard = h("div.card");
    const detail = h("div");
    el.append(refCard, currentCard, listCard);

    on("referee.settings", (s) => {
      if (!s) return;
      const panels = get("referee.panels") || [];
      const toggle = (key, label, title) => h("label.check", { title },
        h("input", { type: "checkbox", checked: s[key], onchange: (e) => run("referee.settings", { ...s, [key]: e.target.checked }) }), label);
      replace(refCard,
        h("h3", "Auto-referee", h("span.grow"),
          h("span.row.small", led(panels[0] ? "green" : "red"), "red panel ", led(panels[1] ? "green" : "red"), "blue panel")),
        h("div.row.wrap", { style: { gap: "14px" } },
          toggle("enabled", "Score tower climbs", "Read climbs off the field and enter them on the scoring panels"),
          toggle("autoCommit", "Commit scoring panels", "Commit both scoring panels after the end-of-match assessment"),
          toggle("callFouls", "Call pinning fouls", "Defenders pinning an opponent for 3 s draw a foul"),
          toggle("autoPost", "Auto commit & post", "Act as the scorekeeper: commit and post each match")),
        h("div.btn-row",
          h("button.btn.sm", { onclick: () => run("referee.foul", { alliance: "red", major: false }) }, "Minor foul on red"),
          h("button.btn.sm", { onclick: () => run("referee.foul", { alliance: "blue", major: false }) }, "Minor foul on blue"),
          h("button.btn.sm", { onclick: () => run("referee.foul", { alliance: "red", major: true }) }, "Major on red"),
          h("button.btn.sm", { onclick: () => run("referee.foul", { alliance: "blue", major: true }) }, "Major on blue")));
    });

    on("reports.current", (report) => {
      if (!report) return;
      replace(currentCard,
        h("h3", report.finished ? "Last match" : "Current match", h("span.grow"), tag(report.match),
          report.aborted ? tag("ABORTED", "bad") : null),
        report.finished ? reportDetail(report) : h("div.log", { style: { height: "220px" } }, eventRows(report.events || [])));
    });

    on("reports.list", (list) => {
      replace(listCard, h("h3", "Saved reports (run/reports)"),
        !list?.length ? h("div.hint", "Reports are written when each match ends.") : h("table.tbl",
          h("tr", h("th", "When"), h("th", "Match"), h("th", "Score"), h("th", "Events"), h("th", "")),
          list.map((r) => h("tr",
            h("td.small", new Date(r.startedAt).toLocaleString([], { hour12: false })),
            h("td", r.match, r.aborted ? " (aborted)" : ""),
            h("td.mono", h("span", { style: { color: "#ffb1b3" } }, r.redScore), " – ", h("span", { style: { color: "#b7d2ff" } }, r.blueScore)),
            h("td.mono", r.events),
            h("td", h("button.btn.sm", { onclick: async () => {
              const report = await cmd("reports.get", { file: r.file }).catch((e) => toast(e.message, "error"));
              if (report) replace(detail, h("div.card", h("h3", report.match), reportDetail(report)));
            } }, "Open"))))),
        detail);
    });
  },
});
