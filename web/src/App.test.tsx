import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import userEvent from '@testing-library/user-event';
import ActionList from './ActionList';
import App from './App';
import { BrowserRouter } from 'react-router-dom';
import { fetchActions, fetchSavings, fetchAuthStatus, fetchSettings, updateSettings, login, logout } from './api';

// Mock the API
vi.mock('./api', () => ({
    fetchActions: vi.fn(),
    fetchSavings: vi.fn(),
    fetchAuthStatus: vi.fn(),
    fetchSettings: vi.fn(),
    updateSettings: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    BatteryMode: {
        NoChange: 0,
        Standby: 1,
        ChargeAny: 2,
        ChargeSolar: 3,
        Load: -1,
    },
    SolarMode: {
        NoChange: 0,
        NoExport: 1,
        Any: 2,
    },
}));

// Mock Google OAuth
vi.mock('@react-oauth/google', () => ({
    GoogleOAuthProvider: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    GoogleLogin: ({ onSuccess }: { onSuccess: (res: any) => void }) => (
        <button onClick={() => onSuccess({ credential: 'test-token' })}>
            Google Sign In
        </button>
    ),
}));

const renderWithRouter = (component: React.ReactNode) => {
    return render(
        <BrowserRouter>
            {component}
        </BrowserRouter>
    );
};

