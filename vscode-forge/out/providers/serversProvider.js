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
exports.ServersProvider = exports.ServerItem = void 0;
const vscode = __importStar(require("vscode"));
class ServerItem extends vscode.TreeItem {
    constructor(entry, client) {
        const connected = client.state.connected;
        super(entry.app_name, vscode.TreeItemCollapsibleState.None);
        this.entry = entry;
        this.client = client;
        this.description = `${entry.app_version} · ${entry.debug_addr}`;
        this.tooltip = `PID: ${entry.pid}\nApp: ${entry.app_addr}\nDebug: ${entry.debug_addr}\nWorkspace: ${entry.workspace_dir}`;
        this.iconPath = new vscode.ThemeIcon(connected ? 'circle-filled' : 'circle-outline');
        this.contextValue = 'forgeServer';
    }
}
exports.ServerItem = ServerItem;
class ServersProvider {
    constructor() {
        this._onDidChangeTreeData = new vscode.EventEmitter();
        this.onDidChangeTreeData = this._onDidChangeTreeData.event;
        this.items = [];
    }
    updateClients(clients) {
        this.items = Array.from(clients.values()).map(c => new ServerItem(c.entry, c));
        this._onDidChangeTreeData.fire(undefined);
    }
    getTreeItem(element) { return element; }
    getChildren() { return this.items; }
}
exports.ServersProvider = ServersProvider;
//# sourceMappingURL=serversProvider.js.map