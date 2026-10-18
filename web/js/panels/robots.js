// Robots & field panel: alliance game plans, per-robot strategy/auto/start overrides, designs, skill, manual driving,
// live robot status, field reset, human players and chutes.
import { h, replace, led, tag, toast, cmdButton } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";
import { icon } from "../core/icons.js";

const ARCHETYPES = [
  ["scorer", "Low shooter (trench)"], ["tall", "Tall shooter"], ["turret", "Turret shooter"], ["volley", "Wide volley shooter (4 lanes)"],
  ["bigdumper", "Big dumper (4-wide)"], ["shuttle", "Shuttle bot"], ["kitbot", "Kitbot"], ["defender", "Defender"],
];
const STRATEGIES = [["scorer", "Scorer"], ["shuttle", "Shuttle"], ["reverse", "Reverse shuttle"], ["defense", "Defense"], ["feeder", "Outpost feeder"], ["steal", "Ball stealer"]];
const INACTIVE = [["hold", "Fill & hold"], ["defend", "Play defense"], ["steal", "Steal FUEL"], ["shuttle", "Stockpile (shuttle)"]];
const AUTOS = [["preload", "Preload only"], ["center", "Center rush"], ["depot", "Depot run"], ["outpost", "Outpost run"], ["climb", "Preload + L1 climb"], ["shuttle", "Shuttle rush"]];
const STARTS = [["outpost-trench", "Outpost-side trench"], ["outpost-bump", "Outpost-side bump"], ["hub", "Behind hub"], ["depot-bump", "Depot-side bump"], ["depot-trench", "Depot-side trench"]];

const run = (command, args) => cmd(command, args).catch((e) => toast(e.message, "error"));

function planSelect(options, override, current, onPick) {
  return h("select", { onchange: (e) => onPick(e.target.value), title: override ? "Overridden" : "Chosen by the alliance game plan" },
    h("option", { value: "", selected: !override }, `plan: ${(options.find(([v]) => v === current) || ["", current || "—"])[1]}`),
    options.map(([value, label]) => h("option", { value, selected: override === value }, label)));
}

