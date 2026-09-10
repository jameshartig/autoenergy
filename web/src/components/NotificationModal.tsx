import React, { useEffect, useState, useCallback } from 'react';
import { Dialog } from '@base-ui/react/dialog';
import { Field } from '@base-ui/react/field';
import { Switch } from '@base-ui/react/switch';
import { Select } from '@base-ui/react/select';
import {
    fetchNotificationSettings,
    updateNotificationSettings,
    fetchVAPIDPublicKey,
    subscribePushNotification,
    unsubscribePushNotification,
    type UserNotificationSettings,
    type PushSubscription,
    type MorningSummaryFlavor,
    type EveningSummaryFlavor,
    type AnomalyAlertSensitivity
} from '../api';
import { HelpButton } from './HelpButton';
import { isIOSDevice, hasNotificationSupport, isPushSupportedInBrowser } from '../utils/pwaUtils';
import { formatHour12 } from '../utils/dashboardUtils';
import './NotificationModal.css';

interface NotificationModalProps {
    open: boolean;
    onClose: () => void;
    siteID?: string;
    siteName?: string;
}

const flavorPreviews: Record<MorningSummaryFlavor, { name: string; desc: string; title: string; body: string }> = {
    metrics_heavy: {
        name: 'Metrics Heavy',
        desc: 'Comprehensive data with SOC kWh, solar percentage comparison, and full-charge ETA.',
        title: '🔋 74% SOC (10.1 kWh) • ☀️ 38.4 kWh Solar',
        body: 'Forecast: +15% vs yesterday. Full charge expected by 1:15 PM.'
    },
    home_planner: {
        name: 'Home Planner',
        desc: 'Clear solar outlook and battery expectations for the day.',
        title: '☀️ Great Solar Day Ahead',
        body: 'Battery at 74%. Full battery expected by 1:15 PM.'
    },
    executive: {
        name: 'Executive Summary',
        desc: 'High-level snapshot highlighting solar generation and battery status.',
        title: '☀️ 38.0 kWh Solar Expected • 🔋 74% SOC',
        body: 'Great solar today; battery will fully top off by 1:15 PM.'
    },
    pilot: {
        name: 'Autonomous Pilot',
        desc: 'Real-time reasoning from RateRudder explaining how the battery is being automated.',
        title: '🤖 RateRudder: Morning Outlook',
        body: 'Battery at 74%. Forecast shows 38.0 kWh solar refilling battery by 1:15 PM. Optimizing daytime self-consumption.'
    }
};

const eveningFlavorPreviews: Record<EveningSummaryFlavor, { name: string; desc: string; title: string; body: string }> = {
    metrics_heavy: {
        name: 'Metrics Heavy',
        desc: 'Complete totals for the day: generation, consumption, grid export, and battery reserve.',
        title: '🌙 38.4 kWh Solar • 🔋 85% SOC (11.6 kWh)',
        body: 'Today: 38.4 kWh solar, 22.1 kWh home (12.0 kWh exported). Battery: 11.6 kWh powers home through sunrise.'
    },
    home_planner: {
        name: 'Home Planner',
        desc: 'Actionable evening status on how long the battery will last overnight.',
        title: '🌙 Evening Energy Wrap-up',
        body: 'Battery at 85% (11.6 kWh). Projected to supply home until ~1:15 AM before drawing from the grid.'
    },
    executive: {
        name: 'Executive Summary',
        desc: 'Concise daily recap highlighting solar harvest and overnight battery status.',
        title: '🌙 38.4 kWh Solar Today • 🔋 85% SOC',
        body: 'Solar generated 38.4 kWh today with 12.0 kWh exported to the grid. Battery entering night at 85%.'
    },
    pilot: {
        name: 'Autonomous Pilot',
        desc: 'Real-time explanation of nighttime battery strategy and grid switchover projection.',
        title: '🤖 RateRudder: Evening Wrap-up',
        body: 'Automated battery managed 38.4 kWh solar today. Stored 11.6 kWh projected to power home through sunrise.'
    }
};

const priceSpikeLabels: Record<string, string> = {
    '': 'Disabled',
    disabled: 'Disabled',
    low: 'Low Sensitivity',
    medium: 'Medium Sensitivity',
    high: 'High Sensitivity'
};

const solarUnderproductionLabels: Record<string, string> = {
    '': 'Disabled',
    disabled: 'Disabled',
    low: 'Low Sensitivity',
    medium: 'Medium Sensitivity',
    high: 'High Sensitivity'
};

interface NotificationSampleProps {
    headerLabel?: string;
    title: string;
    body: string;
}

const NotificationSample: React.FC<NotificationSampleProps> = ({ headerLabel = 'Sample Notification', title, body }) => (
    <div className="notification-preview-box" style={{ marginTop: '0.75rem', marginBottom: '0.5rem' }}>
        <div className="notification-preview-header">
            <span>{headerLabel}</span>
        </div>
        <div className="notification-preview-card">
            <div className="notification-preview-icon">
                <img src="/logo_192.png" alt="RateRudder" />
            </div>
            <div className="notification-preview-text">
                <div className="notification-preview-title">{title}</div>
                <div className="notification-preview-body">{body}</div>
            </div>
        </div>
    </div>
);

const getDeviceName = (ua?: string): string => {
    if (!ua) return 'Web Browser';
    if (ua.includes('iPhone')) return 'Safari on iPhone';
    if (ua.includes('iPad')) return 'Safari on iPad';
    if (ua.includes('Android')) {
        if (ua.includes('Chrome')) return 'Chrome on Android';
        if (ua.includes('Firefox')) return 'Firefox on Android';
        return 'Android Browser';
    }
    if (ua.includes('Macintosh')) {
        if (ua.includes('Chrome')) return 'Chrome on Mac';
        if (ua.includes('Safari')) return 'Safari on Mac';
        if (ua.includes('Firefox')) return 'Firefox on Mac';
        return 'Mac Browser';
    }
    if (ua.includes('Windows')) {
        if (ua.includes('Edg')) return 'Edge on Windows';
        if (ua.includes('Chrome')) return 'Chrome on Windows';
        if (ua.includes('Firefox')) return 'Firefox on Windows';
        return 'Windows Browser';
    }
    return 'Web Browser';
};

