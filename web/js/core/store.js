// Client-side state store: mirrors bus topics from the simulator over /ws and sends commands.
//
// While a replay is active (replay.state.active), the player republishes each recorded topic as "replay.t.<topic>";
// the store hands those out in place of the live values, so every panel and 3D device replays without knowing it.

const REPLAY_PREFIX = "replay.t.";
const topics = new Map();
const replayTopics = new Map();
let replay = null; // latest replay.state
const topicListeners = new Map();
const eventListeners = new Set();
const connectionListeners = new Set();
const pending = new Map();
const events = [];
const MAX_EVENTS = 3000;

let socket = null;
let nextId = 1;
let connected = false;

export function connect() {
  const url = `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws`;
  socket = new WebSocket(url);
  socket.onopen = () => setConnected(true);
  socket.onclose = () => {
    setConnected(false);
    for (const [, p] of pending) p.reject(new Error("disconnected"));
    pending.clear();
    setTimeout(connect, 1000);
  };
  socket.onmessage = (message) => {
    let msg;
    try { msg = JSON.parse(message.data); } catch { return; }
    if (msg.type === "topic") {
      receiveTopic(msg.topic, msg.data);
    } else if (msg.type === "event") {
      events.push(msg.data);
      if (events.length > MAX_EVENTS) events.splice(0, events.length - MAX_EVENTS);
      for (const fn of eventListeners) fn(msg.data);
    } else if (msg.type === "reply") {
      const p = pending.get(msg.id);
      if (p) {
        pending.delete(msg.id);
        msg.ok ? p.resolve(msg.result) : p.reject(new Error(msg.error || "command failed"));
      }
    }
  };
}

function receiveTopic(name, data) {
  if (name.startsWith(REPLAY_PREFIX)) {
    const target = name.slice(REPLAY_PREFIX.length);
    if (data === null) replayTopics.delete(target);
    else replayTopics.set(target, data);
    if (replay?.active) notifyTopic(target, value(target));
    return;
  }
  const hidden = replay?.active && replayTopics.has(name);
  if (data === null) topics.delete(name); // the topic was removed on the simulator
  else topics.set(name, data);
  if (name === "replay.state") {
    const was = !!replay?.active;
    replay = data;
    notifyTopic(name, data);
    if (was !== !!replay?.active) renotifyAll();
    return;
  }
  if (!hidden) notifyTopic(name, data);
}

function renotifyAll() {
  for (const name of new Set([...topics.keys(), ...replayTopics.keys()])) notifyTopic(name, value(name));
}

/** The value panels see: the replayed one during a replay, otherwise the live one. */
function value(name) {
  return replay?.active && replayTopics.has(name) ? replayTopics.get(name) : topics.get(name);
}

/** The current replay state, or null when showing the live simulator. */
export function replayState() {
  return replay?.active ? replay : null;
}

/** How fast displayed time advances relative to wall time: 1 live, the playback speed in a replay (0 when paused). */
export function clockRate() {
  if (!replay?.active) return 1;
  return replay.playing ? replay.speed : 0;
}

function setConnected(value) {
  connected = value;
  for (const fn of connectionListeners) fn(value);
}

function notifyTopic(topic, data) {
  const listeners = topicListeners.get(topic);
  if (listeners) for (const fn of listeners) fn(data, topic);
  const wildcard = topicListeners.get("*");
  if (wildcard) for (const fn of wildcard) fn(data, topic);
}

/** Subscribe to a topic; the callback fires immediately if a value is cached. Returns an unsubscribe function. */
export function on(topic, fn, { immediate = true } = {}) {
  if (!topicListeners.has(topic)) topicListeners.set(topic, new Set());
  topicListeners.get(topic).add(fn);
  if (immediate && topic !== "*" && value(topic) !== undefined) fn(value(topic), topic);
  return () => topicListeners.get(topic)?.delete(fn);
}

/** Subscribe to several topics with one callback that receives all of their current values. */
export function onAll(names, fn) {
  const run = () => fn(...names.map((n) => value(n)));
  const offs = names.map((n) => on(n, run, { immediate: false }));
  run();
  return () => offs.forEach((off) => off());
}

export function get(topic) {
  return value(topic);
}

export function onEvent(fn) {
  eventListeners.add(fn);
  return () => eventListeners.delete(fn);
}

export function recentEvents() {
  return events;
}

export function onConnection(fn) {
  connectionListeners.add(fn);
  fn(connected);
  return () => connectionListeners.delete(fn);
}

export function isConnected() {
  return connected;
}

/** Send a command to the simulator; resolves with its result. */
export function cmd(name, args = {}) {
  return new Promise((resolve, reject) => {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      reject(new Error("not connected to simulator"));
      return;
    }
    const id = nextId++;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ type: "cmd", id, cmd: name, args }));
    setTimeout(() => {
      if (pending.has(id)) {
        pending.delete(id);
        reject(new Error(`${name} timed out`));
      }
    }, 15000);
  });
}
