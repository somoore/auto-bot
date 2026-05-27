import { describe, expect, it } from 'vitest';
import { Mutex } from './index';

describe('Mutex', () => {
  it('should not be locked initially', () => {
    const mutex = new Mutex();
    expect(mutex.isLocked()).toBe(false);
  });

  it('should lock and unlock correctly', async () => {
    const mutex = new Mutex();
    const unlock = await mutex.lock();
    expect(mutex.isLocked()).toBe(true);
    unlock();
    expect(mutex.isLocked()).toBe(false);
  });

  it('should handle multiple locks', async () => {
    const mutex = new Mutex();
    const unlock1 = await mutex.lock();
    const unlock2Promise = mutex.lock();
    expect(mutex.isLocked()).toBe(true);
    unlock1();
    expect(mutex.isLocked()).toBe(true);
    const unlock3Promise = mutex.lock();
    expect(mutex.isLocked()).toBe(true);
    (await unlock2Promise)();
    expect(mutex.isLocked()).toBe(true);
    (await unlock3Promise)();
    expect(mutex.isLocked()).toBe(false);
  });

  it('should not care about unlocking the same lock twice', async () => {
    const mutex = new Mutex();
    const unlock1 = await mutex.lock();
    expect(mutex.isLocked()).toBe(true);
    unlock1();
    expect(mutex.isLocked()).toBe(false);
    unlock1();
    expect(mutex.isLocked()).toBe(false);
  });
});
