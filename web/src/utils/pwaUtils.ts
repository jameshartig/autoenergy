export const isIOSDevice = (): boolean => {
    if (typeof navigator === 'undefined') return false;
    return (
        /iPad|iPhone|iPod/.test(navigator.userAgent) ||
        (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
    );
};

export const hasNotificationSupport = (): boolean => {
    return typeof window !== 'undefined' && 'Notification' in window && typeof window.Notification !== 'undefined';
};

export const isIOSHomeScreen = (): boolean => {
    return isIOSDevice() && hasNotificationSupport();
};

export const hasServiceWorkerSupport = (): boolean => {
    return typeof navigator !== 'undefined' && 'serviceWorker' in navigator;
};

export const hasPushManagerSupport = (): boolean => {
    return typeof window !== 'undefined' && 'PushManager' in window;
};

export const isPushSupportedInBrowser = (): boolean => {
    if (!hasServiceWorkerSupport()) {
        return false;
    }
    if (isIOSDevice()) {
        // On iOS Safari browser tabs, both Notification and PushManager are undefined
        // until the user adds the web app to the Home Screen. As long as serviceWorker
        // exists, the iOS device supports Web Push via Home Screen installation.
        return true;
    }
    return hasPushManagerSupport() && hasNotificationSupport();
};
