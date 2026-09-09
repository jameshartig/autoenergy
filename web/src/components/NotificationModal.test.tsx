import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { NotificationModal } from './NotificationModal';
import * as api from '../api';

vi.mock('../api');

describe('NotificationModal', () => {
    const mockSiteID = 'test-site-123';
    const mockSiteName = 'Home Battery';
    const mockOnClose = vi.fn();

    let mockPushSubscription: any;
    let mockPushManager: any;
    let mockServiceWorker: any;

    beforeEach(() => {
        vi.resetAllMocks();

        mockPushSubscription = {
            endpoint: 'https://fcm.googleapis.com/fcm/send/test-sub-1',
            toJSON: vi.fn(() => ({
                endpoint: 'https://fcm.googleapis.com/fcm/send/test-sub-1',
                keys: {
                    p256dh: 'test-p256dh-key',
                    auth: 'test-auth-key',
                },
            })),
            getKey: vi.fn((keyName: string) => {
                if (keyName === 'p256dh') return new Uint8Array([1, 2, 3]).buffer;
                if (keyName === 'auth') return new Uint8Array([4, 5, 6]).buffer;
                return null;
            }),
            unsubscribe: vi.fn().mockResolvedValue(true),
        };

        mockPushManager = {
            getSubscription: vi.fn().mockResolvedValue(null),
            subscribe: vi.fn().mockResolvedValue(mockPushSubscription),
        };

        mockServiceWorker = {
            ready: Promise.resolve({
                pushManager: mockPushManager,
            }),
        };

        Object.defineProperty(navigator, 'serviceWorker', {
            value: mockServiceWorker,
            configurable: true,
        });

        (window as any).PushManager = {};

        (window as any).Notification = {
            permission: 'default',
            requestPermission: vi.fn().mockResolvedValue('granted'),
        };

        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: false,
                morningSummaryHour: 7,
                morningSummaryFlavor: 'home_planner',
                eveningSummaryEnabled: false,
                eveningSummaryHour: 20,
                eveningSummaryFlavor: 'home_planner',
                gridOutageAlert: false,
                priceSpikeAlert: '',
                solarUnderproductionAlert: '',
                vppDispatchAlert: false,
            },
            subscriptions: [],
            vapidEnabled: true,
        });

        (api.fetchVAPIDPublicKey as any).mockResolvedValue(new ArrayBuffer(65));
        (api.subscribePushNotification as any).mockResolvedValue(undefined);
        (api.unsubscribePushNotification as any).mockResolvedValue(undefined);
        (api.updateNotificationSettings as any).mockResolvedValue(undefined);
    });

    it('renders dialog with loading and then device connect prompt when 0 devices are connected', async () => {
        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        expect(screen.getByText(/Loading notification preferences/i)).toBeInTheDocument();

        await waitFor(() => {
            expect(screen.getByText('Notifications')).toBeInTheDocument();
            expect(screen.getByText(/Configuring alerts for Home Battery/i)).toBeInTheDocument();
            expect(screen.getByText(/Deliver notifications to this browser/i)).toBeInTheDocument();
            expect(screen.getByText(/Connect this browser above/i)).toBeInTheDocument();
            // Zone B alert options hidden when no devices connected
            expect(screen.queryByText('Daily Morning Summary')).not.toBeInTheDocument();
        });
    });

    it('displays warning banner when VAPID is not configured on server', async () => {
        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {},
            subscriptions: [],
            vapidEnabled: false,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByText(/Web Push Not Configured/i)).toBeInTheDocument();
        });
    });

    it('subscribes current browser when toggle is clicked', async () => {
        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByRole('switch', { name: /Deliver notifications to this browser/i })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('switch', { name: /Deliver notifications to this browser/i }));

        await waitFor(() => {
            expect(api.fetchVAPIDPublicKey).toHaveBeenCalled();
            expect(mockPushManager.subscribe).toHaveBeenCalled();
            expect(api.subscribePushNotification).toHaveBeenCalledWith(
                expect.objectContaining({
                    endpoint: 'https://fcm.googleapis.com/fcm/send/test-sub-1',
                }),
                true
            );
        });
    });

    it('shows multi-device clarity banner and alert options when a device is connected', async () => {
        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: true,
                morningSummaryHour: 8,
                morningSummaryFlavor: 'home_planner',
                eveningSummaryEnabled: true,
                eveningSummaryHour: 21,
                eveningSummaryFlavor: 'metrics_heavy',
                gridOutageAlert: true,
                priceSpikeAlert: 'medium',
                solarUnderproductionAlert: 'low',
                vppDispatchAlert: false,
            },
            subscriptions: [
                {
                    id: 'sub-iphone',
                    endpoint: 'https://fcm.googleapis.com/fcm/send/sub-iphone',
                    keys: { p256dh: 'k1', auth: 'a1' },
                    userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)',
                    tsCreated: '2026-09-02T08:00:00Z',
                },
            ],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            const banner = screen.getByTestId('devices-clarity-banner');
            expect(banner).toBeInTheDocument();
            expect(banner).toHaveTextContent(/Applies to all your connected devices for Home Battery/i);
            expect(banner).toHaveTextContent(/Other members on this site configure their own notifications/i);

            expect(screen.getByText('Daily Morning Summary')).toBeInTheDocument();
            expect(screen.getByText('Daily Evening Summary')).toBeInTheDocument();
            expect(screen.getByText('Grid Outage & Restoration')).toBeInTheDocument();
            expect(screen.getByLabelText('Real-Time Price Spike Alert')).toBeInTheDocument();
            expect(screen.getByLabelText('Unexpected Solar Underproduction')).toBeInTheDocument();
            expect(screen.getByText('Unplanned VPP Grid Support Dispatch')).toBeInTheDocument();
        });
    });

    it('stages settings locally without calling updateNotificationSettings on toggle', async () => {
        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: false,
                morningSummaryHour: 7,
                morningSummaryFlavor: 'metrics_heavy',
                gridOutageAlert: false,
            },
            subscriptions: [
                {
                    id: 'sub-iphone',
                    endpoint: 'https://fcm.googleapis.com/fcm/send/sub-iphone',
                    keys: { p256dh: 'k1', auth: 'a1' },
                    userAgent: 'iPhone',
                },
            ],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByRole('switch', { name: /Daily Morning Summary/i })).toBeInTheDocument();
        });

        // Toggle morning summary ON
        fireEvent.click(screen.getByRole('switch', { name: /Daily Morning Summary/i }));

        // Toggle grid outage ON
        fireEvent.click(screen.getByRole('switch', { name: /Grid Outage & Restoration/i }));

        // Verify API was NOT called yet!
        expect(api.updateNotificationSettings).not.toHaveBeenCalled();
    });

    it('saves staged preferences and closes modal when Save Preferences is clicked', async () => {
        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: false,
                morningSummaryHour: 7,
                morningSummaryFlavor: 'metrics_heavy',
                gridOutageAlert: false,
            },
            subscriptions: [
                {
                    id: 'sub-iphone',
                    endpoint: 'https://fcm.googleapis.com/fcm/send/sub-iphone',
                    keys: { p256dh: 'k1', auth: 'a1' },
                    userAgent: 'iPhone',
                },
            ],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByRole('switch', { name: /Daily Morning Summary/i })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('switch', { name: /Daily Morning Summary/i }));
        fireEvent.click(screen.getByRole('switch', { name: /Grid Outage & Restoration/i }));

        // Click Save Preferences
        fireEvent.click(screen.getByRole('button', { name: /Save Preferences/i }));

        await waitFor(() => {
            expect(api.updateNotificationSettings).toHaveBeenCalledWith(
                mockSiteID,
                expect.objectContaining({
                    morningSummaryEnabled: true,
                    gridOutageAlert: true,
                })
            );
            expect(mockOnClose).toHaveBeenCalled();
        });
    });

    it('discards staged changes and closes without saving when Cancel is clicked', async () => {
        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: false,
                morningSummaryHour: 7,
                morningSummaryFlavor: 'metrics_heavy',
            },
            subscriptions: [
                {
                    id: 'sub-iphone',
                    endpoint: 'https://fcm.googleapis.com/fcm/send/sub-iphone',
                    keys: { p256dh: 'k1', auth: 'a1' },
                    userAgent: 'iPhone',
                },
            ],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByRole('switch', { name: /Daily Morning Summary/i })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('switch', { name: /Daily Morning Summary/i }));

        // Click Cancel
        fireEvent.click(screen.getByRole('button', { name: /Cancel/i }));

        expect(api.updateNotificationSettings).not.toHaveBeenCalled();
        expect(mockOnClose).toHaveBeenCalled();
    });

    it('allows sending a test push notification when subscribed locally', async () => {
        mockPushManager.getSubscription.mockResolvedValue(mockPushSubscription);

        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {},
            subscriptions: [
                {
                    id: 'sub-local',
                    endpoint: 'https://fcm.googleapis.com/fcm/send/test-sub-1',
                    keys: { p256dh: 'k1', auth: 'a1' },
                    userAgent: 'Chrome',
                },
            ],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /Send Test Notification/i })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: /Send Test Notification/i }));

        await waitFor(() => {
            expect(api.subscribePushNotification).toHaveBeenCalledWith(
                expect.objectContaining({
                    endpoint: 'https://fcm.googleapis.com/fcm/send/test-sub-1',
                }),
                true
            );
            expect(screen.getByText(/✓ Notification sent!/i)).toBeInTheDocument();
        });
    });

    it('allows removing an active device from subscriptions list', async () => {
        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {},
            subscriptions: [
                {
                    id: 'sub-1',
                    endpoint: 'https://fcm.googleapis.com/fcm/send/phone',
                    keys: { p256dh: 'k1', auth: 'a1' },
                    userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)',
                },
            ],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('Safari on iPhone')).toBeInTheDocument();
            expect(screen.getByRole('button', { name: /Remove/i })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: /Remove/i }));

        await waitFor(() => {
            expect(api.unsubscribePushNotification).toHaveBeenCalledWith('https://fcm.googleapis.com/fcm/send/phone');
        });
    });

    it('renders side-by-side Delivery Time and Summary Flavor selects when morning summary is enabled', async () => {
        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: true,
                morningSummaryHour: 7,
                morningSummaryFlavor: 'metrics_heavy',
                eveningSummaryEnabled: true,
                eveningSummaryHour: 20,
                eveningSummaryFlavor: 'executive',
            },
            subscriptions: [
                {
                    id: 'sub-1',
                    endpoint: 'https://fcm.googleapis.com/fcm/send/phone',
                    keys: { p256dh: 'k1', auth: 'a1' },
                    userAgent: 'Chrome on Mac',
                },
            ],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Morning Summary Delivery Time')).toBeInTheDocument();
            expect(screen.getByLabelText('Morning Summary Delivery Time')).toHaveTextContent('7:00 AM');
            expect(screen.getByLabelText('Morning Summary Flavor')).toBeInTheDocument();
            expect(screen.getByLabelText('Evening Summary Delivery Time')).toBeInTheDocument();
            expect(screen.getByLabelText('Evening Summary Delivery Time')).toHaveTextContent('8:00 PM');
            expect(screen.getByLabelText('Evening Summary Flavor')).toBeInTheDocument();
            expect(screen.getAllByText('Preview on Device').length).toBe(2);
        });
    });

    it('unsubscribes local browser, turns off switch, and hides alert options when current device is removed', async () => {
        mockPushManager.getSubscription.mockResolvedValue(mockPushSubscription);

        (api.fetchNotificationSettings as any)
            .mockResolvedValueOnce({
                settings: { morningSummaryEnabled: true },
                subscriptions: [
                    {
                        id: 'sub-local',
                        endpoint: 'https://fcm.googleapis.com/fcm/send/test-sub-1',
                        keys: { p256dh: 'k1', auth: 'a1' },
                        userAgent: 'Chrome on Windows',
                    },
                ],
                vapidEnabled: true,
            })
            .mockResolvedValueOnce({
                settings: { morningSummaryEnabled: true },
                subscriptions: [],
                vapidEnabled: true,
            });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByRole('switch', { name: /Deliver notifications to this browser/i })).toBeChecked();
            expect(screen.getByText('Daily Morning Summary')).toBeInTheDocument();
            expect(screen.getByRole('button', { name: /Remove/i })).toBeInTheDocument();
        });

        // When remove is called on the local browser
        mockPushSubscription.unsubscribe.mockImplementation(async () => {
            mockPushManager.getSubscription.mockResolvedValue(null);
            return true;
        });
        fireEvent.click(screen.getByRole('button', { name: /Remove/i }));

        await waitFor(() => {
            expect(mockPushSubscription.unsubscribe).toHaveBeenCalled();
            expect(api.unsubscribePushNotification).toHaveBeenCalledWith('https://fcm.googleapis.com/fcm/send/test-sub-1');
            expect(screen.getByRole('switch', { name: /Deliver notifications to this browser/i })).not.toBeChecked();
            expect(screen.queryByText('Daily Morning Summary')).not.toBeInTheDocument();
            expect(screen.getByText(/Connect this browser above/i)).toBeInTheDocument();
        });
    });

    it('saves preferences with valid defaults after removing the last browser without enabling this browser', async () => {
        mockPushManager.getSubscription.mockResolvedValue(null);

        (api.fetchNotificationSettings as any)
            .mockResolvedValueOnce({
                settings: {
                    morningSummaryEnabled: false,
                    morningSummaryHour: 0,
                    morningSummaryFlavor: '',
                    eveningSummaryEnabled: false,
                    eveningSummaryHour: 0,
                    eveningSummaryFlavor: '',
                },
                subscriptions: [
                    {
                        id: 'sub-remote',
                        endpoint: 'https://fcm.googleapis.com/fcm/send/phone-1',
                        keys: { p256dh: 'k1', auth: 'a1' },
                        userAgent: 'Safari on iPhone',
                    },
                ],
                vapidEnabled: true,
            })
            .mockResolvedValueOnce({
                settings: {
                    morningSummaryEnabled: false,
                    morningSummaryHour: 0,
                    morningSummaryFlavor: '',
                },
                subscriptions: [],
                vapidEnabled: true,
            });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('Safari on iPhone')).toBeInTheDocument();
            expect(screen.getByRole('button', { name: /Remove/i })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: /Remove/i }));

        await waitFor(() => {
            expect(api.unsubscribePushNotification).toHaveBeenCalledWith('https://fcm.googleapis.com/fcm/send/phone-1');
            expect(screen.getByText(/Connect this browser above/i)).toBeInTheDocument();
        });

        const saveButton = screen.getByRole('button', { name: /Save Preferences/i });
        fireEvent.click(saveButton);

        await waitFor(() => {
            expect(api.updateNotificationSettings).toHaveBeenCalledWith(
                mockSiteID,
                expect.objectContaining({
                    morningSummaryFlavor: 'home_planner',
                    eveningSummaryFlavor: 'home_planner',
                    morningSummaryHour: 7,
                    eveningSummaryHour: 20,
                    morningSummaryEnabled: false,
                    eveningSummaryEnabled: false,
                })
            );
            expect(mockOnClose).toHaveBeenCalled();
            expect(screen.queryByText(/morning summary flavor is required/i)).not.toBeInTheDocument();
        });
    });

    it('shows iOS Safari guidance with share icon and hides browser toggle when on iOS in browser', async () => {
        vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
        delete (window as any).Notification;
        delete (window as any).PushManager;
        window.matchMedia = vi.fn().mockImplementation((query) => ({
            matches: false,
            media: query,
            onchange: null,
            addListener: vi.fn(),
            removeListener: vi.fn(),
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
            dispatchEvent: vi.fn(),
        }));
        Object.defineProperty(navigator, 'standalone', { value: false, configurable: true });

        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: false,
                morningSummaryHour: 7,
                morningSummaryFlavor: 'home_planner',
                eveningSummaryEnabled: false,
                eveningSummaryHour: 20,
                eveningSummaryFlavor: 'home_planner',
            },
            subscriptions: [],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            const banner = screen.getByText('iOS Safari Push Setup').closest('.notification-banner-content');
            expect(banner).toBeInTheDocument();
            expect(banner).toHaveTextContent(/three-dot menu \(⋯\)/);
            expect(banner).toHaveTextContent(/Share/);
            expect(banner).toHaveTextContent(/View More/);
            expect(banner).toHaveTextContent(/Add to Home Screen/);
            expect(screen.getByLabelText('Share')).toBeInTheDocument();

            // Toggle should not be rendered
            expect(screen.queryByText(/Deliver notifications to this browser/i)).not.toBeInTheDocument();

            // Empty device guidance
            expect(screen.getByText(/Add RateRudder to your Home Screen to register this device/i)).toBeInTheDocument();
            expect(screen.getByText('Add to Home Screen to get started')).toBeInTheDocument();
        });
    });

    it('hides iOS guidance banner and shows browser toggle when installed as an iOS standalone PWA', async () => {
        vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
        window.matchMedia = vi.fn().mockImplementation((query) => ({
            matches: query === '(display-mode: standalone)',
            media: query,
            onchange: null,
            addListener: vi.fn(),
            removeListener: vi.fn(),
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
            dispatchEvent: vi.fn(),
        }));
        Object.defineProperty(navigator, 'standalone', { value: true, configurable: true });

        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {
                morningSummaryEnabled: false,
                morningSummaryHour: 7,
                morningSummaryFlavor: 'home_planner',
                eveningSummaryEnabled: false,
                eveningSummaryHour: 20,
                eveningSummaryFlavor: 'home_planner',
            },
            subscriptions: [],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.queryByText('iOS Safari Push Setup')).not.toBeInTheDocument();
            expect(screen.getByText(/Deliver notifications to this browser/i)).toBeInTheDocument();
        });
    });

    it('shows warning banner and hides toggle when push notifications are unsupported in browser', async () => {
        delete (window as any).PushManager;

        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {},
            subscriptions: [],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByTestId('push-unsupported-banner')).toBeInTheDocument();
            expect(screen.getByText('Push notifications are not supported in this browser.')).toBeInTheDocument();
            expect(screen.queryByText(/Deliver notifications to this browser/i)).not.toBeInTheDocument();
            expect(screen.getByText('Push notifications unsupported')).toBeInTheDocument();
        });
    });

    it('shows warning banner and hides iOS guide when push is unsupported on iOS (missing serviceWorker)', async () => {
        vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X)');
        delete (navigator as any).serviceWorker;

        (api.fetchNotificationSettings as any).mockResolvedValue({
            settings: {},
            subscriptions: [],
            vapidEnabled: true,
        });

        render(
            <NotificationModal
                open={true}
                onClose={mockOnClose}
                siteID={mockSiteID}
                siteName={mockSiteName}
            />
        );

        await waitFor(() => {
            expect(screen.getByTestId('push-unsupported-banner')).toBeInTheDocument();
            expect(screen.queryByText('iOS Safari Push Setup')).not.toBeInTheDocument();
            expect(screen.queryByText(/Deliver notifications to this browser/i)).not.toBeInTheDocument();
        });
    });
});
