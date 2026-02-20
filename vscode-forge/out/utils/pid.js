"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.isPidAlive = isPidAlive;
/**
 * Returns true if a process with the given PID is currently alive.
 * Uses process.kill(pid, 0) on Unix/macOS (no signal sent, just existence check).
 */
function isPidAlive(pid) {
    try {
        process.kill(pid, 0);
        return true;
    }
    catch {
        return false;
    }
}
//# sourceMappingURL=pid.js.map