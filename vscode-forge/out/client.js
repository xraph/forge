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
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.ForgeDebugClient = void 0;
const http = __importStar(require("http"));
const https = __importStar(require("https"));
const events_1 = require("events");
const ws_1 = __importDefault(require("ws"));
class ForgeDebugClient extends events_1.EventEmitter {
    constructor(entry) {
        super();
        this.entry = entry;
        this._state = { entry, connected: false, lastUpdate: 0 };
    }
    get state() { return this._state; }
    connect() {
        const url = `ws://${this.entry.debug_addr}/ws`;
        this.ws = new ws_1.default(url);
        this.ws.on('open', () => {
            this._state.connected = true;
        });
        this.ws.on('message', (data) => {
            try {
                const msg = JSON.parse(data.toString());
                this._state.lastUpdate = Date.now();
                switch (msg.type) {
                    case 'snapshot': {
                        this._state.snapshot = msg.payload;
                        this._state.health = this._state.snapshot.health;
                        this.emit('stateChanged');
                        break;
                    }
                    case 'metrics': {
                        const m = msg.payload;
                        this._state.metricsRaw = m.raw;
                        this.emit('metricsUpdated');
                        break;
                    }
                    case 'health': {
                        this._state.health = msg.payload;
                        this.emit('stateChanged');
                        break;
                    }
                }
            }
            catch { /* ignore malformed frames */ }
        });
        this.ws.on('close', () => {
            this._state.connected = false;
            this.emit('disconnected');
            // Attempt reconnect after 3 s.
            this.reconnectTimer = setTimeout(() => this.connect(), 3000);
        });
        this.ws.on('error', (err) => {
            this._state.connected = false;
            this.emit('error', err);
        });
    }
    disconnect() {
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
        }
        this.ws?.terminate();
        this._state.connected = false;
    }
    /** Fetch a fresh snapshot via REST (for one-off refreshes). */
    async fetchSnapshot() {
        return new Promise((resolve) => {
            const url = `http://${this.entry.debug_addr}/state`;
            const lib = url.startsWith('https') ? https : http;
            lib.get(url, (res) => {
                let body = '';
                res.on('data', (c) => body += c);
                res.on('end', () => {
                    try {
                        const msg = JSON.parse(body);
                        if (msg.type === 'snapshot') {
                            this._state.snapshot = msg.payload;
                            this.emit('stateChanged');
                        }
                    }
                    catch { /* ignore */ }
                    resolve();
                });
            }).on('error', () => resolve());
        });
    }
}
exports.ForgeDebugClient = ForgeDebugClient;
//# sourceMappingURL=client.js.map