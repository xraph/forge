import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import type { ComponentFixture } from '@angular/core/testing';
import type { Provider, Type } from '@angular/core';
import { provideClient } from '../src';
import type { Harness } from './harness';

/**
 * Configure a zoneless TestBed with the fixture's cache provided.
 *
 * Zoneless rather than zone.js: change detection driven by the signals this
 * adapter writes is exactly what is under test, and a zone patching every
 * promise in the process would hide a missing notification behind a tick that
 * happened for an unrelated reason.
 */
export function configure(fx: Harness, extra: Provider[] = []): void {
  TestBed.configureTestingModule({
    providers: [provideZonelessChangeDetection(), provideClient(fx.cache), ...extra],
  });
}

/**
 * Let every already-queued microtask run, then apply what changed.
 *
 * No timers and no sleeps: the fake transport resolves on the microtask queue,
 * so draining it is deterministic, and `detectChanges` turns whatever the
 * signals now hold into rendered output.
 */
export async function settle(fixture?: ComponentFixture<unknown>): Promise<void> {
  for (let i = 0; i < 8; i++) await Promise.resolve();

  fixture?.detectChanges();
}

export function render<T>(component: Type<T>): ComponentFixture<T> {
  const fixture = TestBed.createComponent(component);

  fixture.detectChanges();

  return fixture;
}
