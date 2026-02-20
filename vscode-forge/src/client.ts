import * as http from 'http';
import * as https from 'https';
import { EventEmitter } from 'events';
import WebSocket from 'ws';
import {
  AppState, DebugMessage, DebugSnapshot, DebugHealth, ServerEntry
} from './types';

export declare interface ForgeDebugClient {
  on(event: 'stateChanged', listener: () => void): this;
  on(event: 'metricsUpdated', listener: () => void): this;
  on(event: 'error', listener: (err: Error) => void): this;
  on(event: 'disconnected', listener: () => void): this;
}

export class ForgeDebugClient extends EventEmitter {
  private ws?: WebSocket;
  private reconnectTimer?: NodeJS.Timeout;
  private _state: AppState;

  constructor(readonly entry: ServerEntry) {
    super();
    this._state = { entry, connected: false, lastUpdate: 0 };
  }

  get state(): AppState { return this._state; }

  connect(): void {
    const url = `ws://${this.entry.debug_addr}/ws`;
    this.ws = new WebSocket(url);

    this.ws.on('open', () => {
      this._state.connected = true;
    });

    this.ws.on('message', (data: WebSocket.RawData) => {
      try {
        const msg: DebugMessage = JSON.parse(data.toString());
        this._state.lastUpdate = Date.now();
        switch (msg.type) {
          case 'snapshot': {
            this._state.snapshot = msg.payload as DebugSnapshot;
            this._state.health = this._state.snapshot.health;
            this.emit('stateChanged');
            break;
          }
          case 'metrics': {
            const m = msg.payload as { raw: string };
            this._state.metricsRaw = m.raw;
            this.emit('metricsUpdated');
            break;
          }
          case 'health': {
            this._state.health = msg.payload as DebugHealth;
            this.emit('stateChanged');
            break;
          }
        }
      } catch { /* ignore malformed frames */ }
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

  disconnect(): void {
    if (this.reconnectTimer) { clearTimeout(this.reconnectTimer); }
    this.ws?.terminate();
    this._state.connected = false;
  }

  /** Fetch a fresh snapshot via REST (for one-off refreshes). */
  async fetchSnapshot(): Promise<void> {
    return new Promise((resolve) => {
      const url = `http://${this.entry.debug_addr}/state`;
      const lib = url.startsWith('https') ? https : http;
      lib.get(url, (res) => {
        let body = '';
        res.on('data', (c: string) => body += c);
        res.on('end', () => {
          try {
            const msg: DebugMessage = JSON.parse(body);
            if (msg.type === 'snapshot') {
              this._state.snapshot = msg.payload as DebugSnapshot;
              this.emit('stateChanged');
            }
          } catch { /* ignore */ }
          resolve();
        });
      }).on('error', () => resolve());
    });
  }
}
