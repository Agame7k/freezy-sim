// AI panel: what every drive team is thinking (its job, the options it weighed, its target), its shot timing record and
// endgame decision, the alliance coaches' calls, the human players, and the AI settings (difficulty, sight,
// personalities, learning).
import { h, replace, tag, toast, confirmAction } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";
import { setAiOverlay, aiOverlayEnabled } from "../scene/aioverlay.js";

const DIFFICULTIES = [["rookie", "Rookie"], ["regional", "Regional"], ["championship", "Championship"], ["einstein", "Einstein"]];
const JOB_LABELS = {
  score: "Scoring", stage: "Staging (hub off)", defend: "Defense", steal: "Stealing", shuttle: "Shuttling",
  reverse: "Reverse shuttle", feed: "Outpost feed", deliver: "Delivering to HP", herd: "Herding FUEL", climb: "Endgame climb",
};
const TRAITS = [
  ["aggression", "Aggression", "Appetite for defense and contact"],
  ["discipline", "Discipline", "Releases pins early, follows the rules"],
  ["risk", "Risk", "How close it cuts early/late shots"],
  ["consistency", "Consistency", "Low = overshoots and fumbles"],
  ["reaction", "Reaction (s)", "Driver reaction time"],
  ["curiosity", "Curiosity", "How readily it tries new autos, shot ranges and roles"],
];
const CHOICE_LABELS = { auto: "Auto", range: "Shot range", inactive: "Hub off", climb: "Climb" };

const run = (command, args) => cmd(command, args).catch((e) => toast(e.message, "error"));
const pct = (a, b) => (b > 0 ? `${Math.round((100 * a) / b)}%` : "—");

