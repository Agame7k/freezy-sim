// Opens the mock FRC Driver Station screen for a station in a native window (or a browser tab as fallback).
import { openWindow } from "../core/windows.js";

export async function popOutMockDs(station) {
  const url = `${location.origin}/ds.html?station=${encodeURIComponent(station)}`;
  await openWindow(url, `Driver Station — ${station}`, 1060, 320);
}