registerPanel({
  id: "robots",
  title: "Robots",
  mount(el) {
    const fieldCard = h("div.card");
    const planCard = h("div.card");
    const robotsCard = h("div.card");
    const outpostCard = h("div.card");
    el.append(fieldCard, planCard, robotsCard, outpostCard);
    const busy = (card) => card.contains(document.activeElement) && /SELECT|INPUT/.test(document.activeElement.tagName);
    const colorOf = (station) => (station[0] === "R" ? "#ffb1b3" : "#b7d2ff");

    on("world.state", (w) => {
      if (!w) return;
      replace(fieldCard,
        h("h3", "Field", h("span.grow"), w.paused ? tag("PAUSED", "warn") : tag(`t=${w.time.toFixed(0)}s`)),
        h("div.btn-row",
          cmdButton("Field reset (restage FUEL, new game plans)", "world.reset", {}, "primary"),
          h("button.btn", { onclick: () => run("world.pause", { paused: !w.paused }) }, icon(w.paused ? "play" : "pause"), w.paused ? "Resume physics" : "Pause physics"),
        ),
        h("dl.kv",
          h("dt", "FUEL"), h("dd", Object.entries(w.fuel).map(([k, v]) => `${k} ${v}`).join(" · ")),
          h("dt", "Red hub"), h("dd", `stored ${w.hubs[0].stored} · sensors ${w.hubs[0].sensed.join("/")} · motor ${w.hubs[0].motorOn ? "ON" : "off"} · light ${w.hubs[0].lightOn ? "ON" : "off"}`),
          h("dt", "Blue hub"), h("dd", `stored ${w.hubs[1].stored} · sensors ${w.hubs[1].sensed.join("/")} · motor ${w.hubs[1].motorOn ? "ON" : "off"} · light ${w.hubs[1].lightOn ? "ON" : "off"}`),
          h("dt", "Shots"), h("dd", `red ${w.stats.shots[0]} (${w.stats.entered[0]} in) · blue ${w.stats.shots[1]} (${w.stats.entered[1]} in) · out of field ${w.stats.outOfField}`),
        ));

      if (!busy(planCard)) {
        replace(planCard,
          h("h3", "Game plans", h("span.grow"),
            tag(`Red: ${w.plans?.[0] || "—"}`, "red"), tag(`Blue: ${w.plans?.[1] || "—"}`, "blue")),
          h("table.tbl",
            h("tr", h("th", "Stn"), h("th", "Design"), h("th", "Teleop strategy"), h("th", { title: "What the robot does while its own hub is inactive" }, "Hub inactive"), h("th", "Auto"), h("th", "Start")),
            w.robots.map((r) => h("tr",
              h("td.mono", { style: { color: colorOf(r.station) } }, r.station),
              h("td", h("select", { onchange: (e) => run("world.robot", { station: r.station, archetype: e.target.value }),
                title: r.model?.label || "Picking a stock design locks it; \"Team's own build\" lets the team design its robot" },
                r.archetype?.startsWith("build:") ? h("option", { value: r.archetype, selected: true }, `Team-built (${r.model?.capacity ?? "?"} FUEL)`) : null,
                h("option", { value: "team" }, "Team's own build…"),
                ARCHETYPES.map(([value, label]) => h("option", { value, selected: value === r.archetype }, label)))),
              h("td", planSelect(STRATEGIES, r.override.strategy, r.plan.strategy,
                (value) => run("world.plan", { station: r.station, strategy: value }))),
              h("td", planSelect(INACTIVE, r.override.inactive, r.plan.inactive,
                (value) => run("world.plan", { station: r.station, inactive: value }))),
              h("td", planSelect(AUTOS, r.override.auto, r.plan.auto,
                (value) => run("world.plan", { station: r.station, auto: value }))),
              h("td", planSelect(STARTS, r.override.start, r.plan.start,
                (value) => run("world.plan", { station: r.station, start: value, apply: true }))),
            ))),
          h("div.hint", { style: { marginTop: "6px" } },
            "Each field reset picks a coordinated plan per alliance (Balanced, Triple scorer, Shuttle line, Lockdown defense, ",
            "Deny, Outpost feed, Double shuttle, Raiders), gives every robot a job for when its hub is inactive, and puts ",
            "robots on starting spots that suit their auto. Overrides stick until cleared."));
      }

      if (!busy(robotsCard)) {
        replace(robotsCard, h("h3", "Robots"),
          h("table.tbl",
            h("tr", h("th", "Stn"), h("th", "Team"), h("th", "Skill"), h("th", "State"), h("th", "Fuel"), h("th", "Scored"),
              h("th", "Climb"), h("th", "Issues"), h("th", "")),
            w.robots.map((r) => h("tr", { style: { opacity: r.present ? 1 : 0.45 } },
              h("td.mono", { style: { color: colorOf(r.station) } }, r.station),
              h("td.mono", r.team || "—"),
              h("td", h("input", {
                type: "range", min: 0.4, max: 1.6, step: 0.1, value: r.skill, style: { width: "60px" },
                title: `Skill ${r.skill.toFixed(1)}`,
                onchange: (e) => run("world.robot", { station: r.station, skill: Number(e.target.value) }),
              })),
              h("td.small", led(r.enabled ? "green" : ""), " ", r.manual ? "manual" : (r.aiState || "—")),
              h("td.mono", `${r.hopper}/${r.capacity}`),
              h("td.mono", { title: `${r.shots} shots, ${r.passes} passes` }, `${r.scored}${r.passes ? ` +${r.passes}p` : ""}`),
              h("td.mono", r.climbing ? `↑${r.climbLevel}` : r.climbLevel ? `L${r.climbLevel}` : "—"),
              h("td.small", { style: { color: "var(--amber)" } }, (r.faults || []).join(", ")),
              h("td.row",
                h("button.btn.sm", { class: r.manual ? "on" : "", title: "Drive this robot with keyboard/gamepad",
                  "aria-label": "Drive", onclick: () => window.venue?.drive?.start(r.station) }, icon("gamepad")),
                r.manual ? h("button.btn.sm", { title: "Hand back to the AI driver",
                  onclick: () => run("world.robot", { station: r.station, manual: false }) }, "AI") : null,
                h("button.btn.sm", { title: "Follow camera", "aria-label": "Follow camera", onclick: () => {
                  const robot = window.venue?.robots?.[r.station];
                  if (robot) window.venue.cameras.followRobot(robot.root, r.station);
                } }, icon("video"))),
            ))),
          h("div.hint", { style: { marginTop: "6px" } },
            "AI drivers only use what a real robot knows: DS enable/auto, FMS match time and game data, and the hub lights."));
      }

      if (!busy(outpostCard)) {
        replace(outpostCard, h("h3", "Outposts & human players"),
          h("table.tbl",
            h("tr", h("th", ""), h("th", "Chute"), h("th", "Corral"), h("th", "Door"), h("th", "Human player")),
            [["red", 0], ["blue", 1]].map(([alliance, i]) => {
              const o = w.outposts[i];
              return h("tr",
                h("td", { style: { color: alliance === "red" ? "#ffb1b3" : "#b7d2ff" } }, alliance),
                h("td.mono", o.chute), h("td.mono", o.corral),
                h("td", h("button.btn.sm", { class: o.doorOpen ? "warn" : "",
                  onclick: () => run("world.chute", { alliance, open: !o.doorOpen }) }, o.doorOpen ? "Open" : "Closed")),
                h("td", h("select", { onchange: (e) => run("world.humanPlayer", { alliance, mode: e.target.value }) },
                  ["auto", "feed", "score", "idle"].map((mode) => h("option", { value: mode, selected: mode === o.humanMode }, mode)))),
              );
            })));
      }
    });
  },
});
