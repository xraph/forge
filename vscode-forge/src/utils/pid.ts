/**
 * Returns true if a process with the given PID is currently alive.
 * Uses process.kill(pid, 0) on Unix/macOS (no signal sent, just existence check).
 */
export function isPidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}
