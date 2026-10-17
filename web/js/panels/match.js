// Match control: load test/practice/qual/playoff matches and drive the match through the arena's Match Play socket.
import { h, replace, cmdButton, toast, STATIONS, led } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

registerPanel({
  id: "match",
  title: "Match",
  mount(el) {
    const controls = h("div.card",
      h("h3", "Match control"),
      h("div.btn-row",
        cmdButton("Load test match", "match.loadTest"),
        cmdButton("▶ Start match", "match.start", {}, "go"),
        cmdButton("■ Abort", "match.abort", {}, "danger"),
        cmdButton("Commit & post", "match.commit", {}, "primary"),
        cmdButton("Discard results", "match.discard", {}, "warn"),
      ),
      h("div.btn-row",
        cmdButton("Signal field reset", "match.signalReset"),
        cmdButton("Signal volunteers", "match.signalVolunteers"),
      ),
    );

    const stations = h("div.card");
    const listCard = h("div.card");
    const typeSelect = h("select",
      ["practice", "qualification", "playoff"].map((t) => h("option", { value: t }, t)));
    const listBody = h("div", { style: { maxHeight: "360px", overflow: "auto" } });

    replace(listCard,
      h("h3", "Schedule", h("span.grow"), typeSelect,
        h("button.btn.sm", { onclick: loadList }, "Refresh")),
      listBody,
    );
    // Event director: play the schedule unattended (load, field reset, start, AI play, auto-referee, commit).
    const directorCard = h("div.card");
    const directorTypes = { qualification: "Qualifications", practice: "Practice", event: "Whole event (quals → selection → playoffs)",
      selection: "Alliance selection only", playoff: "Playoffs only" };
    const directorType = h("select", Object.entries(directorTypes).map(([t, label]) => h("option", { value: t }, label)));
    const directorLimit = h("input", { type: "number", min: 0, value: 0, style: { width: "60px" }, title: "0 = all" });
    const directorBody = h("div");
    replace(directorCard,
      h("h3", "Event director", h("span.grow"), directorType, directorLimit,
        h("button.btn.sm.go", { onclick: () => cmd("director.start", { type: directorType.value,
          limit: Number(directorLimit.value) || 0 }).catch((e) => toast(e.message, "error")) }, "Run"),
        h("button.btn.sm.danger", { onclick: () => cmd("director.stop") }, "Stop")),
      directorBody);
    on("director.state", (state) => {
      if (!state) return;
      const done = (state.matches || []).length;
      replace(directorBody,
        state.running ? h("div.small", `Running ${state.type}: ${done}/${state.total} done · ${state.current || ""} — ${state.phase}`) :
          done ? h("div.small", `Finished ${done} ${state.type} matches.`, state.report ? h("span.muted", ` Report: ${state.report}`) : null) :
          h("div.hint", "Runs the arena's unplayed matches in order, like a scorekeeper and field crew would, and reports cycle times and anything that went wrong."),
        done ? h("div.small.mono", `average cycle ${(state.avgCycle / 60).toFixed(1)} min · field ready ${state.avgReady.toFixed(0)} s after load`) : null,
        (state.anomalies || []).slice(-5).map((a) => h("div.small", { style: { color: "#ff9b9b" } }, a)),
        done ? h("table.tbl",
          h("tr", h("th", "Match"), h("th", "Ready"), h("th", "Length"), h("th", "Cycle"), h("th", "Score")),
          state.matches.slice(-8).reverse().map((m) => h("tr",
            h("td.mono", m.name), h("td.mono", `${m.readySecs.toFixed(0)} s`), h("td.mono", `${m.duration.toFixed(0)} s`),
            h("td.mono", m.cycleSecs ? `${(m.cycleSecs / 60).toFixed(1)} min` : "—"),
            h("td.mono", m.problem ? h("span", { style: { color: "#ff9b9b" } }, m.problem) : `${m.red}–${m.blue}`)))) : null,
        state.winner ? h("div", { style: { margin: "6px 0", fontWeight: 600 } }, `🏆 ${state.winner}`) : null,
        (state.timeouts || []).map((t) => h("div.small.muted", t)),
        state.alliances?.length ? h("table.tbl",
          h("tr", h("th", "Alliance"), h("th", "Captain"), h("th", "Picks")),
          state.alliances.map((teams, i) => h("tr", h("td.mono", i + 1), h("td.mono", teams[0]), h("td.mono", teams.slice(1).join(", "))))) :
          (state.selection || []).length ? h("div.log", { style: { height: "120px" } },
            state.selection.slice().reverse().map((s) => h("div.line", h("span.s", s.kind), h("span.m", s.text)))) : null,
        state.pickList?.length && !state.alliances?.length ? h("table.tbl",
          h("tr", h("th", "#"), h("th", "Team"), h("th", "Rank"), h("th", "OPR"), h("th", "FUEL"), h("th", "Climb"), h("th", "Reliable")),
          state.pickList.slice(0, 12).map((r, i) => h("tr", h("td.mono", i + 1), h("td.mono", r.team), h("td.mono", r.rank || "—"),
            h("td.mono", r.opr.toFixed(1)), h("td.mono", r.avgScored.toFixed(1)), h("td.mono", r.avgClimbPts.toFixed(0)),
            h("td.mono", `${(r.reliability * 100).toFixed(0)}%`)))) : null);
    });
    // Score check: after every match the arena's score is compared with what really happened on the field.
    const diffCard = h("div.card");
    const history = [];
    on("arena.diff", (diff) => {
      if (!diff) return;
      if (!history.length || history[history.length - 1].at !== diff.at) history.push(diff);
      if (history.length > 10) history.shift();
      const color = { error: "#ff8080", warn: "#ffd27a", info: "#8b95a5" };
      replace(diffCard,
        h("h3", "Score check", h("span.grow"), h("span.small.muted", `${diff.match} · seed ${diff.seed}`)),
        diff.ok ? h("div.small", { style: { color: "#9be29b" } }, `The arena's score matches the field (${(diff.checked || []).join(", ")}).`) :
          h("div.small", { style: { color: "#ff8080" } }, "The arena's score differs from what happened on the field:"),
        (diff.lines || []).map((line) => h("div.small", { style: { color: color[line.severity] || "" } },
          `${line.alliance ? line.alliance.toUpperCase() + " " : ""}${line.area}: ${line.text}`)),
        history.length > 1 ? h("div.small.muted", { style: { marginTop: "4px" } },
          "Recent: ", history.slice(0, -1).reverse().map((d) => h("span", { style: { color: d.ok ? "#9be29b" : "#ff8080", marginRight: "8px" } },
            `${d.match} ${d.ok ? "✓" : "✗"}`))) : null);
    });
    replace(diffCard, h("h3", "Score check"), h("div.hint",
      "After each match the simulator compares the arena's score with what really happened: every FUEL the hub sensors counted (and which shift it belongs in), tower levels, and the fouls the referees called."));
    el.append(controls, directorCard, diffCard, stations, listCard);

    async function loadList() {
      try {
        const matches = await cmd("arena.matches", { type: typeSelect.value });
        replace(listBody, !matches?.length ? h("div.hint", "No matches of this type. Generate a schedule in the arena.")
          : h("table.tbl",
            h("tr", h("th", "Match"), h("th", "Red"), h("th", "Blue"), h("th", "")),
            matches.map((m) => h("tr",
              h("td.mono", m.ShortName || m.LongName),
              h("td.mono", { style: { color: "#ffb1b3" } }, [m.Red1, m.Red2, m.Red3].join(" ")),
              h("td.mono", { style: { color: "#b7d2ff" } }, [m.Blue1, m.Blue2, m.Blue3].join(" ")),
              h("td", h("button.btn.sm", {
                onclick: () => cmd("match.load", { matchId: m.Id }).catch((e) => toast(e.message, "error")),
              }, "Load")),
            ))));
      } catch (err) {
        replace(listBody, h("div.hint", err.message));
      }
    }
    typeSelect.addEventListener("change", loadList);

    on("arena.arenaStatus", (status) => {
      if (!status?.AllianceStations) return;
      replace(stations, h("h3", "Stations (arena view)"),
        h("table.tbl",
          h("tr", h("th", "Stn"), h("th", "Team"), h("th", "DS"), h("th", "Radio"), h("th", "RIO"), h("th", "Code"),
            h("th", "Batt"), h("th", "E"), h("th", "A"), h("th", "")),
          STATIONS.map((station) => {
            const s = status.AllianceStations[station] || {};
            const ds = s.DsConn || {};
            const color = station[0] === "R" ? "#ffb1b3" : "#b7d2ff";
            return h("tr",
              h("td.mono", { style: { color } }, station),
              h("td.mono", s.Team?.Id ?? "—"),
              h("td", led(ds.DsLinked ? "green" : "red")),
              h("td", led(ds.RadioLinked ? "green" : "red")),
              h("td", led(ds.RioLinked ? "green" : "red")),
              h("td", led(ds.RobotLinked ? "green" : "red")),
              h("td.mono", ds.BatteryVoltage ? ds.BatteryVoltage.toFixed(1) : "—"),
              h("td", led(s.EStop ? "amber" : "")),
              h("td", led(s.AStop ? "amber" : "")),
              h("td", h("button.btn.sm", { class: s.Bypass ? "warn" : "",
                onclick: () => cmd("match.bypass", { station }).catch((e) => toast(e.message, "error")),
              }, s.Bypass ? "Bypassed" : "Bypass")),
            );
          })));
    });
    setTimeout(loadList, 1500);
  },
});
