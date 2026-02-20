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
exports.ServicesProvider = void 0;
const vscode = __importStar(require("vscode"));
class ServiceItem extends vscode.TreeItem {
    constructor(name) {
        super(name, vscode.TreeItemCollapsibleState.None);
        this.iconPath = new vscode.ThemeIcon('symbol-class');
        this.contextValue = 'forgeService';
    }
}
class ServicesProvider {
    constructor() {
        this._onDidChangeTreeData = new vscode.EventEmitter();
        this.onDidChangeTreeData = this._onDidChangeTreeData.event;
    }
    setClient(client) {
        this.activeClient = client;
        client?.on('stateChanged', () => this._onDidChangeTreeData.fire(undefined));
        this._onDidChangeTreeData.fire(undefined);
    }
    getTreeItem(e) { return e; }
    getChildren() {
        const services = this.activeClient?.state.snapshot?.services ?? [];
        if (services.length === 0) {
            const empty = new vscode.TreeItem('No services registered');
            empty.iconPath = new vscode.ThemeIcon('info');
            return [empty];
        }
        return services.map(s => new ServiceItem(s));
    }
}
exports.ServicesProvider = ServicesProvider;
//# sourceMappingURL=servicesProvider.js.map