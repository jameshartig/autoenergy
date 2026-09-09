import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
    isIOSDevice,
    hasNotificationSupport,
    isIOSHomeScreen,
    hasServiceWorkerSupport,
    hasPushManagerSupport,
    isPushSupportedInBrowser,
} from './pwaUtils';

describe('pwaUtils', () => {
    beforeEach(() => {
        vi.restoreAllMocks();
    });

    describe('isIOSDevice', () => {
        it('detects iPhone, iPad, and iPod from userAgent', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            expect(isIOSDevice()).toBe(true);

            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPad; CPU OS 16_5 like Mac OS X)');
            expect(isIOSDevice()).toBe(true);

            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPod touch; CPU iPhone OS 14_0 like Mac OS X)');
            expect(isIOSDevice()).toBe(true);
        });

        it('detects iPadOS with MacIntel and touch points > 1', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)');
            vi.spyOn(navigator, 'platform', 'get').mockReturnValue('MacIntel');
            Object.defineProperty(navigator, 'maxTouchPoints', { value: 5, configurable: true });
            expect(isIOSDevice()).toBe(true);
        });

        it('returns false for macOS desktop without touch points', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)');
            vi.spyOn(navigator, 'platform', 'get').mockReturnValue('MacIntel');
            Object.defineProperty(navigator, 'maxTouchPoints', { value: 0, configurable: true });
            expect(isIOSDevice()).toBe(false);
        });

        it('returns false for Windows and Android', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            expect(isIOSDevice()).toBe(false);

            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Linux; Android 14)');
            expect(isIOSDevice()).toBe(false);
        });
    });

    describe('hasNotificationSupport', () => {
        it('returns true when window.Notification is defined', () => {
            (window as any).Notification = { permission: 'default' };
            expect(hasNotificationSupport()).toBe(true);
        });

        it('returns false when window.Notification is undefined', () => {
            delete (window as any).Notification;
            expect(hasNotificationSupport()).toBe(false);
        });
    });

    describe('isIOSHomeScreen', () => {
        it('returns true when on iOS and Notification is supported', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            (window as any).Notification = { permission: 'default' };
            expect(isIOSHomeScreen()).toBe(true);
        });

        it('returns false when on iOS and Notification is not supported', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            delete (window as any).Notification;
            expect(isIOSHomeScreen()).toBe(false);
        });

        it('returns false when Notification is supported but device is not iOS', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            (window as any).Notification = { permission: 'default' };
            expect(isIOSHomeScreen()).toBe(false);
        });
    });

    describe('hasServiceWorkerSupport', () => {
        it('returns true when serviceWorker is in navigator', () => {
            Object.defineProperty(navigator, 'serviceWorker', { value: {}, configurable: true });
            expect(hasServiceWorkerSupport()).toBe(true);
        });

        it('returns false when serviceWorker is not in navigator', () => {
            const originalSW = Object.getOwnPropertyDescriptor(navigator, 'serviceWorker');
            delete (navigator as any).serviceWorker;
            expect(hasServiceWorkerSupport()).toBe(false);
            if (originalSW) {
                Object.defineProperty(navigator, 'serviceWorker', originalSW);
            }
        });
    });

    describe('hasPushManagerSupport', () => {
        it('returns true when PushManager is in window', () => {
            (window as any).PushManager = {};
            expect(hasPushManagerSupport()).toBe(true);
        });

        it('returns false when PushManager is not in window', () => {
            delete (window as any).PushManager;
            expect(hasPushManagerSupport()).toBe(false);
        });
    });

    describe('isPushSupportedInBrowser', () => {
        beforeEach(() => {
            Object.defineProperty(navigator, 'serviceWorker', { value: {}, configurable: true });
            (window as any).PushManager = {};
            (window as any).Notification = { permission: 'default' };
        });

        it('returns true on non-iOS desktop when serviceWorker, PushManager, and Notification exist', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            expect(isPushSupportedInBrowser()).toBe(true);
        });

        it('returns false on non-iOS when Notification is undefined', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            delete (window as any).Notification;
            expect(isPushSupportedInBrowser()).toBe(false);
        });

        it('returns false when serviceWorker is missing', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            delete (navigator as any).serviceWorker;
            expect(isPushSupportedInBrowser()).toBe(false);
        });

        it('returns false when PushManager is missing', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            delete (window as any).PushManager;
            expect(isPushSupportedInBrowser()).toBe(false);
        });

        it('returns true on iOS even when Notification and PushManager are undefined (standard iOS Safari tab)', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            delete (window as any).Notification;
            delete (window as any).PushManager;
            expect(isPushSupportedInBrowser()).toBe(true);
        });

        it('returns false on iOS when serviceWorker is missing', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            delete (navigator as any).serviceWorker;
            expect(isPushSupportedInBrowser()).toBe(false);
        });
    });
});
