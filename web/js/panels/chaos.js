// Chaos panel: how often things go wrong, manual fault injection, and the history of everything that went wrong.
import { h, replace, tag, toast, STATIONS } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

const run = (command, args) => cmd(command, args).catch((e) => toast(e.message, "error"));
const LEVELS = ["Off", "Occasional (~1.5/match)", "Frequent (~3.5/match)", "Mayhem (~7/match)"];

registerPanel({
  id: "chaos",
  title: "Chaos",
  mount(el, { setBadge }) {
    const configCard = h("div.card");
    const injectCard = h("div.card");
    const historyCard = h("div.card");
    const checksCard = h("div.card");
    el.append(configCard, injectCard, checksCard, historyCard);
    let target = "random";
    let lastHistory = 0;

    on("chaos.state", (state) => {
      if (!state) return;
      replace(configCard,
        h("h3", "Random problems", h("span.grow"), state.pending ? tag(`${state.pending} pending this match`, "warn") : null),
        h("div.row.wrap",
          LEVELS.map((label, level) => h("button.btn.sm", { class: state.level === level ? "on" : "",
            "aria-pressed": String(state.level === level),
            title: level ? "Random problems are drawn when each match starts" : "No random problems",
            onclick: () => run("chaos.config", { level }) }, label))),
        h("label.check.small", { style: { marginTop: "6px" } },
          h("input", { type: "checkbox", checked: state.reveal, onchange: (e) => run("chaos.config", { reveal: e.target.checked }) }),
          "Reveal the schedule (spoilers)"),
        state.reveal && state.upcoming?.length ? h("table.tbl", { style: { marginTop: "6px" } },
          h("tr", h("th", "At"), h("th", "Station"), h("th", "Problem")),
          state.upcoming.map((u) => h("tr", h("td.mono", `${u.at.toFixed(0)}s`), h("td.mono", u.station), h("td", u.type)))) : null,
        h("div.hint", { style: { marginTop: "6px" } },
          "Problems are drawn when each match starts and hit through the real paths: DS reboots and crashes, battery sag, ",
          "packet loss, mechanical faults in the physics, drivers pressing A-stop/E-stop, and fouls entered on the scoring panels."));

      if (!injectCard.contains(document.activeElement)) {
        const select = h("select", { onchange: (e) => { target = e.target.value; } },
          h("option", { value: "random", selected: target === "random" }, "Random robot"),
          STATIONS.map((s) => h("option", { value: s, selected: target === s }, s)));
        const groups = {};
        for (const t of state.types) (groups[t.category] ||= []).push(t);
        replace(injectCard,
          h("h3", "Inject now", h("span.grow"), select),
          Object.entries(groups).map(([category, types]) => h("div", { style: { marginBottom: "6px" } },
            h("div.small.muted", category),
            h("div.row.wrap", types.map((t) => h("button.btn.sm", {
              title: `Make this happen now to ${target === "random" ? "a random robot" : target}` +
                (t.autoOnly ? " (most realistic during AUTO)" : ""),
              onclick: () => run("chaos.inject", { type: t.id, station: target }),
            }, t.label))))));
      }

      // Recovery checks: what a correct FMS must show after each fault it can see, and whether the arena did.
      const checks = (state.checks || []).slice().reverse();
      const color = { pass: "#9be29b", fail: "#ff8080", waiting: "#ffd27a", "n/a": "#8b95a5" };
      const passed = checks.filter((c) => c.status === "pass").length;
      const failed = checks.filter((c) => c.status === "fail").length;
      replace(checksCard,
        h("h3", "Recovery checks", h("span.grow"), checks.length ? tag(`${passed} passed`, "ok") : null, " ",
          failed ? tag(`${failed} failed`, "bad") : null),
        checks.length ? h("table.tbl",
          h("tr", h("th", "Fault"), h("th", "Station"), h("th", "Result"), h("th", "")),
          checks.slice(0, 15).map((c) => h("tr",
            h("td.small", c.fault, h("span.muted.mono", ` @${Math.round(c.at)}s`)),
            h("td.mono", c.station),
            h("td.small", { style: { color: color[c.status] || "" } }, c.status),
            h("td.small", { title: c.detail || "" }, c.status === "waiting" ? `waiting: ${c.step}` : c.status === "pass" ? c.detail : c.step)))) :
          h("div.hint", "Each fault the arena can see comes with what a correct FMS must show and how fast (a rebooted radio drops and regains the link, an A-stop disables only for the rest of AUTO, an E-stop latches, a foul shows up in the score). Inject one or turn chaos on."));

      const history = state.history || [];
      setBadge(history.length && history.length !== lastHistory ? String(history.length) : "");
      lastHistory = history.length;
      replace(historyCard,
        h("h3", "What went wrong", h("span.grow"), h("button.btn.sm", { title: "Empty this list",
          onclick: () => run("chaos.clearHistory") }, "Clear")),
        history.length ? h("div.log", { style: { height: "260px" } },
          history.slice().reverse().map((r) => h("div.line.lvl-warn",
            h("span.t", new Date(r.time).toLocaleTimeString([], { hour12: false })),
            h("span.s", `${r.at.toFixed(0)}s`), h("span.m", r.text)))) : h("div.hint", "Nothing has gone wrong yet."));
    });
  },
});
