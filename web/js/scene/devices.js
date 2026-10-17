// Live bindings: drives every physical field device in the 3D venue from simulator/arena state.
import * as THREE from "three";
import { on, get, cmd } from "../core/store.js";
import { clock } from "../core/match.js";
import { toast, fmtClock } from "../core/dom.js";
import { popOutMockDs } from "../ui/mockds-link.js";
import { openDisplay, SCREEN_DISPLAYS } from "../core/displays.js";
import { setupLiveScreens } from "./css3d.js";

const STATION_INDEX = { R1: 0, R2: 1, R3: 2, B1: 3, B2: 4, B3: 5 };
const SIGN_COLORS = { red: "#ff2a2a", blue: "#2f7bff", orange: "#ff8a00", green: "#21e06a", purple: "#b05cff", white: "#ffffff" };
const STACK_CSS = { orangered: 0xff4500, red: 0xff2020, blue: 0x2f7bff, black: 0x000000 };

export function bindDevices(venue) {
  const { field, env, robots } = venue;
  const blinkOn = () => Math.floor(performance.now() / 250) % 2 === 0;

  // ---- Hub DMX bars (sACN) -------------------------------------------------------------------------------------
  // New frames set targets; the shown colours ease toward them every rendered frame (smooth pulse/rainbow modes).
  on("sacn.hubs", (hubs) => {
    if (!hubs) return;
    for (const alliance of ["red", "blue"]) {
      if (hubs[alliance]) field.hubs[alliance]?.leds?.setTarget(hubs[alliance]);
    }
  });
  const chipColor = new THREE.Color();
  venue.addUpdater((dt) => {
    for (const alliance of ["red", "blue"]) field.hubs[alliance]?.leds?.update(dt, chipColor);
  });

  // ---- PLC coils: hub lights/motors, field stack light, field reset light --------------------------------------
  let coils = {};
  on("field.io", (io) => {
    if (!io) return;
    coils = Object.fromEntries(io.coilNames.map((name, i) => [name, io.coils[i]]));
    // E-stop / A-stop button caps.
    for (const [name, station] of Object.entries(field.stations)) {
      const index = STATION_INDEX[name];
      const eDown = io.teamEStop[index];
      const aDown = io.teamAStop[index];
      station.eStop.cap.position.y = station.eStop.restY - (eDown ? 0.018 : 0);
      station.eStop.cap.material.emissiveIntensity = eDown ? 0.6 : 0.05;
      station.aStop.cap.position.y = station.aStop.restY - (aDown ? 0.018 : 0);
      station.aStop.cap.material.color.setHex(aDown ? 0xf6a823 : 0x1b1d22);
      station.aStop.cap.material.emissive.setHex(aDown ? 0xf6a823 : 0x000000);
    }
    venue.fieldIo = io;
  });

  const lampOn = (mesh, onState, intensity = 4.5) => {
    mesh.material.emissiveIntensity = onState ? intensity : 0.03;
  };
  const buzzerOff = env.stackLight?.buzzer.material;
  const buzzerOn = new THREE.MeshStandardMaterial({ color: 0xffd34d, emissive: 0xffd34d, emissiveIntensity: 1 });
  let rollerAngle = 0;
  venue.addUpdater((dt) => {
    const stack = env.stackLight;
    if (stack) {
      lampOn(stack.lamps.red, coils.stackLightRed);
      lampOn(stack.lamps.blue, coils.stackLightBlue);
      lampOn(stack.lamps.orange, coils.stackLightOrange);
      lampOn(stack.lamps.green, coils.stackLightGreen);
      const any = coils.stackLightRed || coils.stackLightBlue || coils.stackLightOrange || coils.stackLightGreen;
      stack.glow.intensity = any ? 0.8 : 0;
      stack.glow.color.setHex(coils.stackLightGreen ? 0x35c47c : coils.stackLightOrange ? 0xf6a823
        : coils.stackLightRed ? 0xef4b4f : 0x3d8bff);
      stack.buzzer.material = coils.stackLightBuzzer && blinkOn() ? buzzerOn : buzzerOff;
    }
    if (env.resetLight) lampOn(env.resetLight, coils.fieldResetLight, 3);
    rollerAngle += dt * 18;
    for (const alliance of ["red", "blue"]) {
      const hub = field.hubs[alliance];
      if (!hub) continue;
      const lit = alliance === "red" ? coils.redHubLight : coils.blueHubLight;
      hub.glow.intensity = lit ? 6 : 0;
      hub.glowDisc.material.opacity = lit ? 0.55 : 0;
      const motor = alliance === "red" ? coils.redHubMotor : coils.blueHubMotor;
      for (const { roller } of hub.exits) if (motor) roller.rotation.y = rollerAngle;
    }
    // Network rack activity LEDs.
    if (env.rackLeds) {
      const t = performance.now();
      env.rackLeds.forEach((ledMesh, i) => {
        const active = (Math.sin(t / (60 + i * 13)) > 0.2);
        ledMesh.material.color.setHex(active ? 0x3dff7a : 0x0d260d);
      });
    }
  });

  // ---- Team LED stacks (arena's own stack light logic) ------------------------------------------------------
  let stackLights = null;
  on("arena.teamStackLights", (lights) => { stackLights = lights; });
  venue.addUpdater(() => {
    if (!stackLights) return;
    for (const [name, station] of Object.entries(field.stations)) {
      const alliance = name[0] === "R" ? "red" : "blue";
      const light = stackLights[alliance]?.[Number(name[1]) - 1];
      if (!light) continue;
      const [stop, robot] = light.lightStates;
      const robotOn = robot.color !== "black" && (!robot.blink || blinkOn());
      const stopOn = stop.color !== "black" && (!stop.blink || blinkOn());
      station.lamps[0].material.emissiveIntensity = robotOn ? 4.5 : 0.04;
      station.lamps[1].material.emissiveIntensity = robotOn ? 4.5 : 0.04;
      station.lamps[2].material.emissiveIntensity = stopOn ? 4.8 : 0.04;
    }
  });

  // ---- Team signs and DS2 timers (mirrors the arena's Cypress sign logic) -----------------------------------
  venue.addUpdater(() => updateSigns(venue, coils, blinkOn()));

  // ---- DS laptop screens ------------------------------------------------------------------------------------
  let dsStates = [];
  on("ds.stations", (stations) => {
    dsStates = stations || [];
    for (const s of dsStates) {
      const robot = robots[s.station];
      if (robot) {
        robot.setTeam(s.team);
        robot.enabled = s.enabled;
      }
    }
  });
  let lastScreenDraw = 0;
  venue.addUpdater((dt, now) => {
    if (now - lastScreenDraw < 250) return;
    lastScreenDraw = now;
    for (const s of dsStates) {
      const station = field.stations[s.station];
      if (station) drawDsScreen(station.screenPanel, s);
    }
  });

  // ---- Robot signal lights ----------------------------------------------------------------------------------
  venue.addUpdater((dt, now) => {
    // The roboRIO blinks the RSL at about 2.5 Hz while enabled.
    const flash = Math.floor(now / 200) % 2 === 0;
    for (const robot of Object.values(robots)) {
      const lit = robot.enabled ? flash : true;
      // A brownout makes the RSL flicker as the voltage sags.
      const sag = robot.flags && robot.flags.brownout ? 0.25 + Math.random() * 0.75 : 1;
      robot.parts.rsl.material.emissiveIntensity = lit ? 4.0 * sag : 0.05;

    }
  });

  // ---- Clicks -----------------------------------------------------------------------------------------------
  const { picking } = venue;
  const toggleStop = (control) => (info) => {
    const io = venue.fieldIo;
    if (!io) return;
    const index = STATION_INDEX[info.station];
    const current = control === "teamEStop" ? io.teamEStop[index] : io.teamAStop[index];
    cmd("field.set", { control, index, value: !current }).catch((e) => toast(e.message, "error"));
  };
  picking.on("estop", toggleStop("teamEStop"));
  picking.on("astop", toggleStop("teamAStop"));
  picking.on("dsLaptop", (info) => popOutMockDs(info.station));
  picking.on("screen", (info) => {
    const display = SCREEN_DISPLAYS[info.screenId];
    if (display) openDisplay(display);
  });
  picking.on("robot", (info, result) => {
    let object = result.object;
    while (object && !object.name.startsWith("robot-")) object = object.parent;
    if (object) venue.cameras.followRobot(object, info.label);
  });
  picking.on("stackLight", () => toast("Field stack light — red/blue: alliance not ready · orange: scores pending · green: ready/running"));

  setupLiveScreens(venue);
}

