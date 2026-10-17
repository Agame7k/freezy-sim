// Field I/O panel: physical E-stops/A-stops/FTA switch, stack light, hub sensors, PLC/alt-IO transport and raw I/O.
import { h, replace, led, tag, toast, cmdButton, STATIONS } from "../core/dom.js";
import { on, cmd, get } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

const run = (command, args) => cmd(command, args).catch((e) => toast(e.message, "error"));

function stackLight(coils) {
  const lamp = (color, on, label) => h("div", {
    title: label,
    style: {
      width: "34px", height: "22px", borderRadius: "4px", border: "1px solid #0009",
      background: on ? color : "#1c2028", boxShadow: on ? `0 0 14px ${color}` : "inset 0 0 6px #000",
    },
  });
  return h("div", { style: { display: "flex", flexDirection: "column", gap: "3px", alignItems: "center" } },
    lamp("#ef4b4f", coils.stackLightRed, "Red: red alliance not ready"),
    lamp("#3d8bff", coils.stackLightBlue, "Blue: blue alliance not ready"),
    lamp("#f6a823", coils.stackLightOrange, "Orange: scores not committed"),
    lamp("#35c47c", coils.stackLightGreen, "Green: field ready / match running"),
    h("div", { style: { width: "6px", height: "26px", background: "#444" } }),
    h("div.small", { style: { color: coils.stackLightBuzzer ? "var(--amber)" : "var(--faint)" } },
      coils.stackLightBuzzer ? "🔔 buzzer" : "buzzer"));
}

