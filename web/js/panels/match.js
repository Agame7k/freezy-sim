// Match control: load test/practice/qual/playoff matches and drive the match through the arena's Match Play socket.
import { h, replace, cmdButton, toast, STATIONS, led } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";
import { icon } from "../core/icons.js";

registerPanel({
  id: "match",
  title: "Match",
  mount(el) {
    const loadButton = cmdButton("Load test match", "match.loadTest", {}, "",
      { title: "Load the arena's test match with the teams already in the stations (Load test in the top bar also fills the stations)" });
    const startButton = cmdButton([icon("play"), "Start match"], "match.start", {}, "go",
      { title: "Start the loaded match (needs Field Ready: no start blockers)" });
    const abortButton = cmdButton([icon("stop"), "Abort"], "match.abort", {}, "danger",
      { title: "Stop the match in progress and disable every robot" });
    const commitButton = cmdButton([icon("check"), "Commit & post"], "match.commit", {}, "primary",
      { title: "Save the results to the arena and show the score on the audience display" });
    const discardButton = cmdButton("Discard results", "match.discard", {}, "warn", {
      title: "Throw away this match's results without saving them",
      confirm: { title: "Discard this match's results?", message: "The score is not saved or posted, and the arena loads the next match. This can't be undone.",
        okLabel: "Discard results", danger: true },
    });
    const resetButton = cmdButton("Signal field reset", "match.signalReset", {}, "",
      { title: "Tell the field crew it is safe to reset the field (green lights and the field-reset light). Not during a match." });
    const volunteersButton = cmdButton("Signal volunteers", "match.signalVolunteers", {}, "",
      { title: "Call the field volunteers in to count and collect FUEL (purple lights). Not during a match." });
    const controls = h("div.card",
      h("h3", "Match control"),
      h("div.btn-row", loadButton, startButton, abortButton, commitButton, discardButton),
      h("div.btn-row", resetButton, volunteersButton),
    );
    // Only the actions the arena accepts in the current match state are enabled; it ignores or rejects the rest. With the
    // arena offline, none of them can work.
    let lastStatus = null;
    let matchPlayUp = false;
    on("arena.sockets", (sockets) => {
      matchPlayUp = !!(sockets || []).find((s) => s.name === "matchPlay")?.connected;
      gateButtons(lastStatus);
    });
    on("arena.arenaStatus", (status) => gateButtons(status));
    function gateButtons(status) {
      lastStatus = status;
      if (!status || !matchPlayUp) {
        for (const button of [loadButton, startButton, abortButton, commitButton, discardButton, resetButton, volunteersButton]) {
          button.disabled = true;
        }
        return;
      }
      const state = status.MatchState;
      const inMatch = state >= 1 && state <= 4;
      loadButton.disabled = inMatch || state === 6;
      startButton.disabled = !(state === 0 && status.CanStartMatch);
      abortButton.disabled = !inMatch && state !== 6;
      commitButton.disabled = state !== 5;
      discardButton.disabled = state !== 5;
      resetButton.disabled = inMatch;
      volunteersButton.disabled = inMatch;
    }

    const stations = h("div.card");
    const listCard = h("div.card");
    const typeSelect = h("select",
      ["practice", "qualification", "playoff"].map((t) => h("option", { value: t }, t)));
    const listBody = h("div", { style: { maxHeight: "360px", overflow: "auto" } });

    replace(listCard,
      h("h3", "Schedule", h("span.grow"), typeSelect,
        h("button.btn.sm", { onclick: loadList, title: "Fetch the schedule from the arena again" }, "Refresh")),
      listBody,
    );
    // Event director: play the schedule unattended (load, field reset, start, AI play, auto-referee, commit).
    const directorCard = h("div.card");
    const directorTypes = { qualification: "Qualifications", practice: "Practice", event: "Whole event (quals → selection → playoffs)",
      selection: "Alliance selection only", playoff: "Playoffs only" };
    const directorType = h("select", Object.entries(directorTypes).map(([t, label]) => h("option", { value: t }, label)));
    const directorLimit = h("input", { type: "number", min: 0, value: 0, style: { width: "64px" },
      title: "How many matches to play (0 = all of them)", "aria-label": "Match limit" });
    const directorBody = h("div");
    const directorRun = h("button.btn.sm.go", {
      title: "Play the chosen matches unattended: load, field reset, start, AI play, referee and commit",
      onclick: () => cmd("director.start", { type: directorType.value, limit: Number(directorLimit.value) || 0 })
        .catch((e) => toast(e.message, "error")),
    }, icon("play", 12), "Run");
    const directorStop = h("button.btn.sm.danger", { disabled: true, title: "Stop after the current step; the match on the field is left as it is",
      onclick: () => cmd("director.stop").catch((e) => toast(e.message, "error")) }, icon("stop", 12), "Stop");
    replace(directorCard,
      h("h3", "Event director"),
      h("div.row.wrap", { style: { marginBottom: "8px" } },
        directorType,
        h("label.row.small.muted", { style: { gap: "6px" } }, "Matches", directorLimit, h("span.hint", "0 = all")),
        h("span.grow"),
        directorRun, directorStop),
      directorBody);
    on("director.state", (state) => {
      if (!state) return;
      directorRun.disabled = !!state.running;
      directorStop.disabled = !state.running;
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
        state.winner ? h("div.row", { style: { margin: "6px 0", fontWeight: 600, color: "var(--amber)" } }, icon("trophy"), state.winner) : null,
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
        replace(listBody, !matches?.length ? h("div.hint", "No matches of this type yet. Generate a schedule in the arena, then Refresh.")
          : h("table.tbl",
            h("tr", h("th", "Match"), h("th", "Red"), h("th", "Blue"), h("th", "")),
            matches.map((m) => h("tr",
              h("td.mono", m.ShortName || m.LongName),
              h("td.mono", { style: { color: "#ffb1b3" } }, [m.Red1, m.Red2, m.Red3].join(" ")),
              h("td.mono", { style: { color: "#b7d2ff" } }, [m.Blue1, m.Blue2, m.Blue3].join(" ")),
              h("td", h("button.btn.sm", {
                title: `Load ${m.LongName || m.ShortName} into the arena (not during a match)`,
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
            h("th", { title: "Battery voltage" }, "Batt"), h("th", { title: "E-stopped" }, "E"),
            h("th", { title: "A-stopped" }, "A"), h("th", "")),
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
              h("td", h("button.btn.sm", { class: s.Bypass ? "warn" : "", "aria-pressed": String(!!s.Bypass),
                title: s.Bypass ? "Bypassed: the match can start without this station. Click to stop bypassing it."
                  : "Let the match start without this station (its robot stays disabled)",
                onclick: () => cmd("match.bypass", { station }).catch((e) => toast(e.message, "error")),
              }, s.Bypass ? "Bypassed" : "Bypass")),
            );
          })));
    });
    // Load the schedule whenever the arena (re)connects, so it is not left showing an error from before the arena was up.
    let arenaUp = false;
    on("arena.sockets", (sockets) => {
      const up = !!(sockets || []).find((s) => s.name === "matchPlay")?.connected;
      if (up && !arenaUp) setTimeout(loadList, 500);
      arenaUp = up;
    });
    replace(listBody, h("div.hint", "Waiting for the arena…"));
  },
});
