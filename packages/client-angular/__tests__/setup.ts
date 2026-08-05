// The JIT compiler, so `@Component({template})` in a test file is compiled at
// runtime. A published Angular application is compiled ahead of time and never
// loads this; a test that writes its own components has to.
import '@angular/compiler';
import { TestBed } from '@angular/core/testing';
import { BrowserTestingModule, platformBrowserTesting } from '@angular/platform-browser/testing';
import { afterEach } from 'vitest';
import { setClient } from '@forge-go/client-core';

TestBed.initTestEnvironment(BrowserTestingModule, platformBrowserTesting());

afterEach(() => {
  // Destroys every fixture the test created, which is what runs the
  // `DestroyRef` callbacks -- a subscription that outlived its `it()` would
  // otherwise turn the next test's mount-count assertion into a pass or a
  // failure for the wrong reason.
  TestBed.resetTestingModule();
  // The module-level client is global state, and a test that configures one
  // and then leaks it turns the next test's "no client configured" assertion
  // into a pass for the wrong reason.
  setClient(undefined);
});