// ---------------------------------------------------------------------------------------------------------------

function drawDsScreen(panel, s) {
  const key = [s.team, s.modeText, s.enabled, s.robotLinked, s.codeRunning, s.fmsLinked, s.gameData,
    (s.robot.voltage || 0).toFixed(1), s.control.remainingSeconds].join("|");
  if (panel.last === key) return;
  panel.last = key;
  const { g, canvas: c } = panel;
  const red = s.station[0] === "R";
  g.fillStyle = "#1f1f1f";
  g.fillRect(0, 0, c.width, c.height);
  g.fillStyle = red ? "#e5484d" : "#3d8bff";
  g.fillRect(0, 0, c.width, 8);
  g.fillStyle = "#aaa";
  g.font = "20px 'Segoe UI'";
  g.textAlign = "left";
  g.fillText("Team #", 20, 42);
  g.fillStyle = "#fff";
  g.font = "bold 56px 'Segoe UI'";
  g.fillText(s.team || "—", 20, 96);
  g.fillStyle = "#aaa";
  g.font = "20px 'Segoe UI'";
  g.fillText("Battery", 20, 130);
  g.fillStyle = s.robotLinked ? "#fff" : "#666";
  g.font = "bold 40px Consolas";
  g.fillText(s.robotLinked ? (s.robot.voltage || 0).toFixed(2) : "--.--", 20, 172);
  const indicator = (y, label, ok) => {
    g.fillStyle = ok ? "#2fb35a" : "#b52a2a";
    g.fillRect(250, y - 16, 46, 18);
    g.fillStyle = "#ddd";
    g.font = "20px 'Segoe UI'";
    g.fillText(label, 306, y);
  };
  indicator(48, "Communications", s.robotLinked);
  indicator(78, "Robot Code", s.codeRunning);
  indicator(108, s.fmsLinked ? "FMS Connected" : "No FMS", s.fmsLinked);
  g.font = "bold 34px 'Segoe UI'";
  g.fillStyle = s.enabled ? "#6ee08f" : /Stop|No Robot|Brownout/.test(s.modeText) ? "#ff6b6b" : "#fff";
  g.fillText(s.modeText, 20, 232, c.width - 40);
  g.font = "22px 'Segoe UI'";
  g.fillStyle = "#bbb";
  g.fillText(`Game data: ${s.gameData || "—"}    Match time: ${s.fmsLinked ? s.control.remainingSeconds : "—"}`, 20, 280);
  panel.texture.needsUpdate = true;
}

