"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.ServerDiscovery = void 0;
const vscode = __importStar(require("vscode"));
const registry_1 = require("./utils/registry");
const pid_1 = require("./utils/pid");
class ServerDiscovery {
    constructor() {
        this.listeners = [];
    }
    start() {
        this.watcher = (0, registry_1.watchRegistry)((servers) => {
            const live = this.filterLive(servers);
            this.listeners.forEach(l => l(live));
        });
    }
    stop() {
        this.watcher?.close();
    }
    current() {
        return this.filterLive((0, registry_1.readRegistry)());
    }
    onChange(listener) {
        this.listeners.push(listener);
    }
    filterLive(servers) {
        const showAll = vscode.workspace.getConfiguration('forge').get('showAllServers', false);
        const workspaceDirs = (vscode.workspace.workspaceFolders ?? []).map(f => f.uri.fsPath);
        return servers.filter(s => {
            if (!(0, pid_1.isPidAlive)(s.pid)) {
                return false;
            }
            if (showAll) {
                return true;
            }
            return workspaceDirs.some(d => s.workspace_dir.startsWith(d) || d.startsWith(s.workspace_dir));
        });
    }
}
exports.ServerDiscovery = ServerDiscovery;
//# sourceMappingURL=discovery.js.map