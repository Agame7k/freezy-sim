// Officials panel: what the referees are calling. Live PIN counts (G418), every foul called this match with the rule,
// the robots involved and why, the totals by alliance, and the head referee's event-wide record: warnings, YELLOW and
// RED CARDS carried between matches, cards to present after this match, and recent rulings.
import { h, replace, tag } from "../core/dom.js";
import { on } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

const PIN_LIMIT = 3; // seconds (G418)
const colorOf = (station) => (station && station[0] === "R" ? "#ffb1b3" : station && station[0] === "B" ? "#b7d2ff" : "");

registerPanel({
  id: "officials",
  title: "Officials",
  mount(el, { setBadge }) {
    const liveCard = h("div.card");
    const callsCard = h("div.card");
    const headCard = h("div.card");
    el.append(liveCard, callsCard, headCard);

    // This match's calls, collected from the "flags in the air" list (which only holds the last few seconds).
    let calls = [];
    const seen = new Set();
    let lastTotals = [0, 0];

    on("world.state", (state) => {
      const officials = state && state.officials;
      if (!officials) return;
      const totals = officials.totals || [0, 0];
      if (totals[0] + totals[1] < lastTotals[0] + lastTotals[1]) { // a new match: the counts went back to zero
        calls = [];
        seen.clear();
      }
      lastTotals = totals;
      for (const call of officials.recent || []) {
        const key = `${call.at}|${call.rule}|${call.station || ""}`;
        if (seen.has(key)) continue;
        seen.add(key);
        calls.push(call);
      }
      setBadge(calls.length ? String(calls.length) : "");

      const pins = officials.pins || [];
      replace(liveCard,
        h("h3", "On the field now", h("span.grow"),
          tag(`Red ${totals[0]} fouls`, totals[0] ? "bad" : ""), " ", tag(`Blue ${totals[1]} fouls`, totals[1] ? "bad" : "")),
        pins.length ? h("table.tbl",
          h("tr", h("th", "Pin"), h("th", "Count"), h("th", "")),
          pins.map((pin) => h("tr",
            h("td.small", h("b", { style: { color: colorOf(pin.pinner) } }, pin.pinner), " pinning ",
              h("b", { style: { color: colorOf(pin.pinned) } }, pin.pinned)),
            h("td", { style: { width: "45%" } }, h("div", {
              style: { height: "8px", background: "#2a313c", borderRadius: "4px", overflow: "hidden" },
              title: `${pin.seconds.toFixed(1)} of ${PIN_LIMIT} s${pin.paused ? " (count paused)" : ""}`,
            }, h("div", { style: {
              width: `${Math.min(100, (100 * pin.seconds) / PIN_LIMIT)}%`, height: "100%",
              background: pin.seconds > 2.2 ? "#ff6b6b" : pin.seconds > 1.2 ? "#ffd27a" : "#9be29b",
            } }))),
            h("td.small.mono", `${pin.seconds.toFixed(1)} s`, pin.paused ? " ⏸" : "", pin.fouls ? ` · ${pin.fouls} called` : "")))) :
          h("div.hint", "No pins in progress. The referees count a PIN from first contact; holding one past 3 s is a foul (G418)."));

      replace(callsCard,
        h("h3", "Fouls this match"),
        calls.length ? h("table.tbl",
          h("tr", h("th", "Time"), h("th", "Rule"), h("th", "Against"), h("th", "Why")),
          calls.slice().reverse().map((call) => h("tr",
            h("td.mono.small", `${Math.floor(call.matchTime)}s`),
            h("td.small", h("b", call.rule), " ", tag(call.major ? "MAJOR" : "minor", call.major ? "bad" : "warn"),
              call.strategic ? h("div.small.muted", "strategic") : null),
            h("td.small", h("b", { style: { color: colorOf(call.station) || (call.alliance === 0 ? "#ffb1b3" : "#b7d2ff") } },
              call.station || (call.alliance === 0 ? "Red" : "Blue")),
            call.team ? h("span.mono.muted", ` ${call.team}`) : null,
            call.victim ? h("div.small.muted", `against ${call.victim}`) : null),
            h("td.small", call.reason, call.awardLevel3 ? h("div.small", { style: { color: "#9be29b" } },
              `${call.awardLevel3} awarded LEVEL 3 (G420)`) : null,
            call.egregious ? h("div.small", { style: { color: "#ff9b9b" } }, "referred to the head referee") : null)))) :
          h("div.hint", "No fouls called yet this match."));
    });

    on("officials.headRef", (head) => {
      if (!head) return;
      const ledger = Object.entries(head.ledger || {})
        .filter(([, r]) => r.warnings || r.yellow || r.red || r.fouls)
        .sort((a, b) => (b[1].red - a[1].red) || (Number(b[1].yellow) - Number(a[1].yellow)) || (b[1].fouls - a[1].fouls));
      const pending = Object.entries(head.pending || {});
      replace(headCard,
        h("h3", "Head referee"),
        pending.length ? h("div.row.wrap", { style: { marginBottom: "6px" } }, h("span.small", "Cards after this match: "),
          pending.map(([team, card]) => tag(`${team} ${card}`, /red/i.test(card) ? "bad" : "warn"))) : null,
        ledger.length ? h("table.tbl",
          h("tr", h("th", "Team"), h("th", "Fouls"), h("th", "Warnings"), h("th", "Cards"), h("th", "Strategic G408")),
          ledger.map(([team, r]) => h("tr",
            h("td.mono", team), h("td.mono", r.fouls), h("td.mono", r.warnings),
            h("td", r.red ? tag(`RED ×${r.red}`, "bad") : null, r.yellow ? tag("YELLOW (carried)", "warn") : null,
              !r.red && !r.yellow ? h("span.muted", "—") : null),
            h("td.mono", r.strategic || 0)))) :
          h("div.hint", "No team has a foul record yet this tournament phase."),
        h("div.small.muted", { style: { marginTop: "8px" } }, "Recent rulings"),
        (head.rulings || []).length ? h("div.log", { style: { height: "180px" } },
          head.rulings.slice().reverse().map((ruling) => h("div.line",
            h("span.t", ruling.time), h("span.s", ruling.match || ""), h("span.m", ruling.text)))) :
          h("div.hint", "No rulings yet."));
    });
  },
});