describe('ActionList', () => {
    beforeEach(() => {
        vi.resetAllMocks();
    });

    it('renders loading state initially', () => {
        (fetchActions as any).mockReturnValueOnce(new Promise(() => {}));
        renderWithRouter(<ActionList />);
        expect(screen.getByText('Loading day...')).toBeInTheDocument();
    });

    it('renders actions when loaded', async () => {
        const actions = [{
            description: 'This is a test',
            timestamp: new Date().toISOString(),
            batteryMode: 1,
            solarMode: 1,
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<ActionList />);

        await waitFor(() => {
            const standbyElements = screen.getAllByText('Hold Battery');
            expect(standbyElements.length).toBeGreaterThan(0);
            expect(screen.getByText('This is a test')).toBeInTheDocument();
        });
    });

    it('renders no actions message when empty', async () => {
        (fetchActions as any).mockResolvedValue([]);
        renderWithRouter(<ActionList />);
        await waitFor(() => {
            expect(screen.getByText('No actions recorded for this day.')).toBeInTheDocument();
        });
    });

    it('navigates to previous day', async () => {
         const user = userEvent.setup();
         (fetchActions as any).mockResolvedValue([]);
         renderWithRouter(<ActionList />);

         await waitFor(() => {
             expect(screen.getByText(/Prev/)).toBeInTheDocument();
             expect(screen.getByText(/Prev/)).not.toBeDisabled();
         });

         const prevButton = screen.getByText(/Prev/);
         await user.click(prevButton);

         await waitFor(() => {
             const calls = (fetchActions as any).mock.calls;
             if (calls.length < 2) throw new Error('fetchActions not called twice');
             const lastCall = calls[calls.length - 1];
             const startArg = lastCall[0] as Date;
             const now = new Date();
             const expectedDate = new Date(now);
             expectedDate.setDate(expectedDate.getDate() - 1);
             expect(startArg.getDate()).toBe(expectedDate.getDate());
         });
    });

    it('renders dry run badge', async () => {
        const actions = [{
            description: 'Dry run test',
            timestamp: new Date().toISOString(),
            batteryMode: 1, // Standby
            solarMode: 1, // NoExport
            dryRun: true,
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<ActionList />);

        await waitFor(() => {
            expect(screen.getByText('Dry Run')).toBeInTheDocument();
            expect(screen.getByText('Dry Run')).toHaveClass('tag', 'dry-run');
        });
    });

    it('hides no change badges', async () => {
        const actions = [{
            description: 'Mixed modes test',
            timestamp: new Date().toISOString(),
            batteryMode: 0, // NoChange
            solarMode: 1, // NoExport
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<ActionList />);

        await waitFor(() => {
            // Solar mode should be visible
            expect(screen.getByText('No Export')).toBeInTheDocument();
            // Battery mode (NoChange) should NOT be visible as a badge/tag
            // However, the label might be used elsewhere?
            // In ActionList.tsx: <h3>{getBatteryModeLabel(action.batteryMode)}</h3> renders the label in h3.
            // But the badges are in .tags span.

            // Let's check specifically for the badge
            const badges = screen.queryAllByText((content, element) => {
                return element !== null && element.classList.contains('tag') && content === 'No Change';
            });
            expect(badges.length).toBe(0);
        });
    });

    it('groups consecutive no change actions into summary', async () => {
        const actions = [
            {
                description: 'No change 1',
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0, // NoChange
                solarMode: 0, // NoChange
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
            },
            {
                description: 'No change 2',
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.20, tsStart: '', tsEnd: '' }
            }
        ];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<ActionList />);

        await waitFor(() => {
            // Should show "No Change" title/header
            expect(screen.getByRole('heading', { name: /No Change/ })).toBeInTheDocument();
            // Should show average price: (0.10 + 0.20) / 2 = 0.15
            expect(screen.getByText(/Avg Price:/)).toBeInTheDocument();
            expect(screen.getByText(/\$0.150\/kWh/)).toBeInTheDocument();
            // Should show range: 0.10 - 0.20
            expect(screen.getByText(/Range: \$0.100 - \$0.200/)).toBeInTheDocument();
            // Should show count in title
            expect(screen.getByText('(2x)')).toBeInTheDocument();
        });
    });

    it('renders daily savings summary', async () => {
        (fetchActions as any).mockResolvedValue([]);
        (fetchSavings as any).mockResolvedValue({
            batterySavings: 5.50,
            solarSavings: 5.00,
            cost: 2.00,
            credit: 1.00,
            avoidedCost: 6.00,
            chargingCost: 0.50,
            solarGenerated: 20,
            gridImported: 10,
            gridExported: 5,
            homeUsed: 25,
            batteryUsed: 10
        });

        renderWithRouter(<ActionList />);

        await waitFor(() => {
            expect(screen.getByText('Daily Overview')).toBeInTheDocument();
            expect(screen.getByText('Net Savings')).toBeInTheDocument();
            expect(screen.getByText('$10.50')).toBeInTheDocument();
            expect(screen.getByText('Solar Savings')).toBeInTheDocument();
            expect(screen.getByText('$5.00')).toBeInTheDocument();
            expect(screen.getByText('Battery Savings')).toBeInTheDocument();
            expect(screen.getByText('$5.50')).toBeInTheDocument();
        });
    });
});

describe('App & Settings', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        // Default mocks
        (fetchActions as any).mockResolvedValue([]);
        (fetchSettings as any).mockResolvedValue({
            dryRun: false,
            pause: false,
            minBatterySOC: 10,
        });
    });

    const defaultAuthStatus = {
        isAdmin: false,
        loggedIn: true,
        authRequired: true,
        clientID: 'test-client-id',
        email: 'user@example.com'
    };

    it('shows login button when auth required and not logged in', async () => {
        (fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: false
        });

        render(<App />);

        await waitFor(() => {
            expect(screen.getByText('Google Sign In')).toBeInTheDocument();
        });
    });

    it('calls login api on successful google login', async () => {
         (fetchAuthStatus as any).mockResolvedValueOnce({
            ...defaultAuthStatus,
            loggedIn: false
        }).mockResolvedValueOnce({
            ...defaultAuthStatus,
            loggedIn: true
        });

        (login as any).mockResolvedValue(undefined);

        render(<App />);

        await waitFor(() => {
            expect(screen.getByText('Google Sign In')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Google Sign In'));

        await waitFor(() => {
            expect(login).toHaveBeenCalledWith('test-token');
        });
    });

    it('shows logout button when logged in and calls logout on click', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });
        (logout as any).mockResolvedValue(undefined);

        render(<App />);

        await waitFor(() => {
            expect(screen.getByText('Logout')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Logout'));

        await waitFor(() => {
            expect(logout).toHaveBeenCalled();
        });
    });

    it('shows settings link when not admin', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, isAdmin: false });

        render(<App />);

        await waitFor(() => {
            expect(screen.getByText('Settings')).toBeInTheDocument();
        });
    });

    it('settings page is read-only when not admin', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, isAdmin: false });
        render(<App />);

        // Navigate
        await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
        fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

        // Check button
        await waitFor(() => {
            const btn = screen.getByText('Read Only');
            expect(btn).toBeInTheDocument();
            expect(btn).toBeDisabled();
        });
    });

    it('shows settings link when admin', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, isAdmin: true });

        render(<App />);

        await waitFor(() => {
            expect(screen.getByText('Settings')).toBeInTheDocument();
        });
    });

    it('navigates to settings and loads data', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, isAdmin: true });

        render(<App />);

        // Wait for link to appear
        await waitFor(() => {
            expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument();
        });

        // Click settings
        fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

        // Check if settings component loaded and fetched data
        await waitFor(() => {
            expect(screen.getByLabelText(/Min Battery SOC/i)).toBeInTheDocument();
            expect(screen.getByDisplayValue('10')).toBeInTheDocument();
        });
    });

    it('can update settings', async () => {
         (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, isAdmin: true });
         render(<App />);

         // Navigate
         await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
         fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

         // Change input
         await waitFor(() => expect(screen.getByLabelText(/Min Battery SOC/i)).toBeInTheDocument());
         const input = screen.getByLabelText(/Min Battery SOC/i);
         fireEvent.change(input, { target: { value: '20' } });

         // Mock update success
         (updateSettings as any).mockResolvedValue(undefined);

         // Helper to click save
         const saveBtn = screen.getByText('Save Settings');
         fireEvent.click(saveBtn);

         await waitFor(() => {
             expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 minBatterySOC: 20
             }));
         });
    });

    it('can toggle pause setting', async () => {
         (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, isAdmin: true });
         render(<App />);

         // Navigate
         await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
         fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

         // Toggle Pause
         await waitFor(() => expect(screen.getByLabelText(/Pause Updates/i)).toBeInTheDocument());
         const input = screen.getByLabelText(/Pause Updates/i);
         fireEvent.click(input);

         // Mock update success
         (updateSettings as any).mockResolvedValue(undefined);

         // Helper to click save
         const saveBtn = screen.getByText('Save Settings');
         fireEvent.click(saveBtn);

         await waitFor(() => {
             expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 pause: true
             }));
         });
    });
});