function updateSigns(venue, coils, blink) {
  const status = get("arena.arenaStatus");
  if (!status?.AllianceStations) return;
  const asMode = get("arena.allianceStationDisplayMode") || "match";
  const score = get("arena.realtimeScore");
  const c = clock();
  const state = c.state;
  const countdown = fmtClock(c.remaining);
  const inMatch = state >= 1 && state <= 4;
  const fieldReset = !!coils.fieldResetLight;
  // Purple field-cleanup signal (the arena keeps it on through commit until green or the next match starts).
  const leds = get("arena.ledModes");
  const volunteers = !fieldReset && leds?.RedMode === 4 && leds?.BlueMode === 4;

  for (const [name, station] of Object.entries(venue.field.stations)) {
    const alliance = name[0] === "R" ? "red" : "blue";
    const s = status.AllianceStations[name] || {};
    const ds = s.DsConn;
    let front = "";
    let color = SIGN_COLORS.white;
    let rear = "";
    if (asMode === "blank") {
      front = "";
    } else if (asMode === "logo") {
      front = "2026";
      color = SIGN_COLORS[alliance];
    } else if (!s.Team) {
      rear = "No Team Assigned";
    } else {
      front = String(s.Team.Id);
      const stationReady = !s.EStop && (s.Bypass || ds?.RobotLinked);
      if (s.EStop) color = SIGN_COLORS.orange;
      else if (s.AStop && state === 2) color = blink ? SIGN_COLORS.orange : "#000000";
      else if (state === 0 && stationReady) color = SIGN_COLORS[alliance];
      else if (fieldReset) color = SIGN_COLORS.green;
      else if (volunteers) color = SIGN_COLORS.purple;
      else if (ds && !ds.RobotLinked && inMatch) color = blink ? SIGN_COLORS[alliance] : "#000000";
      else color = SIGN_COLORS[alliance];

      let message = "";
      if (s.EStop) message = "E-STOP";
      else if (s.AStop && state === 2) message = "A-STOP";
      else if (state === 0 || state === 6) {
        if (s.Bypass) message = "Bypassed";
        else if (!s.Ethernet) message = "Connect PC";
        else if (!ds) message = "Start DS";
        else if (ds.WrongStation) message = "Move Station";
        else if (!ds.RadioLinked) message = "No Radio";
        else if (!ds.RioLinked) message = "No Rio";
        else if (!ds.RobotLinked) message = "No Code";
        else message = "Ready";
      }
      if (message) {
        rear = `${s.Team.Id}  ${message}`;
      } else if (score) {
        const key = alliance === "red" ? "Red" : "Blue";
        const summary = score[key]?.ScoreSummary || {};
        const shift = (c.shift || "").replace("Shift ", "S").replace("Transition", "T").replace("End Game", "E").replace("Auto", "A");
        rear = `${shift} ${summary.NumFuel ?? 0} fuel  ${countdown}`;
      }
    }
    station.front.text(front, { fg: color, bg: "#050505", font: "bold 130px Consolas, monospace" });
    station.rear.text(rear, { fg: "#ffcc33", bg: "#050505", font: "bold 54px Consolas, monospace" });
  }

  for (const alliance of ["red", "blue"]) {
    const timer = venue.field.timers[alliance];
    if (!timer) continue;
    let front = countdown;
    let color = SIGN_COLORS.white;
    if (asMode === "blank") front = "";
    else if (asMode === "logo") front = "2026";
    else if (fieldReset && state !== 6) { front = "SAFE"; color = SIGN_COLORS.green; }
    else if (volunteers && state !== 6) { front = "CLEAn"; color = SIGN_COLORS.purple; }
    let rear = countdown;
    if (score) {
      const mine = alliance === "red" ? score.Red : score.Blue;
      const theirs = alliance === "red" ? score.Blue : score.Red;
      const a = alliance === "red" ? "R" : "B";
      const b = alliance === "red" ? "B" : "R";
      rear = `${countdown}  ${a}${String(mine?.ScoreSummary?.Score ?? 0).padStart(3, "0")}-${b}${String(theirs?.ScoreSummary?.Score ?? 0).padStart(3, "0")}`;
    }
    timer.front.text(front, { fg: color, bg: "#050505", font: "bold 130px Consolas, monospace" });
    timer.rear.text(rear, { fg: "#ffcc33", bg: "#050505", font: "bold 50px Consolas, monospace" });
  }
}