const defaultSettingsValues: UserNotificationSettings = {
    morningSummaryEnabled: false,
    morningSummaryHour: 7,
    morningSummaryFlavor: 'home_planner',
    eveningSummaryEnabled: false,
    eveningSummaryHour: 20,
    eveningSummaryFlavor: 'home_planner',
    gridOutageAlert: false,
    priceSpikeAlert: '',
    solarUnderproductionAlert: '',
    vppDispatchAlert: false
};

export const NotificationModal: React.FC<NotificationModalProps> = ({
    open,
    onClose,
    siteID,
    siteName
}) => {
    const [savedSettings, setSavedSettings] = useState<UserNotificationSettings>(defaultSettingsValues);
    const [draftSettings, setDraftSettings] = useState<UserNotificationSettings>(defaultSettingsValues);
    const [subscriptions, setSubscriptions] = useState<PushSubscription[]>([]);
    const [vapidEnabled, setVapidEnabled] = useState(true);
    const [isSubscribedLocally, setIsSubscribedLocally] = useState(false);
    const [permission, setPermission] = useState<NotificationPermission>(() => {
        return typeof Notification !== 'undefined' ? Notification.permission : 'default';
    });
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [testing, setTesting] = useState(false);
    const [testSuccess, setTestSuccess] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const hasAnyDeviceConnected = isSubscribedLocally || subscriptions.length > 0;

    const isIOS = isIOSDevice();
    const hasNotifications = hasNotificationSupport();
    const isPushSupported = isPushSupportedInBrowser();
    const isIOSWithoutNotifications = isIOS && isPushSupported && !hasNotifications;

    const checkLocalSubscription = useCallback(async (knownSubscriptions?: PushSubscription[]) => {
        if (typeof window !== 'undefined' && 'serviceWorker' in navigator && 'PushManager' in window) {
            try {
                const reg = await navigator.serviceWorker.ready;
                const sub = await reg.pushManager.getSubscription();
                if (!sub) {
                    setIsSubscribedLocally(false);
                    return;
                }
                // If knownSubscriptions was provided, verify this browser's subscription is registered
                if (knownSubscriptions) {
                    const isRegistered = knownSubscriptions.some(s => s.endpoint === sub.endpoint);
                    if (!isRegistered) {
                        try {
                            await sub.unsubscribe();
                        } catch (e) {
                            console.error('Failed to unsubscribe stale push subscription', e);
                        }
                        setIsSubscribedLocally(false);
                        return;
                    }
                }
                setIsSubscribedLocally(true);
            } catch {
                setIsSubscribedLocally(false);
            }
        }
    }, []);

    const loadSettings = useCallback(async () => {
        if (!siteID) {
            setLoading(false);
            return;
        }
        try {
            setLoading(true);
            const res = await fetchNotificationSettings(siteID);
            const server = res.settings;
            const initial: UserNotificationSettings = server ? {
                morningSummaryEnabled: server.morningSummaryEnabled ?? defaultSettingsValues.morningSummaryEnabled,
                morningSummaryHour: (server.morningSummaryHour !== undefined && server.morningSummaryHour !== null && (server.morningSummaryHour !== 0 || server.morningSummaryEnabled))
                    ? server.morningSummaryHour
                    : defaultSettingsValues.morningSummaryHour,
                morningSummaryFlavor: server.morningSummaryFlavor || defaultSettingsValues.morningSummaryFlavor,
                eveningSummaryEnabled: server.eveningSummaryEnabled ?? defaultSettingsValues.eveningSummaryEnabled,
                eveningSummaryHour: (server.eveningSummaryHour !== undefined && server.eveningSummaryHour !== null && (server.eveningSummaryHour !== 0 || server.eveningSummaryEnabled))
                    ? server.eveningSummaryHour
                    : defaultSettingsValues.eveningSummaryHour,
                eveningSummaryFlavor: server.eveningSummaryFlavor || defaultSettingsValues.eveningSummaryFlavor,
                gridOutageAlert: server.gridOutageAlert ?? defaultSettingsValues.gridOutageAlert,
                priceSpikeAlert: server.priceSpikeAlert || '',
                solarUnderproductionAlert: server.solarUnderproductionAlert || '',
                vppDispatchAlert: server.vppDispatchAlert ?? defaultSettingsValues.vppDispatchAlert,
            } : defaultSettingsValues;
            setSavedSettings(initial);
            setDraftSettings(initial);
            const serverSubs = res.subscriptions || [];
            setSubscriptions(serverSubs);
            setVapidEnabled(res.vapidEnabled);
            await checkLocalSubscription(serverSubs);
        } catch (err: any) {
            console.error('Failed to load notification settings', err);
        } finally {
            setLoading(false);
        }
    }, [siteID, checkLocalSubscription]);

    useEffect(() => {
        if (open) {
            loadSettings();
        }
    }, [open, loadSettings]);

    const handleToggleLocalPush = async (checked: boolean) => {
        if (!isPushSupported || !hasNotifications) {
            return;
        }

        setError(null);
        setSaving(true);

        try {
            if (checked) {
                let perm = Notification.permission;
                if (perm === 'denied') {
                    setError('Notifications are blocked by your browser. Please enable notifications in your browser site settings.');
                    setSaving(false);
                    return;
                }

                if (perm !== 'granted') {
                    perm = await Notification.requestPermission();
                    setPermission(perm);
                    if (perm !== 'granted') {
                        setError('Notification permission was not granted.');
                        setSaving(false);
                        return;
                    }
                }

                const pubKeyBuffer = await fetchVAPIDPublicKey();
                const reg = await navigator.serviceWorker.ready;
                const sub = await reg.pushManager.subscribe({
                    userVisibleOnly: true,
                    applicationServerKey: pubKeyBuffer
                });

                const subJSON = sub.toJSON();
                await subscribePushNotification({
                    endpoint: sub.endpoint,
                    keys: {
                        p256dh: subJSON.keys?.p256dh || '',
                        auth: subJSON.keys?.auth || ''
                    },
                    userAgent: navigator.userAgent
                }, true);

                setIsSubscribedLocally(true);
                if (siteID) {
                    const updated = await fetchNotificationSettings(siteID);
                    setSubscriptions(updated.subscriptions || []);
                }
            } else {
                const reg = await navigator.serviceWorker.ready;
                const sub = await reg.pushManager.getSubscription();
                if (sub) {
                    await sub.unsubscribe();
                    await unsubscribePushNotification(sub.endpoint);
                }
                setIsSubscribedLocally(false);
                if (siteID) {
                    const updated = await fetchNotificationSettings(siteID);
                    setSubscriptions(updated.subscriptions || []);
                }
            }
        } catch (err: any) {
            setError(err.message || 'Failed to update push subscription');
        } finally {
            setSaving(false);
        }
    };

    const handleUpdateDraft = (updates: Partial<UserNotificationSettings>) => {
        setDraftSettings(prev => ({ ...prev, ...updates }));
    };

    const handleSavePreferences = async () => {
        if (!siteID) return;
        setSaving(true);
        setError(null);

        const settingsToSave: UserNotificationSettings = {
            ...draftSettings,
            morningSummaryFlavor: draftSettings.morningSummaryFlavor || defaultSettingsValues.morningSummaryFlavor,
            morningSummaryHour: draftSettings.morningSummaryHour ?? defaultSettingsValues.morningSummaryHour,
            eveningSummaryFlavor: draftSettings.eveningSummaryFlavor || defaultSettingsValues.eveningSummaryFlavor,
            eveningSummaryHour: draftSettings.eveningSummaryHour ?? defaultSettingsValues.eveningSummaryHour,
        };

        try {
            await updateNotificationSettings(siteID, settingsToSave);
            setSavedSettings(settingsToSave);
            setDraftSettings(settingsToSave);
            onClose();
        } catch (err: any) {
            setError(err.message || 'Failed to save notification preferences');
        } finally {
            setSaving(false);
        }
    };

    const handleCancel = () => {
        setDraftSettings(savedSettings);
        setError(null);
        onClose();
    };

    // TODO: Remove test notification button before public release
    const handleSendTest = async () => {
        setTesting(true);
        setError(null);
        setTestSuccess(false);

        try {
            if (!('serviceWorker' in navigator)) {
                throw new Error('Service Worker not supported');
            }
            const reg = await navigator.serviceWorker.ready;
            const sub = await reg.pushManager.getSubscription();
            if (!sub) {
                throw new Error('This device is not subscribed to push notifications. Enable the toggle above first.');
            }

            const subJSON = sub.toJSON();
            await subscribePushNotification({
                endpoint: sub.endpoint,
                keys: {
                    p256dh: subJSON.keys?.p256dh || '',
                    auth: subJSON.keys?.auth || ''
                },
                userAgent: navigator.userAgent
            }, true);

            setTestSuccess(true);
            setTimeout(() => setTestSuccess(false), 4000);
        } catch (err: any) {
            setError(err.message || 'Failed to send test push notification');
        } finally {
            setTesting(false);
        }
    };

    const handleRemoveDevice = async (endpoint: string) => {
        try {
            // If the device being removed is this local browser, unsubscribe from pushManager too
            if (typeof window !== 'undefined' && 'serviceWorker' in navigator && 'PushManager' in window) {
                try {
                    const reg = await navigator.serviceWorker.ready;
                    const sub = await reg.pushManager.getSubscription();
                    if (sub && sub.endpoint === endpoint) {
                        await sub.unsubscribe();
                        setIsSubscribedLocally(false);
                    }
                } catch (e) {
                    console.error('Failed to unsubscribe local push manager', e);
                }
            }

            await unsubscribePushNotification(endpoint);
            let updatedSubs: PushSubscription[] = [];
            if (siteID) {
                const updated = await fetchNotificationSettings(siteID);
                updatedSubs = updated.subscriptions || [];
                setSubscriptions(updatedSubs);
            }
            await checkLocalSubscription(updatedSubs);
        } catch (err: any) {
            setError(err.message || 'Failed to remove device');
        }
    };

    const currentFlavor = draftSettings.morningSummaryFlavor || 'home_planner';
    const activePreview = flavorPreviews[currentFlavor] || flavorPreviews.home_planner;

    const currentEveningFlavor = draftSettings.eveningSummaryFlavor || 'home_planner';
    const activeEveningPreview = eveningFlavorPreviews[currentEveningFlavor] || eveningFlavorPreviews.home_planner;

    return (
        <Dialog.Root open={open} onOpenChange={(isOpen) => { if (!isOpen) handleCancel(); }}>
            <Dialog.Portal>
                <Dialog.Backdrop className="dialog-backdrop" />
                <Dialog.Popup className="dialog-popup notification-dialog-popup" aria-labelledby="notification-modal-title">
                    <div className="modal-header-row">
                        <div className="modal-title-group">
                            <Dialog.Title className="dialog-title" id="notification-modal-title">
                                Notifications
                            </Dialog.Title>
                            {siteName && (
                                <span className="modal-site-subtitle">
                                    Configuring alerts for {siteName}
                                </span>
                            )}
                        </div>
                        <Dialog.Close
                            className="dialog-close-icon"
                            aria-label="Close notification settings"
                            onClick={handleCancel}
                        >
                            ✕
                        </Dialog.Close>
                    </div>

                    <div className="modal-scrollable-body">
                        {loading ? (
                            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', padding: '1rem 0' }}>
                                Loading notification preferences...
                            </div>
                        ) : !vapidEnabled ? (
                            <div className="notification-banner warning">
                                <span className="notification-banner-icon" aria-hidden="true">⚠️</span>
                                <div className="notification-banner-content">
                                    <strong>Web Push Not Configured</strong>
                                    Web push keys are not configured on this server instance.
                                </div>
                            </div>
                        ) : (
                            <>
                                {/* Push Notifications Unsupported Warning */}
                                {!isPushSupported && (
                                    <div className="notification-banner warning" data-testid="push-unsupported-banner">
                                        <span className="notification-banner-icon" aria-hidden="true">⚠️</span>
                                        <div className="notification-banner-content">
                                            <strong>Push Notifications Unsupported</strong>
                                            Push notifications are not supported in this browser.
                                        </div>
                                    </div>
                                )}

                                {/* iOS Safari Guidance Banner */}
                                {isIOSWithoutNotifications && (
                                    <div className="notification-banner info">
                                        <span className="notification-banner-icon" aria-hidden="true">📱</span>
                                        <div className="notification-banner-content">
                                            <strong>iOS Safari Push Setup</strong>
                                            To receive notifications on your iPhone or iPad, tap the three-dot menu (⋯), tap <strong>Share</strong>{' '}
                                            <svg
                                                width="15"
                                                height="15"
                                                viewBox="0 0 24 24"
                                                fill="none"
                                                stroke="currentColor"
                                                strokeWidth="2"
                                                strokeLinecap="round"
                                                strokeLinejoin="round"
                                                aria-label="Share"
                                                style={{ display: 'inline-block', verticalAlign: '-2px', margin: '0 2px' }}
                                            >
                                                <path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8" />
                                                <polyline points="16 6 12 2 8 6" />
                                                <line x1="12" y1="2" x2="12" y2="15" />
                                            </svg>
                                            , tap <strong>&quot;View More&quot;</strong>, then select <strong>&quot;Add to Home Screen&quot;</strong>. Then open RateRudder from your Home Screen.
                                        </div>
                                    </div>
                                )}

                                {/* Browser Permission Blocked Alert */}
                                {permission === 'denied' && (
                                    <div className="notification-banner warning">
                                        <span className="notification-banner-icon" aria-hidden="true">🚫</span>
                                        <div className="notification-banner-content">
                                            <strong>Notifications Blocked</strong>
                                            Notifications are blocked in your browser settings. To enable them, tap the lock/settings icon in your address bar and allow Notifications for RateRudder.
                                        </div>
                                    </div>
                                )}

                                {error && (
                                    <div className="error-message" style={{ marginBottom: '0.75rem' }}>
                                        {error}
                                    </div>
                                )}

                                {/* Zone A: Device Management */}
                                <div className="notif-section">
                                    {isPushSupported && !isIOSWithoutNotifications && (
                                        <Field.Root className="form-group switch-group" style={{ marginBottom: 0 }}>
                                            <div className="switch-row">
                                                <Switch.Root
                                                    id="pushNotificationsToggle"
                                                    checked={isSubscribedLocally}
                                                    onCheckedChange={handleToggleLocalPush}
                                                    disabled={saving}
                                                    className="switch-root"
                                                    aria-label="Deliver notifications to this browser"
                                                >
                                                    <Switch.Thumb className="switch-thumb" />
                                                </Switch.Root>
                                                <Field.Label htmlFor="pushNotificationsToggle" style={{ cursor: 'pointer', fontWeight: 600 }}>
                                                    Deliver notifications to this browser
                                                </Field.Label>
                                            </div>
                                            <Field.Description>
                                                Receive daily summaries and critical grid alerts on this device.
                                            </Field.Description>
                                        </Field.Root>
                                    )}

                                    <div>
                                        <Field.Root className="form-group" style={{ marginBottom: '0.35rem' }}>
                                            <Field.Label style={{ fontSize: '0.8rem', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>
                                                Active Subscribed Devices ({subscriptions.length})
                                            </Field.Label>
                                        </Field.Root>

                                        {subscriptions.length === 0 ? (
                                            <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                                                {!isPushSupported
                                                    ? 'No devices registered yet. Access RateRudder from a supported browser to register for notifications.'
                                                    : isIOSWithoutNotifications
                                                    ? 'No devices registered yet. Add RateRudder to your Home Screen to register this device.'
                                                    : 'No devices registered yet. Enable notifications above to register this browser.'}
                                            </div>
                                        ) : (
                                            <div className="device-list">
                                                {subscriptions.map((sub, idx) => (
                                                    <div key={sub.endpoint || idx} className="device-item">
                                                        <div className="device-info">
                                                            <span className="device-name">{getDeviceName(sub.userAgent)}</span>
                                                            <span className="device-date">
                                                                 Added {sub.tsCreated ? new Date(sub.tsCreated).toLocaleDateString() : 'recently'}
                                                            </span>
                                                        </div>
                                                        <button
                                                            type="button"
                                                            className="text-button danger"
                                                            onClick={() => handleRemoveDevice(sub.endpoint)}
                                                            title="Remove this device"
                                                        >
                                                            Remove
                                                        </button>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                </div>

                                {/* Zone B: Site Alert Preferences (Shown once at least 1 device is connected) */}
                                {!hasAnyDeviceConnected ? (
                                    <div className="notif-connect-prompt">
                                        <span className="notif-prompt-icon">🔔</span>
                                        <div className="notif-prompt-text">
                                            <strong>
                                                {!isPushSupported
                                                    ? 'Push notifications unsupported'
                                                    : isIOSWithoutNotifications
                                                    ? 'Add to Home Screen to get started'
                                                    : 'Connect this browser above'}
                                            </strong>
                                            <p>
                                                {!isPushSupported
                                                    ? 'This browser does not support Web Push notifications. Use a supported browser or install to your Home Screen on iOS to enable alerts.'
                                                    : isIOSWithoutNotifications
                                                    ? 'Follow the steps above to add RateRudder to your Home Screen to receive morning summaries, evening wrap-ups, and real-time energy alerts.'
                                                    : 'Enable push notifications on this device to customize daily morning summaries, evening wrap-ups, and real-time energy alerts.'}
                                            </p>
                                        </div>
                                    </div>
                                ) : (
                                    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                                        {/* Clarity Banner: Applies to all user's connected devices */}
                                        <div className="notification-banner info" data-testid="devices-clarity-banner">
                                            <span className="notification-banner-icon" aria-hidden="true">🔔</span>
                                            <div className="notification-banner-content">
                                                <strong>Applies to all your connected devices for {siteName || 'this site'}</strong>
                                                These alert settings apply to all your connected devices. Other members on this site configure their own notifications.
                                            </div>
                                        </div>

                                        {/* Morning Summary */}
                                        <div className="notif-section">
                                            <Field.Root className="form-group switch-group" style={{ marginBottom: 0 }}>
                                                <div className="switch-row">
                                                    <Switch.Root
                                                        id="morningSummaryToggle"
                                                        checked={draftSettings.morningSummaryEnabled}
                                                        onCheckedChange={(val) => handleUpdateDraft({ morningSummaryEnabled: val })}
                                                        className="switch-root"
                                                        aria-label="Daily Morning Summary"
                                                    >
                                                        <Switch.Thumb className="switch-thumb" />
                                                    </Switch.Root>
                                                    <Field.Label htmlFor="morningSummaryToggle" style={{ cursor: 'pointer' }}>
                                                        Daily Morning Summary
                                                    </Field.Label>
                                                    <HelpButton
                                                        title="Morning Summary"
                                                        description={
                                                            <p>
                                                                A daily morning briefing sent at your chosen hour with your current battery SOC, solar generation forecast for today compared to yesterday, and full-charge ETA.
                                                            </p>
                                                        }
                                                    />
                                                </div>
                                                <Field.Description>
                                                    Get a daily morning wake-up status report with battery charge and solar forecast.
                                                </Field.Description>
                                            </Field.Root>

                                            {draftSettings.morningSummaryEnabled && (
                                                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                                                    <div className="notif-row-split">
                                                        {/* Delivery Hour Dropdown */}
                                                        <Field.Root className="form-group compact">
                                                            <Field.Label htmlFor="morningSummaryHour" style={{ marginBottom: '0.35rem', display: 'block' }}>Delivery Time</Field.Label>
                                                            <Select.Root
                                                                value={String(draftSettings.morningSummaryHour ?? 7)}
                                                                onValueChange={(val) => handleUpdateDraft({ morningSummaryHour: parseInt(val as string, 10) })}
                                                            >
                                                                <Select.Trigger className="select-trigger" id="morningSummaryHour" aria-label="Morning Summary Delivery Time">
                                                                    <Select.Value>
                                                                        {formatHour12(draftSettings.morningSummaryHour ?? 7)}
                                                                    </Select.Value>
                                                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                                        </svg>
                                                                    </Select.Icon>
                                                                </Select.Trigger>
                                                                <Select.Portal>
                                                                    <Select.Positioner className="select-positioner">
                                                                        <Select.Popup className="select-popup">
                                                                            <Select.List>
                                                                                {Array.from({ length: 24 }, (_, i) => (
                                                                                    <Select.Item key={i} className="select-item" value={String(i)}>
                                                                                        <Select.ItemText>{formatHour12(i)}</Select.ItemText>
                                                                                    </Select.Item>
                                                                                ))}
                                                                            </Select.List>
                                                                        </Select.Popup>
                                                                    </Select.Positioner>
                                                                </Select.Portal>
                                                            </Select.Root>
                                                        </Field.Root>

                                                        {/* Summary Flavor Dropdown */}
                                                        <Field.Root className="form-group compact">
                                                            <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', marginBottom: '0.35rem' }}>
                                                                <Field.Label htmlFor="morningSummaryFlavor">Summary Flavor</Field.Label>
                                                                <HelpButton
                                                                    title="Morning Summary Flavors"
                                                                    ariaLabel="More info about morning summary flavors"
                                                                    description={
                                                                        <div>
                                                                            <p>Choose how you want your daily morning briefing presented:</p>
                                                                            <ul>
                                                                                <li><strong>Metrics Heavy:</strong> Detailed numerical data including battery SOC (kWh), solar output percentage vs yesterday, and full-charge ETA.</li>
                                                                                <li><strong>Home Planner:</strong> Practical lifestyle scheduling highlighting optimal hours to run heavy appliances or charge your EV.</li>
                                                                                <li><strong>Executive Summary:</strong> High-level overview of expected solar harvest and battery status in concise bullet points.</li>
                                                                                <li><strong>Autonomous Pilot:</strong> RateRudder&apos;s AI reasoning explaining the strategy behind today&apos;s battery automation.</li>
                                                                            </ul>
                                                                        </div>
                                                                    }
                                                                />
                                                            </div>
                                                            <Select.Root
                                                                value={currentFlavor}
                                                                onValueChange={(val) => handleUpdateDraft({ morningSummaryFlavor: val as MorningSummaryFlavor })}
                                                            >
                                                                <Select.Trigger className="select-trigger" id="morningSummaryFlavor" aria-label="Morning Summary Flavor">
                                                                    <Select.Value>
                                                                        {flavorPreviews[currentFlavor]?.name || 'Home Planner'}
                                                                    </Select.Value>
                                                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                                        </svg>
                                                                    </Select.Icon>
                                                                </Select.Trigger>
                                                                <Select.Portal>
                                                                    <Select.Positioner className="select-positioner">
                                                                        <Select.Popup className="select-popup">
                                                                            <Select.List>
                                                                                {(Object.keys(flavorPreviews) as MorningSummaryFlavor[]).map((flavorKey) => (
                                                                                    <Select.Item key={flavorKey} className="select-item" value={flavorKey}>
                                                                                        <Select.ItemText>{flavorPreviews[flavorKey].name}</Select.ItemText>
                                                                                    </Select.Item>
                                                                                ))}
                                                                            </Select.List>
                                                                        </Select.Popup>
                                                                    </Select.Positioner>
                                                                </Select.Portal>
                                                            </Select.Root>
                                                        </Field.Root>
                                                    </div>

                                                    {/* Live Notification Preview Box */}
                                                    <div className="notification-preview-box">
                                                        <div className="notification-preview-header">
                                                            <span>Preview on Device</span>
                                                        </div>
                                                        <div className="notification-preview-card">
                                                            <div className="notification-preview-icon">
                                                                <img src="/logo_192.png" alt="RateRudder" />
                                                            </div>
                                                            <div className="notification-preview-text">
                                                                <div className="notification-preview-title">{activePreview.title}</div>
                                                                <div className="notification-preview-body">{activePreview.body}</div>
                                                            </div>
                                                        </div>
                                                    </div>
                                                </div>
                                            )}
                                        </div>

                                        {/* Evening Summary */}
                                        <div className="notif-section">
                                            <Field.Root className="form-group switch-group" style={{ marginBottom: 0 }}>
                                                <div className="switch-row">
                                                    <Switch.Root
                                                        id="eveningSummaryToggle"
                                                        checked={draftSettings.eveningSummaryEnabled ?? false}
                                                        onCheckedChange={(val) => handleUpdateDraft({ eveningSummaryEnabled: val })}
                                                        className="switch-root"
                                                        aria-label="Daily Evening Summary"
                                                    >
                                                        <Switch.Thumb className="switch-thumb" />
                                                    </Switch.Root>
                                                    <Field.Label htmlFor="eveningSummaryToggle" style={{ cursor: 'pointer' }}>
                                                        Daily Evening Summary
                                                    </Field.Label>
                                                    <HelpButton
                                                        title="Evening Summary"
                                                        description={
                                                            <p>
                                                                A daily evening wrap-up sent at your chosen hour detailing total solar generated today, home consumption, grid energy exported, and battery reserve entering the overnight period.
                                                            </p>
                                                        }
                                                    />
                                                </div>
                                                <Field.Description>
                                                    Get an evening digest wrapping up today&apos;s solar performance and overnight battery preparedness.
                                                </Field.Description>
                                            </Field.Root>

                                            {draftSettings.eveningSummaryEnabled && (
                                                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                                                    <div className="notif-row-split">
                                                        {/* Delivery Hour Dropdown */}
                                                        <Field.Root className="form-group compact">
                                                            <Field.Label htmlFor="eveningSummaryHour" style={{ marginBottom: '0.35rem', display: 'block' }}>Delivery Time</Field.Label>
                                                            <Select.Root
                                                                value={String(draftSettings.eveningSummaryHour ?? 20)}
                                                                onValueChange={(val) => handleUpdateDraft({ eveningSummaryHour: parseInt(val as string, 10) })}
                                                            >
                                                                <Select.Trigger className="select-trigger" id="eveningSummaryHour" aria-label="Evening Summary Delivery Time">
                                                                    <Select.Value>
                                                                        {formatHour12(draftSettings.eveningSummaryHour ?? 20)}
                                                                    </Select.Value>
                                                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                                        </svg>
                                                                    </Select.Icon>
                                                                </Select.Trigger>
                                                                <Select.Portal>
                                                                    <Select.Positioner className="select-positioner">
                                                                        <Select.Popup className="select-popup">
                                                                            <Select.List>
                                                                                {Array.from({ length: 24 }, (_, i) => (
                                                                                    <Select.Item key={i} className="select-item" value={String(i)}>
                                                                                        <Select.ItemText>{formatHour12(i)}</Select.ItemText>
                                                                                    </Select.Item>
                                                                                ))}
                                                                            </Select.List>
                                                                        </Select.Popup>
                                                                    </Select.Positioner>
                                                                </Select.Portal>
                                                            </Select.Root>
                                                        </Field.Root>

                                                        {/* Evening Flavor Dropdown */}
                                                        <Field.Root className="form-group compact">
                                                            <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', marginBottom: '0.35rem' }}>
                                                                <Field.Label htmlFor="eveningSummaryFlavor">Summary Flavor</Field.Label>
                                                                <HelpButton
                                                                    title="Evening Summary Flavors"
                                                                    ariaLabel="More info about evening summary flavors"
                                                                    description={
                                                                        <div>
                                                                            <p>Choose how today&apos;s daily wrap-up is delivered:</p>
                                                                            <ul>
                                                                                <li><strong>Metrics Heavy:</strong> Complete end-of-day accounting of solar generated, household energy consumed, grid export, and battery reserve.</li>
                                                                                <li><strong>Home Planner:</strong> Overnight preparedness report advising on evening energy usage until tomorrow&apos;s sunrise.</li>
                                                                                <li><strong>Executive Summary:</strong> Concise recap summarizing total solar harvest and battery reserve entering the night.</li>
                                                                                <li><strong>Autonomous Pilot:</strong> Recap of automation actions taken during peak rate hours and handover to overnight self-consumption.</li>
                                                                            </ul>
                                                                        </div>
                                                                    }
                                                                />
                                                            </div>
                                                            <Select.Root
                                                                value={currentEveningFlavor}
                                                                onValueChange={(val) => handleUpdateDraft({ eveningSummaryFlavor: val as EveningSummaryFlavor })}
                                                            >
                                                                <Select.Trigger className="select-trigger" id="eveningSummaryFlavor" aria-label="Evening Summary Flavor">
                                                                    <Select.Value>
                                                                        {eveningFlavorPreviews[currentEveningFlavor]?.name || 'Home Planner'}
                                                                    </Select.Value>
                                                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                                        </svg>
                                                                    </Select.Icon>
                                                                </Select.Trigger>
                                                                <Select.Portal>
                                                                    <Select.Positioner className="select-positioner">
                                                                        <Select.Popup className="select-popup">
                                                                            <Select.List>
                                                                                {(Object.keys(eveningFlavorPreviews) as EveningSummaryFlavor[]).map((flavorKey) => (
                                                                                    <Select.Item key={flavorKey} className="select-item" value={flavorKey}>
                                                                                        <Select.ItemText>{eveningFlavorPreviews[flavorKey].name}</Select.ItemText>
                                                                                    </Select.Item>
                                                                                ))}
                                                                            </Select.List>
                                                                        </Select.Popup>
                                                                    </Select.Positioner>
                                                                </Select.Portal>
                                                            </Select.Root>
                                                        </Field.Root>
                                                    </div>

                                                    {/* Live Evening Notification Preview Box */}
                                                    <div className="notification-preview-box">
                                                        <div className="notification-preview-header">
                                                            <span>Preview on Device</span>
                                                        </div>
                                                        <div className="notification-preview-card">
                                                            <div className="notification-preview-icon">
                                                                <img src="/logo_192.png" alt="RateRudder" />
                                                            </div>
                                                            <div className="notification-preview-text">
                                                                <div className="notification-preview-title">{activeEveningPreview.title}</div>
                                                                <div className="notification-preview-body">{activeEveningPreview.body}</div>
                                                            </div>
                                                        </div>
                                                    </div>
                                                </div>
                                            )}
                                        </div>

                                        {/* Real-Time Alerts */}
                                        <div className="notif-section">
                                            {/* Grid Outage & Restoration */}
                                            <Field.Root className="form-group switch-group" style={{ marginBottom: 0 }}>
                                                <div className="switch-row">
                                                    <Switch.Root
                                                        id="gridOutageToggle"
                                                        checked={draftSettings.gridOutageAlert ?? false}
                                                        onCheckedChange={(val) => handleUpdateDraft({ gridOutageAlert: val })}
                                                        className="switch-root"
                                                        aria-label="Grid Outage & Restoration"
                                                    >
                                                        <Switch.Thumb className="switch-thumb" />
                                                    </Switch.Root>
                                                    <Field.Label htmlFor="gridOutageToggle" style={{ cursor: 'pointer' }}>
                                                        Grid Outage & Restoration
                                                    </Field.Label>
                                                    <HelpButton
                                                        title="Grid Outage Alerts"
                                                        ariaLabel="More info about grid outage alerts"
                                                        description={
                                                            <div>
                                                                <p>
                                                                    Alerts you when the utility grid goes down for more than 5 minutes (debounced to filter out momentary flickers), reporting your battery reserve and estimated runtime. Sends an all-clear notification when grid power resumes.
                                                                </p>
                                                                <NotificationSample
                                                                    headerLabel="Sample Outage Alert"
                                                                    title="🚨 Grid Outage Detected"
                                                                    body="Utility grid power lost. Home running on battery (82% SOC, ~9.5 hrs remaining)."
                                                                />
                                                                <NotificationSample
                                                                    headerLabel="Sample Restoration Alert"
                                                                    title="✅ Grid Power Restored"
                                                                    body="Utility grid is back online. Battery has resumed normal operation."
                                                                />
                                                            </div>
                                                        }
                                                    />
                                                </div>
                                                <Field.Description>
                                                    Alert me if utility grid power is lost for &gt;5 minutes, and when grid power resumes.
                                                </Field.Description>
                                            </Field.Root>

                                            {/* Price Spike Alert */}
                                            <Field.Root className="form-group" style={{ marginBottom: 0 }}>
                                                <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', marginBottom: '0.35rem' }}>
                                                    <Field.Label htmlFor="priceSpikeSelect">Price Spike Alert</Field.Label>
                                                    <HelpButton
                                                        title="Price Spike Alerts"
                                                        ariaLabel="More info about price spike alerts"
                                                        description={
                                                            <div>
                                                                <p>
                                                                    Advance alert sent ~1 hour before an upcoming real-time price surge. RateRudder compares rates against recent days at that hour to filter out routine scheduled price changes.
                                                                </p>
                                                                <ul>
                                                                    <li><strong>Low Sensitivity:</strong> Alerts only during major, extreme rate surges.</li>
                                                                    <li><strong>Medium Sensitivity:</strong> Recommended; alerts during notable, unexpected price spikes.</li>
                                                                    <li><strong>High Sensitivity:</strong> Alerts earlier on smaller rate increases for maximum advance notice.</li>
                                                                </ul>
                                                                <NotificationSample
                                                                    headerLabel="Sample Price Spike Alert"
                                                                    title="⚡ Price Spike Alert: $0.45/kWh Expected"
                                                                    body="Upcoming rate at 2:00 PM is $0.45/kWh (unusually high vs recent days). Battery will prioritize home loads to shield from peak cost."
                                                                />
                                                            </div>
                                                        }
                                                    />
                                                </div>
                                                <Select.Root
                                                    value={draftSettings.priceSpikeAlert || 'disabled'}
                                                    onValueChange={(val) => handleUpdateDraft({ priceSpikeAlert: (val === 'disabled' ? '' : val) as AnomalyAlertSensitivity })}
                                                >
                                                    <Select.Trigger className="select-trigger" id="priceSpikeSelect" aria-label="Real-Time Price Spike Alert">
                                                        <Select.Value placeholder="Select sensitivity...">
                                                            {priceSpikeLabels[draftSettings.priceSpikeAlert || ''] || 'Disabled'}
                                                        </Select.Value>
                                                        <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                            </svg>
                                                        </Select.Icon>
                                                    </Select.Trigger>
                                                    <Select.Portal>
                                                        <Select.Positioner className="select-positioner">
                                                            <Select.Popup className="select-popup">
                                                                <Select.List>
                                                                    <Select.Item className="select-item" value="disabled">
                                                                        <Select.ItemText>Disabled</Select.ItemText>
                                                                    </Select.Item>
                                                                    <Select.Item className="select-item" value="low">
                                                                        <Select.ItemText>Low Sensitivity</Select.ItemText>
                                                                    </Select.Item>
                                                                    <Select.Item className="select-item" value="medium">
                                                                        <Select.ItemText>Medium Sensitivity</Select.ItemText>
                                                                    </Select.Item>
                                                                    <Select.Item className="select-item" value="high">
                                                                        <Select.ItemText>High Sensitivity</Select.ItemText>
                                                                    </Select.Item>
                                                                </Select.List>
                                                            </Select.Popup>
                                                        </Select.Positioner>
                                                    </Select.Portal>
                                                </Select.Root>
                                                <Field.Description>
                                                    Advance 1-hour warning when electricity prices surge abnormally.
                                                </Field.Description>
                                            </Field.Root>

                                            {/* Solar Underproduction Alert */}
                                            <Field.Root className="form-group" style={{ marginBottom: 0 }}>
                                                <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', marginBottom: '0.35rem' }}>
                                                    <Field.Label htmlFor="solarUnderproductionSelect">Solar Underproduction Alert</Field.Label>
                                                    <HelpButton
                                                        title="Solar Underproduction Alerts"
                                                        ariaLabel="More info about solar underproduction alerts"
                                                        description={
                                                            <div>
                                                                <p>
                                                                    Midday check alerting you when solar generation falls substantially below weather forecast expectations. Automatically suppressed during overcast weather, storms, or equipment alarms.
                                                                </p>
                                                                <ul>
                                                                    <li><strong>Low Sensitivity:</strong> Alerts only during severe, unexpected generation shortfalls.</li>
                                                                    <li><strong>Medium Sensitivity:</strong> Recommended; alerts when generation is noticeably below expected levels.</li>
                                                                    <li><strong>High Sensitivity:</strong> Alerts on moderate underproduction for closer system monitoring.</li>
                                                                </ul>
                                                                <NotificationSample
                                                                    headerLabel="Sample Solar Underproduction Alert"
                                                                    title="⚠️ Solar Underproduction Alert"
                                                                    body="Current generation (1.2 kW) is significantly below the 5.4 kW forecast for 1:00 PM under clear sky conditions."
                                                                />
                                                            </div>
                                                        }
                                                    />
                                                </div>
                                                <Select.Root
                                                    value={draftSettings.solarUnderproductionAlert || 'disabled'}
                                                    onValueChange={(val) => handleUpdateDraft({ solarUnderproductionAlert: (val === 'disabled' ? '' : val) as AnomalyAlertSensitivity })}
                                                >
                                                    <Select.Trigger className="select-trigger" id="solarUnderproductionSelect" aria-label="Unexpected Solar Underproduction">
                                                        <Select.Value placeholder="Select sensitivity...">
                                                            {solarUnderproductionLabels[draftSettings.solarUnderproductionAlert || ''] || 'Disabled'}
                                                        </Select.Value>
                                                        <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                            </svg>
                                                        </Select.Icon>
                                                    </Select.Trigger>
                                                    <Select.Portal>
                                                        <Select.Positioner className="select-positioner">
                                                            <Select.Popup className="select-popup">
                                                                <Select.List>
                                                                    <Select.Item className="select-item" value="disabled">
                                                                        <Select.ItemText>Disabled</Select.ItemText>
                                                                    </Select.Item>
                                                                    <Select.Item className="select-item" value="low">
                                                                        <Select.ItemText>Low Sensitivity</Select.ItemText>
                                                                    </Select.Item>
                                                                    <Select.Item className="select-item" value="medium">
                                                                        <Select.ItemText>Medium Sensitivity</Select.ItemText>
                                                                    </Select.Item>
                                                                    <Select.Item className="select-item" value="high">
                                                                        <Select.ItemText>High Sensitivity</Select.ItemText>
                                                                    </Select.Item>
                                                                </Select.List>
                                                            </Select.Popup>
                                                        </Select.Positioner>
                                                    </Select.Portal>
                                                </Select.Root>
                                                <Field.Description>
                                                    Alert me if midday solar falls significantly below weather forecast.
                                                </Field.Description>
                                            </Field.Root>

                                            {/* Unplanned VPP Dispatch */}
                                            <Field.Root className="form-group switch-group" style={{ marginBottom: 0 }}>
                                                <div className="switch-row">
                                                    <Switch.Root
                                                        id="vppDispatchToggle"
                                                        checked={draftSettings.vppDispatchAlert ?? false}
                                                        onCheckedChange={(val) => handleUpdateDraft({ vppDispatchAlert: val })}
                                                        className="switch-root"
                                                        aria-label="Unplanned VPP Grid Support Dispatch"
                                                    >
                                                        <Switch.Thumb className="switch-thumb" />
                                                    </Switch.Root>
                                                    <Field.Label htmlFor="vppDispatchToggle" style={{ cursor: 'pointer' }}>
                                                        Unplanned VPP Grid Support Dispatch
                                                    </Field.Label>
                                                    <HelpButton
                                                        title="VPP Dispatch Alerts"
                                                        ariaLabel="More info about VPP dispatch alerts"
                                                        description={
                                                            <div>
                                                                <p>
                                                                    Alerts you immediately when your battery begins discharging to support the grid during an unscheduled demand response or Virtual Power Plant event.
                                                                </p>
                                                                <NotificationSample
                                                                    headerLabel="Sample VPP Dispatch Alert"
                                                                    title="🔋 Virtual Power Plant Event Active"
                                                                    body="Battery is discharging to support the grid during an unscheduled demand response event."
                                                                />
                                                            </div>
                                                        }
                                                    />
                                                </div>
                                                <Field.Description>
                                                    Alert me when battery discharges during an unscheduled grid support event.
                                                </Field.Description>
                                            </Field.Root>
                                        </div>
                                    </div>
                                )}
                            </>
                        )}
                    </div>

                    {/* Modal Footer Action Bar */}
                    <div className="modal-footer">
                        <div className="modal-footer-left">
                            {/* TODO: Remove test notification button before public release */}
                            {isSubscribedLocally && (
                                <>
                                    <button
                                        type="button"
                                        className="btn btn-secondary"
                                        onClick={handleSendTest}
                                        disabled={testing}
                                    >
                                        {testing ? 'Sending Push...' : 'Send Test Notification'}
                                    </button>
                                    {testSuccess && (
                                        <span style={{ color: 'var(--success-color, #10b981)', fontSize: '0.85rem', fontWeight: 500 }}>
                                            ✓ Notification sent!
                                        </span>
                                    )}
                                </>
                            )}
                        </div>
                        <div className="modal-footer-right">
                            <button
                                type="button"
                                className="btn btn-secondary"
                                onClick={handleCancel}
                                disabled={saving}
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                className="btn btn-primary"
                                onClick={handleSavePreferences}
                                disabled={saving || loading || !siteID}
                            >
                                {saving ? 'Saving...' : 'Save Preferences'}
                            </button>
                        </div>
                    </div>
                </Dialog.Popup>
            </Dialog.Portal>
        </Dialog.Root>
    );
};