registerPanel({
  id: "fieldio",
  title: "Field I/O",
  mount(el, { setBadge }) {
    const profileSelect = h("select",
      h("option", { value: "altio" }, "Freezy alternate IO (ESP32 boxes)"),
      h("option", { value: "plc" }, "Modbus PLC"),
      h("option", { value: "minimal" }, "Minimal (driver stations only)"));
    profileSelect.addEventListener("change", async () => {
      await run("field.profile", { profile: profileSelect.value });
      await run("arena.configure");
    });

    const profileCard = h("div.card",
      h("h3", "Field I/O profile"),
      h("div.row", profileSelect, cmdButton("Apply to arena", "arena.configure", {}, "primary")),
      h("div.hint", { style: { marginTop: "6px" } },
        "Alternate IO: E-stops via emulated ESP32 boxes, hub counts via the arena PLC websocket. ",
        "Modbus PLC: the arena polls the emulated PLC on 127.0.0.1:502 every 100 ms."));

    const controlsCard = h("div.card");
    const lightsCard = h("div.card");
    const hubsCard = h("div.card");
    const transportCard = h("div.card");
    const ioCard = h("div.card");
    el.append(profileCard, controlsCard, lightsCard, hubsCard, transportCard, ioCard);

    on("field.transport", (t) => {
      if (!t) return;
      if (document.activeElement !== profileSelect) profileSelect.value = t.profile;
      if (t.profile === "plc") {
        const m = t.modbus;
        replace(transportCard, h("h3", "Modbus PLC emulator", h("span.grow"),
          led(m.listening && m.heartbeatOk ? "green" : m.listening ? "amber" : "red")),
        h("dl.kv",
          h("dt", "Address"), h("dd", m.address + (m.listening ? "" : " (not listening)")),
          h("dt", "Arena client"), h("dd", (m.clients || []).join(", ") || "none"),
          h("dt", "Poll rate"), h("dd", `${m.pollHz.toFixed(1)} Hz · ${m.requests} requests · ${m.coilWrites} coil writes`),
          h("dt", "Heartbeat"), h("dd", m.heartbeatOk ? "OK" : "LOST — outputs forced off"),
          m.error ? [h("dt", "Error"), h("dd", { style: { color: "var(--red)" } }, m.error)] : null),
        h("div.btn-row",
          h("button.btn.sm", { class: m.offline ? "off" : "", onclick: () => run("field.plcOffline", { offline: !m.offline }) },
            m.offline ? "PLC offline (click to restore)" : "Take PLC offline"),
          h("button.btn.sm", { onclick: () => run("field.plcLag", { ms: m.responseLagMs ? 0 : 1500 }) },
            m.responseLagMs ? `Lag ${m.responseLagMs}ms (clear)` : "Inject 1.5 s lag"),
        ));
      } else if (t.profile === "altio") {
        replace(transportCard, h("h3", "ESP32 E-stop boxes"),
          h("table.tbl",
            h("tr", h("th", ""), h("th", "Box"), h("th", "Address"), h("th", "Posts"), h("th", "Probes"), h("th", "")),
            t.boxes.map((b) => h("tr",
              h("td", led(b.listening && !b.postError ? "green" : b.online ? "amber" : "red")),
              h("td", b.name),
              h("td.mono", b.address),
              h("td.mono", b.posts),
              h("td.mono", { title: "Arena health-check connections" }, b.probes),
              h("td", h("button.btn.sm", { onclick: () => run("field.box", { name: b.name, online: !b.online }) },
                b.online ? "Unplug" : "Plug in")),
            ))),
          t.boxes.some((b) => b.bindError || b.postError) ? h("div.hint", { style: { color: "var(--amber)" } },
            t.boxes.map((b) => b.bindError || b.postError).filter(Boolean).join(" · ")) : null,
          h("div.small.muted", { style: { marginTop: "6px" } },
            `PLC websocket: ${t.altRegistersSent} register syncs, ${t.altInputsSent} input syncs`,
            t.altError ? ` · error: ${t.altError}` : ""),
          h("label.check.small", { style: { marginTop: "4px" } },
            h("input", { type: "checkbox", checked: t.altSendSensors,
              onchange: (e) => run("field.altSensors", { enabled: e.target.checked }) }),
            "Also stream hub beam-break inputs"));
      } else {
        replace(transportCard, h("h3", "Transport"), h("div.hint", "No field I/O is connected in the minimal profile."));
      }
      const rates = t.feederRate || [0, 0];
      hubsCard.dataset.rates = JSON.stringify(rates);
    });

    on("field.io", (io) => {
      if (!io) return;
      const coils = Object.fromEntries(io.coilNames.map((n, i) => [n, io.coils[i]]));
      const pressedCount = io.teamEStop.filter(Boolean).length + io.teamAStop.filter(Boolean).length + (io.fieldEStop ? 1 : 0);
      setBadge(pressedCount ? String(pressedCount) : "");

      replace(controlsCard,
        h("h3", "Physical controls"),
        h("div.row", { style: { gap: "12px", marginBottom: "8px" } },
          h("button.btn", {
            style: { background: io.fieldEStop ? "#b3191f" : "#4a1416", borderColor: "#ff4d4f", fontWeight: 700, padding: "10px 14px" },
            onclick: () => run("field.set", { control: "fieldEStop", index: 0, value: !io.fieldEStop }),
          }, io.fieldEStop ? "FIELD E-STOP ACTIVE (release)" : "FIELD E-STOP"),
          h("button.btn", { class: io.ftaReady ? "on" : "",
            onclick: () => run("field.set", { control: "ftaReady", index: 0, value: !io.ftaReady }) },
          io.ftaReady ? "FTA ready ✓" : "FTA ready switch"),
        ),
        h("table.tbl",
          h("tr", h("th", "Station"), h("th", "E-stop"), h("th", "A-stop"), h("th", "Ethernet")),
          STATIONS.map((s, i) => h("tr",
            h("td.mono", { style: { color: s[0] === "R" ? "#ffb1b3" : "#b7d2ff" } }, s),
            h("td", h("button.btn.sm", { class: io.teamEStop[i] ? "off" : "",
              onclick: () => run("field.set", { control: "teamEStop", index: i, value: !io.teamEStop[i] }) },
            io.teamEStop[i] ? "PRESSED" : "E-stop")),
            h("td.row",
              h("button.btn.sm", { class: io.teamAStop[i] ? "warn" : "",
                onclick: () => run("field.set", { control: "teamAStop", index: i, value: !io.teamAStop[i] }) },
              io.teamAStop[i] ? "PRESSED" : "A-stop"),
              h("button.btn.sm.ghost", { title: "Tap (press for 0.8 s)",
                onclick: () => run("field.pulse", { control: "teamAStop", index: i, ms: 800 }) }, "tap")),
            h("td", led(io.inputs[13 + i] ? "green" : "red")),
          ))),
        h("div.small.muted", { style: { marginTop: "6px" } },
          "ArmorBlocks: ", ["redDs", "blueDs", "redIoLink", "blueIoLink"].map((name, i) =>
            h("button.btn.sm", { class: io.armorBlocks[i] ? "on" : "off", style: { marginLeft: "4px" },
              onclick: () => run("field.set", { control: "armorBlock", index: i, value: !io.armorBlocks[i] }) }, name))),
      );

      replace(lightsCard,
        h("h3", "Field outputs", h("span.grow"), tag(`coils via ${io.coilSource || "—"}`),
          io.heartbeatOk ? null : tag("HEARTBEAT LOST", "bad")),
        h("div.row", { style: { alignItems: "flex-start", gap: "18px" } },
          stackLight(coils),
          h("div", { style: { flex: 1 } },
            h("dl.kv",
              h("dt", "Red hub"), h("dd.row", led(coils.redHubLight ? "red" : ""), "light ",
                led(coils.redHubMotor ? "green" : ""), " motors"),
              h("dt", "Blue hub"), h("dd.row", led(coils.blueHubLight ? "blue" : ""), "light ",
                led(coils.blueHubMotor ? "green" : ""), " motors"),
              h("dt", "Field reset"), h("dd.row", led(coils.fieldResetLight ? "green" : ""), coils.fieldResetLight ? "SAFE TO ENTER" : "off"),
              h("dt", "Awards mode"), h("dd.row", led(coils.awardsModeLight ? "white" : ""), coils.awardsModeLight ? "on" : "off"),
              h("dt", "Winner lights"), h("dd.row", led(coils.redWins ? "red" : ""), led(coils.blueWins ? "blue" : ""),
                io.winnerShow ? `PLC show: ${io.winnerShow.red && io.winnerShow.blue ? "TIE" : io.winnerShow.red ? "RED" : "BLUE"} `
                  + (io.winnerShow.elapsedMs < io.winnerShow.revealMs
                    ? `· reveal in ${((io.winnerShow.revealMs - io.winnerShow.elapsedMs) / 1000).toFixed(1)} s (delay-on)`
                    : `· ${Math.round((io.winnerShow.durationMs - io.winnerShow.elapsedMs) / 1000)} s left`)
                  : "off"),
              h("dt", "Match reset"), h("dd", `${coils.matchReset ? "PULSE" : "—"} · ${io.resetCount} resets seen`),
              h("dt", "Heartbeat"), h("dd", coils.heartbeat ? "on" : "off"),
            ))));

      const rates = JSON.parse(hubsCard.dataset.rates || "[0,0]");
      replace(hubsCard,
        h("h3", "Hub fuel sensors"),
        ["Red", "Blue"].map((name, alliance) => h("div", { style: { marginBottom: "8px" } },
          h("div.row",
            h("b", { style: { width: "40px", color: alliance ? "#b7d2ff" : "#ffb1b3" } }, name),
            h("span.mono", `total ${io.registers[1 + alliance]}`),
            h("span.grow"),
            [0, 1, 2, 3].map((sensor) => h("button.btn.sm.mono", {
              class: io.inputs[20 + alliance * 4 + sensor] ? "on" : "",
              title: `Sensor ${sensor + 1}: +1 fuel`,
              onclick: () => run("field.fuel", { alliance, sensor, count: 1 }),
            }, `S${sensor + 1}:${io.hubCounts[alliance][sensor]}`))),
          h("div.row.small", h("span.muted", { style: { width: "92px" } }, "Test feeder"),
            h("input", { type: "range", min: 0, max: 10, step: 0.5, value: rates[alliance], style: { flex: 1 },
              onchange: (e) => run("field.feeder", { alliance, rate: Number(e.target.value) }) }),
            h("span.mono", `${rates[alliance]}/s`)))),
        h("div.btn-row", cmdButton("Zero counters", "field.resetHubs", {}, "warn")));

      const arenaIo = get("arena.plcIo") || {};
      replace(ioCard,
        h("h3", "Raw I/O (sim → arena view)"),
        h("details", h("summary.small.muted", `Inputs (${io.inputs.length})`),
          h("table.tbl", h("tr", h("th", "#"), h("th", "Input"), h("th", "Sim"), h("th", "Arena")),
            io.inputNames.map((n, i) => h("tr", h("td.mono", i), h("td", n), h("td", led(io.inputs[i] ? "green" : "")),
              h("td", arenaIo.Inputs ? led(arenaIo.Inputs[i] ? "green" : "") : "—"))))),
        h("details", h("summary.small.muted", `Registers (${io.registers.length})`),
          h("table.tbl", h("tr", h("th", "#"), h("th", "Register"), h("th", "Sim"), h("th", "Arena")),
            io.registerNames.map((n, i) => h("tr", h("td.mono", i), h("td", n), h("td.mono", io.registers[i]),
              h("td.mono", arenaIo.Registers ? arenaIo.Registers[i] : "—"))))),
        h("details", h("summary.small.muted", `Coils (${io.coils.length})`),
          h("table.tbl", h("tr", h("th", "#"), h("th", "Coil"), h("th", "Value"), h("th", "Arena override")),
            io.coilNames.map((n, i) => h("tr", h("td.mono", i), h("td", n), h("td", led(io.coils[i] ? "green" : "")),
              h("td.mono", arenaIo.CoilOverrides ? arenaIo.CoilOverrides[i] : "—"))))));
    });
  },
});