registerPanel({
  id: "ai",
  title: "AI",
  mount(el) {
    const settingsCard = h("div.card");
    const robotsCard = h("div.card");
    const autoCard = h("div.card");
    const coachCard = h("div.card");
    const humanCard = h("div.card");
    const personalityCard = h("div.card");
    const learnCard = h("div.card");
    const designCard = h("div.card");
    el.append(settingsCard, robotsCard, designCard, autoCard, coachCard, humanCard, personalityCard, learnCard);
    const busy = (card) => card.contains(document.activeElement) && /SELECT|INPUT/.test(document.activeElement.tagName);
    const colorOf = (station) => (station[0] === "R" ? "#ffb1b3" : "#b7d2ff");
    let editingTeam = 0;
    let libraryOpen = false;
    let training = null;
    let trainMatches = 30;
    on("ai.training", (state) => { training = state; });

    // Pre-season training: the event's teams play practice matches offline and bring back what they learned.
    function trainingRow() {
      const running = training && training.running;
      return h("div.row", { style: { marginTop: "6px" } },
        h("button.btn.sm", { disabled: running, title: "The event's teams play practice matches offline (much faster than real time) and keep what they learn",
          onclick: () => run("world.ai.train", { matches: trainMatches }) }, "Pre-season training"),
        h("input", { type: "number", min: 6, max: 400, value: trainMatches, style: { width: "60px" },
          onchange: (e) => { trainMatches = Number(e.target.value) || 30; } }),
        h("span.small", " matches "),
        running ? h("span.small", `playing ${training.done}/${training.total} with ${training.teams} teams…`) :
          training && training.summary ? h("span.small.muted",
            `last run: avg ${training.summary.avgScore.toFixed(0)} pts, ${training.summary.autoFuel.toFixed(0)} AUTO FUEL per alliance`) :
          training && training.error ? h("span.small", { style: { color: "#ff9b9b" } }, training.error) : null);
    }

    // Team-built robots: what each team built and why, and the pit changes it made between matches.
    function renderDesigns(ai) {
      const onField = new Map(ai.robots.filter((r) => r.team).map((r) => [r.team, r.station]));
      const teams = (ai.learned || []).filter((t) => t.design)
        .sort((a, b) => (onField.has(b.team) - onField.has(a.team)) || a.team - b.team);
      const lastChange = (d) => {
        const c = d.changes && d.changes[d.changes.length - 1];
        return c ? `${c.part}: ${c.from} → ${c.to} (${c.why})` : "";
      };
      replace(designCard,
        h("h3", "Robot designs", h("span.grow"),
          h("label.check", { title: "Teams pick their own parts (drive, shooter, hopper, intake, climber) and may rework one in the pits between matches. Off: every team gets a stock design from its team number." },
            h("input", { type: "checkbox", checked: ai.teamDesigns, onchange: (e) => run("world.ai.settings", { teamDesigns: e.target.checked }) }),
            "Teams build their own robots")),
        teams.length ? h("table.tbl.wide",
          h("tr", h("th", "Team"), h("th", "Robot"), h("th", "Why"), h("th", { title: "Matches since the last pit change" }, "Since"),
            h("th", "Last pit change"), h("th", { title: "Locked designs are never changed by the team" }, "Locked"), h("th", "")),
          teams.slice(0, 40).map((t) => h("tr",
            h("td.mono", onField.has(t.team) ? h("b", { style: { color: colorOf(onField.get(t.team)) } }, t.team) : t.team),
            h("td.small", archetypeLabel(t)),
            h("td.small.muted", t.design.why),
            h("td.small.mono", t.design.since),
            h("td.small", lastChange(t.design), t.design.changes && t.design.changes.length > 1 ?
              h("div.small.muted", `${t.design.changes.length} changes this event`) : null),
            h("td", h("input", { type: "checkbox", checked: t.design.locked,
              onchange: (e) => run("world.ai.lockDesign", { team: t.team, lock: e.target.checked }) })),
            h("td", h("button.btn.sm", { title: "Throw the robot out and run a new build season for this team",
              onclick: async () => (await confirmAction(`Rebuild ${t.team}'s robot?`, { message: "The team throws its robot out and runs a new build season, choosing its parts again.", okLabel: "Rebuild" })) && run("world.ai.redesign", { team: t.team }) }, "Rebuild"))))) :
          h("div.hint", ai.teamDesigns ? "Teams build their robots when they first arrive at the event." :
            "Every team plays a stock design picked from its team number."));
    }

    // A team's robot in words, from its parts.
    function archetypeLabel(t) {
      const b = t.design.build;
      if (!b || !b.drive) return "set by hand";
      const drive = { swerve: "Swerve", tank: "WCD tank", kop: "Kit chassis" }[b.drive] || b.drive;
      const shooter = { fixed: `${b.lanes}-lane shooter`, wide: "4-lane volley", turret: "turret", small: "small shooter", kitbot: "KitBot roller" }[b.shooter] || b.shooter;
      return [drive, b.height > 0.565 ? "tall" : "low", shooter, `${b.hopper} hopper`, b.climber ? `L${b.climber} climb` : "no climb",
        b.ballast ? `${b.ballast} kg ballast` : null, b.tune ? `tuned ×${b.tune}` : null].filter(Boolean).join(" · ");
    }

    on("world.ai", (ai) => {
      if (!ai) return;
      if (!busy(settingsCard)) {
        replace(settingsCard,
          h("h3", "AI drive teams"),
          h("div.row",
            h("label.small", "Difficulty "),
            h("select", { onchange: (e) => run("world.ai.settings", { difficulty: e.target.value }) },
              DIFFICULTIES.map(([v, l]) => h("option", { value: v, selected: ai.difficulty === v }, l))),
            h("label.check", { title: "Robots normally only know about FUEL their cameras and drivers can see" },
              h("input", { type: "checkbox", checked: ai.perfectInfo, onchange: (e) => run("world.ai.settings", { perfectInfo: e.target.checked }) }),
              "Perfect information"),
            h("label.check", { title: "A hard T-bone at speed can (rarely) tip a tall robot over. Off: robots never tip." },
              h("input", { type: "checkbox", checked: ai.tipping, onchange: (e) => run("world.ai.settings", { tipping: e.target.checked }) }),
              "Robots can tip over"),
            h("label.check", { title: "Planned paths, targets and defensive marks in the 3D view" },
              h("input", { type: "checkbox", checked: aiOverlayEnabled(), onchange: (e) => setAiOverlay(e.target.checked) }),
              "Show AI in 3D")),
          h("div.hint", `Drive teams' scoreboard: RED ${ai.scoreboard[0]} — BLUE ${ai.scoreboard[1]} (FUEL counted while scoring)`),
          h("div.row", { style: { marginTop: "6px" } },
            h("span.small", { title: "One seed drives a match's AI decisions, natural mistakes, field reset and chaos faults" },
              "Match seed ", h("b.mono", ai.seed ? String(ai.seed) : "—")),
            h("label.small", " Replay seed "),
            h("input", { type: "text", value: ai.replaySeed ? String(ai.replaySeed) : "", placeholder: "random",
              style: { width: "130px" }, title: "The next match loaded uses this seed (blank = random)",
              onchange: (e) => run("world.ai.settings", { replaySeed: Number(e.target.value) || 0 }) })),
          trainingRow());
      }

      replace(robotsCard,
        h("h3", "What each robot is doing"),
        h("table.tbl",
          h("tr", h("th", ""), h("th", "Job"), h("th", "Thinking"), h("th", "Options weighed"), h("th", "Timed shots"), h("th", "Endgame")),
          ai.robots.filter((r) => r.team).map((r) => h("tr",
            h("td", h("b", { style: { color: colorOf(r.station) } }, r.station), h("div.small.mono", r.team)),
            h("td.small", JOB_LABELS[r.job] || r.job || "—", r.mark ? h("div.small.muted", `marking ${r.mark}`) : null),
            h("td.small", r.intent || r.state, r.stillReason && r.stillReason !== "shooting" ? h("div.small.muted", `still: ${r.stillReason}`) : null,
              r.experiment ? h("div.small", { style: { color: "#c9a7ff" } }, `experiment: ${r.experiment}`) : null),
            h("td.small.mono", (r.options || []).map((o) => h("div", `${o.job} ${o.score.toFixed(2)}`))),
            h("td.small", {
              title: "Early: fired before the hub lit, timed to land just after. Late: counted in the 3 s grace window. % = counted of those that went in.",
            }, `early ${r.shots.early} (${pct(r.shots.earlyScored, r.shots.earlyIn)})`, h("br"),
            `late ${r.shots.late} (${pct(r.shots.lateScored, r.shots.lateIn)})`,
            Math.abs(r.clockError) > 0 ? h("div.small.muted", `clock ${r.clockError > 0 ? "+" : ""}${r.clockError.toFixed(2)} s`) : null),
            h("td.small", endgameText(r))))));

      if (!busy(designCard)) renderDesigns(ai);

      renderAutos(ai);

      replace(coachCard,
        h("h3", "Coaches' calls"),
        h("div", { style: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: "8px" } },
          [0, 1].map((alliance) => h("div",
            h("div.small", { style: { color: alliance === 0 ? "#ffb1b3" : "#b7d2ff" } }, alliance === 0 ? "Red" : "Blue"),
            (ai.coach[alliance] || []).slice(-8).reverse().map((call) => h("div.small",
              h("span.mono.muted", `${Math.floor(call.time)}s `), call.text)),
            !(ai.coach[alliance] || []).length ? h("div.small.muted", "No calls yet this match") : null))));

      if (!busy(humanCard)) {
        replace(humanCard,
          h("h3", "Human players"),
          h("table.tbl",
            h("tr", h("th", ""), h("th", "Mode"), h("th", "Accuracy"), h("th", "Throws"), h("th", "Corral")),
            [0, 1].map((alliance) => {
              const hp = ai.human[alliance];
              const name = alliance === 0 ? "red" : "blue";
              return h("tr",
                h("td", h("b", { style: { color: alliance === 0 ? "#ffb1b3" : "#b7d2ff" } }, alliance === 0 ? "Red" : "Blue")),
                h("td", h("select", { onchange: (e) => run("world.humanPlayer", { alliance: name, mode: e.target.value }) },
                  [["auto", "Decide"], ["feed", "Feed chute"], ["score", "Throw at hub"], ["idle", "Idle"]].map(([v, l]) =>
                    h("option", { value: v, selected: hp.mode === v }, l)))),
                h("td", h("input", {
                  type: "range", min: 0.4, max: 1, step: 0.05, value: hp.accuracy, style: { width: "90px" },
                  title: `${Math.round(hp.accuracy * 100)}% skill`,
                  onchange: (e) => run("world.humanPlayer", { alliance: name, accuracy: Number(e.target.value) }),
                }), h("span.small.mono", ` ${Math.round(hp.accuracy * 100)}`)),
                h("td.small", `${hp.made}/${hp.throws} in (${pct(hp.made, hp.throws)})`),
                h("td.small.mono", hp.corral));
            })),
          h("div.hint", "Robots drop FUEL through the outpost floor opening while their hub is off; the human player throws it in once it lights (timed like the robots' shots) or loads the chute for a teammate on the way."));
      }

      if (!busy(personalityCard)) renderPersonality(ai);

      replace(learnCard,
        h("h3", "What teams have learned", h("span.grow"),
          h("button.btn.sm", { onclick: async () => (await confirmAction("Reset what every team has learned?", { message: "Every team forgets its autonomous routines, shot ranges, habits and match history, and starts learning again from scratch. This can't be undone.", okLabel: "Reset learning", danger: true })) && run("world.ai.resetLearning") }, "Reset learning")),
        ai.learned && ai.learned.length ? h("table.tbl",
          h("tr", h("th", "Team"), h("th", "Matches"), h("th", "Accuracy"), h("th", "Climbs"), h("th", "Timed shots"),
            h("th", "Risk shift"), h("th", "Practice"), h("th", { title: "Build quality plus what the pit crew has fixed" }, "Reliability"),
            h("th", "Best so far"), h("th", "Remembers"), h("th", "")),
          ai.learned.slice(0, 40).map((t) => h("tr",
            h("td.mono", t.team), h("td.mono", t.matches), h("td.small", pct(t.scored, t.shots)),
            h("td.small", `${t.climbMade}/${t.climbTried}`), h("td.small", pct(t.timedScored, t.timedIn)),
            h("td.small.mono", (t.riskAdjust >= 0 ? "+" : "") + t.riskAdjust.toFixed(2)),
            h("td.small.mono", `+${Math.round(t.practice * 100)}%`),
            h("td.small.mono", `${Math.round(t.reliability * 100)}%`),
            h("td.small", bestChoices(t)),
            h("td.small", memories(t)),
            h("td", h("button.btn.sm", { title: "This team forgets its records, mistakes and auto routines",
              onclick: async () => (await confirmAction(`Make ${t.team} forget what it has learned?`, { message: "This team starts learning again from scratch. This can't be undone.", okLabel: "Forget", danger: true })) && run("world.ai.forgetTeam", { team: t.team }) }, "Forget"))))) :
          h("div.hint", "Teams learn from every completed match: bolder or more careful shot timing, driver practice, which game plans, autos, shot ranges and roles work, and the mistakes they made."));

      function renderPersonality() {
        const teams = ai.robots.filter((r) => r.team).map((r) => r.team);
        if (!editingTeam || !teams.includes(editingTeam)) editingTeam = teams[0] || 0;
        const robot = ai.robots.find((r) => r.team === editingTeam);
        if (!robot) {
          replace(personalityCard, h("h3", "Drive-team personality"), h("div.hint", "No teams on the field."));
          return;
        }
        const p = { ...robot.personality };
        replace(personalityCard,
          h("h3", "Drive-team personality", h("span.grow"),
            h("select", { onchange: (e) => { editingTeam = Number(e.target.value); renderPersonality(); } },
              ai.robots.filter((r) => r.team).map((r) => h("option", { value: r.team, selected: r.team === editingTeam }, `${r.station} ${r.team}`)))),
          h("table.tbl", TRAITS.map(([key, label, hint]) => h("tr",
            h("td.small", { title: hint }, label),
            h("td", h("input", {
              type: "range", min: key === "reaction" ? 0.05 : 0, max: key === "reaction" ? 0.6 : 1, step: 0.01, value: p[key],
              style: { width: "160px" },
              onchange: (e) => { p[key] = Number(e.target.value); run("world.ai.personality", { team: editingTeam, personality: p }); },
            })),
            h("td.small.mono", key === "reaction" ? `${p[key].toFixed(2)} s` : p[key].toFixed(2))))),
          h("div.btn-row", h("button.btn.sm", { onclick: () => run("world.ai.personality", { team: editingTeam, reset: true }) }, "Back to team's own")),
          h("div.hint", "Every team has its own drive team, kept between sessions. Values shown include the difficulty preset and what the team has learned."));
      }
    });

    // AUTO: the routine each robot runs (selected on its DS), how far along it is, chooser mistakes and queueing
    // agreements, the pose estimate's error, and every team's routine library with what it tuned.
    function renderAutos(ai) {
      const robots = ai.robots.filter((r) => r.team);
      const stepText = (r) => {
        const a = r.auto;
        if (!a) return h("span.muted", "—");
        if (!a.steps) return a.program;
        const done = a.step >= a.steps;
        return h("span", a.program, h("div.small.muted", done ? "finished" : `step ${a.step + 1}/${a.steps}: ${a.label || ""}`));
      };
      const resultText = (r) => {
        const results = (r.auto && r.auto.results) || [];
        if (!results.length) return h("span.muted", "—");
        return h("span.mono", results.map((s) => h("span", {
          title: `${s.label}: ${s.completed ? "done" : "cut off"} in ${s.seconds}s` +
            (s.kind === "path" ? `, ended ${s.endError} m off` : "") + (s.fired ? `, fired ${s.fired}` : "") +
            (s.collected ? `, picked up ${s.collected}` : "") + (s.contact ? `, hit a ${s.contact}` : ""),
          style: { color: !s.completed ? "#ff9b9b" : s.contact ? "#ffd27a" : s.endError > 0.35 ? "#ffd27a" : "#9be29b" },
        }, s.completed ? "●" : "○")));
      };
      const library = ai.autos || [];
      replace(autoCard,
        h("h3", "Autonomous routines"),
        h("table.tbl",
          h("tr", h("th", ""), h("th", "Selected on the DS"), h("th", "Steps"), h("th", "Pose estimate"), h("th", "Notes")),
          robots.map((r) => h("tr",
            h("td", h("b", { style: { color: colorOf(r.station) } }, r.station), h("div.small.mono", r.team)),
            h("td.small", stepText(r)),
            h("td.small", resultText(r)),
            h("td.small", {
              title: r.vision ? "Wheel odometry + AprilTag cameras" : "Wheel odometry only (drifts; no cameras to correct it)",
            }, `${(r.odoError * 100).toFixed(0)} cm off`, h("div.small.muted", r.vision ? `vision · ${r.tagsSeen} tags` : "no vision")),
            h("td.small", r.auto && r.auto.mistake ? h("div", { style: { color: "#ff9b9b" } }, r.auto.mistake) : null,
              r.auto && r.auto.note ? h("div.muted", r.auto.note) : null)))),
        library.length ? h("details", { open: libraryOpen, ontoggle: (e) => { libraryOpen = e.target.open; } },
          h("summary.small", `Routine libraries (${library.length} routines run by the teams on the field)`),
          h("table.tbl",
            h("tr", h("th", "Team"), h("th", "Routine"), h("th", "Rev"), h("th", "Runs"), h("th", "Avg pts"), h("th", "Latest change")),
            library.map((p) => h("tr",
              h("td.mono", p.team), h("td.small", p.name), h("td.mono", p.revision), h("td.mono", p.runs),
              h("td.mono", p.avgPoints.toFixed(1)),
              h("td.small.muted", (p.notes || []).slice(-1)[0] || "—"))))) :
          h("div.hint", "Each team writes its routines before the match and runs them blind on its pose estimate; after every match it reviews and tunes them."));
    }

    // A team's best-scoring option in each experiment category (with how often it has run it).
    function bestChoices(t) {
      const best = {};
      for (const record of t.records || []) {
        const [category, option] = record.choice.split(":");
        if (category === "climb" || record.runs < 1.5) continue;
        if (!best[category] || record.rate > best[category].rate) best[category] = { option, ...record };
      }
      const lines = Object.entries(best).map(([category, r]) =>
        h("div", { title: `${r.runs.toFixed(1)} runs, ${Math.round(r.rate * 100)}% success` }, `${CHOICE_LABELS[category] || category}: ${r.option}`));
      return lines.length ? lines : h("span.muted", "—");
    }

    // What a team remembers going wrong (strongest first) and how much care fouls have taught it.
    function memories(t) {
      const lines = (t.incidents || []).slice(0, 3).map((m) =>
        h("div", { title: m.x || m.y ? `at (${m.x}, ${m.y}) in the blue frame` : "" }, `${m.kind} ×${m.count.toFixed(1)}`));
      if (t.learnedDiscipline > 0) lines.push(h("div.muted", `+${t.learnedDiscipline.toFixed(2)} discipline`));
      return lines.length ? lines : h("span.muted", "—");
    }

    function endgameText(r) {
      if (r.climbPlan > 0) {
        const option = (r.climb || []).find((o) => o.level === r.climbPlan);
        return option ? `climb L${r.climbPlan} (~${Math.round(option.towerPoints + option.fuelPoints)} pts)` : `climb L${r.climbPlan}`;
      }
      if (r.climbPlan < 0) {
        const stay = (r.climb || [])[0];
        return stay ? `keep scoring (~${Math.round(stay.fuelPoints)} pts)` : "keep scoring";
      }
      return h("span.muted", "undecided");
    }
  },
});
