import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import App from '../App';
import * as api from '../api';
import { setupDefaultApiMocks, defaultAuthStatus, defaultSettings, defaultESSProviders } from '../test/apiMocks';

const { fetchAuthStatus, fetchSettings, updateSettings, login, logout, deleteSite, deleteUser } = api;

vi.mock('../api');

// Mock Google OAuth
vi.mock('@react-oauth/google', () => ({
    GoogleOAuthProvider: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    GoogleLogin: ({ onSuccess }: { onSuccess: (res: any) => void }) => (
        <button onClick={() => onSuccess({ credential: 'test-token' })}>
            Google Sign In
        </button>
    ),
}));

// Helper to navigate to settings page
const navigateToSettings = async (authStatus = defaultAuthStatus) => {
    window.history.pushState(null, '', '/');
    (fetchAuthStatus as any).mockResolvedValue({ ...authStatus });
    render(<App />);
    fireEvent.click(screen.getByText(/Log In/));
    await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

    // If onboarding wizard renders, skip it so the standard settings inputs are available
    await waitFor(() => {
        const hasSettingsHeading = screen.queryByRole('heading', { name: /^Settings$/i });
        const skipBtn = screen.queryByRole('button', { name: /Skip/i });
        expect(hasSettingsHeading || skipBtn).toBeTruthy();
    });

    const skipBtn = screen.queryByRole('button', { name: /Skip/i });
    if (skipBtn) {
        fireEvent.click(skipBtn);
    }

    await screen.findByRole('heading', { name: /^Settings$/i });
};

describe('App & Settings', () => {
    beforeEach(() => {
        vi.unstubAllGlobals();
        window.history.pushState(null, '', '/');
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('shows login button when auth required and not logged in', async () => {
        (fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: false
        });

        render(<App />);

        // On LandingPage, click Login link in header
        fireEvent.click(screen.getByText(/Log In/));

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
        fireEvent.click(screen.getByText(/Log In/));

        await waitFor(() => {
            expect(screen.getByText('Google Sign In')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Google Sign In'));

        await waitFor(() => {
            expect(login).toHaveBeenCalledWith('test-token', 'google');
        });
    });

    it('shows logout button when logged in and calls logout on click', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });
        (logout as any).mockResolvedValue(undefined);

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));

        await waitFor(() => {
            expect(screen.getByText('Log Out')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Log Out'));

        await waitFor(() => {
            expect(logout).toHaveBeenCalled();
        });
    });

    it('shows settings link when logged in', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));

        await waitFor(() => {
            expect(screen.getByText('Settings')).toBeInTheDocument();
        });
    });

    it('navigates to settings and loads data', async () => {
        await navigateToSettings();

        await waitFor(() => {
            expect(screen.getByLabelText(/Minimum Battery %/i)).toBeInTheDocument();
            expect(screen.getByDisplayValue('10')).toBeInTheDocument();
        });
    });

    it('can update settings', async () => {
         await navigateToSettings();

         // Change input
         const input = await screen.findByLabelText(/Minimum Battery %/i);
         fireEvent.change(input, { target: { value: '20' } });

         // Mock update success
         (updateSettings as any).mockResolvedValue(undefined);

         // Helper to click save
         const saveBtn = screen.getByText('Save Settings');
         fireEvent.click(saveBtn);

         await waitFor(() => {
             expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 minBatterySOC: 20,
                 release: "production"
             }), expect.any(String), undefined);
         });
    });

    it('can toggle pause setting', async () => {
         await navigateToSettings();
         (updateSettings as any).mockResolvedValue(undefined);

         const pauseBtn = await screen.findByRole('button', { name: /^Pause$/i });
         fireEvent.click(pauseBtn);

         await waitFor(() => {
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 pause: true
             }), expect.any(String));
         });
    });

    it('can toggle grid strategy settings', async () => {
         await navigateToSettings();

         // Click Update on ESS to reveal grid strategy switches
         const updateBtn = await screen.findByRole('button', { name: /Update Energy Storage System/i });
         fireEvent.click(updateBtn);

         // Toggle Export Battery switch
         const switchEl = await screen.findByRole('switch', { name: /Export Battery to Grid/i });
         fireEvent.click(switchEl);

         // Mock update success
         (updateSettings as any).mockResolvedValue(undefined);

         // Helper to click save
         const saveBtn = screen.getByText('Save Settings');
         fireEvent.click(saveBtn);

         await waitFor(() => {
             expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 gridExportBatteries: true,
                 customGridSettings: true
             }), expect.any(String), undefined);
         });
    });

    it('renders solar settings inputs on settings page', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            postalCode: ''
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        await waitFor(() => {
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
            expect(screen.getByDisplayValue('3')).toBeInTheDocument();
            expect(screen.getByDisplayValue('1')).toBeInTheDocument();
        });
    });

    it('can update solar bell curve multiplier', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            postalCode: ''
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        await waitFor(() => expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument());
        const input = screen.getByLabelText(/Solar Bell Curve Multiplier/i);
        fireEvent.change(input, { target: { value: '0.5' } });

        (updateSettings as any).mockResolvedValue(undefined);
        fireEvent.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                solarBellCurveMultiplier: 0.5
            }), expect.any(String), undefined);
        });
    });

    it('can update home load prediction strategy', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            homeLoadPredictionStrategy: 'default'
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        // Find strategy select trigger by label
        const strategySelect = await screen.findByRole('combobox', { name: /Home Load Prediction Strategy/i });

        // Caution message should not be visible initially
        expect(screen.queryByTestId('conservative-strategy-warning')).not.toBeInTheDocument();

        // Open select and choose Conservative
        await user.click(strategySelect);
        const conservativeOption = await screen.findByRole('option', { name: 'Conservative (High Protection)' });
        await user.click(conservativeOption);

        // Caution message should show now
        expect(screen.getByTestId('conservative-strategy-warning')).toBeInTheDocument();

        // Save
        (updateSettings as any).mockResolvedValue(undefined);
        fireEvent.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                homeLoadPredictionStrategy: 'conservative'
            }), expect.any(String), undefined);
        });
    });

    it('hides solar trend ratio max and solar bell curve multiplier when zip code is entered', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            postalCode: ''
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        // They should be visible initially because zip code is empty
        await waitFor(() => {
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
        });

        // Now enter a zip code in the Zip/Postal Code field
        const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
        fireEvent.change(zipInput, { target: { value: '90210' } });

        // They should be hidden now
        await waitFor(() => {
            expect(screen.queryByLabelText(/Solar Trend Ratio Max/i)).not.toBeInTheDocument();
            expect(screen.queryByLabelText(/Solar Bell Curve Multiplier/i)).not.toBeInTheDocument();
        });

        // Now clear the zip code
        fireEvent.change(zipInput, { target: { value: '' } });

        // They should show up again
        await waitFor(() => {
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
        });
    });

    it('hides headroom when solar exporting is enabled, and hides entire advanced solar settings when both are true', async () => {
        // Mock settings without postalCode and without gridExportSolar so everything is visible
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            postalCode: '',
            gridExportSolar: false
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        // All should be visible initially
        await waitFor(() => {
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Fully Charge Headroom/i)).toBeInTheDocument();
            expect(screen.getByRole('heading', { name: /Advanced Solar Settings/i })).toBeInTheDocument();
        });

        // 1. Toggle solar exporting on
        const updateBtn = await screen.findByRole('button', { name: /Update Energy Storage System/i });
        fireEvent.click(updateBtn);
        const exportSwitch = screen.getByRole('switch', { name: /Export Solar to Grid/i });
        fireEvent.click(exportSwitch);

        // Headroom should be hidden
        await waitFor(() => {
            expect(screen.queryByLabelText(/Solar Fully Charge Headroom/i)).not.toBeInTheDocument();
            // Trend ratio and bell curve should still be visible because postalCode is empty
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
            expect(screen.getByRole('heading', { name: /Advanced Solar Settings/i })).toBeInTheDocument();
        });

        // 2. Now enter a zip code as well (both are now true)
        const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
        fireEvent.change(zipInput, { target: { value: '90210' } });

        // The entire Advanced Solar Settings section should be hidden
        await waitFor(() => {
            expect(screen.queryByRole('heading', { name: /Advanced Solar Settings/i })).not.toBeInTheDocument();
            expect(screen.queryByLabelText(/Solar Trend Ratio Max/i)).not.toBeInTheDocument();
            expect(screen.queryByLabelText(/Solar Bell Curve Multiplier/i)).not.toBeInTheDocument();
            expect(screen.queryByLabelText(/Solar Fully Charge Headroom/i)).not.toBeInTheDocument();
        });

        // 3. Clear zip code again
        fireEvent.change(zipInput, { target: { value: '' } });

        // Section header and trend/bell curve should come back (headroom still hidden since solar exporting is enabled)
        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Advanced Solar Settings/i })).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
            expect(screen.queryByLabelText(/Solar Fully Charge Headroom/i)).not.toBeInTheDocument();
        });
    });



    it('shows location settings only when release is staging', async () => {
        const stagingSettings = { release: 'staging', minArbitrageDifferenceDollarsPerKWH: 0.05, minBatterySOC: 20, minLoadForSolarHedgeKWH: 2.0, ess: 'mock', hasCredentials: { mock: true } };
        (fetchSettings as any).mockResolvedValue(stagingSettings);
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));
        await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
        fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
        await screen.findByRole('heading', { name: /^Settings$/i });

        await waitFor(() => {
            expect(screen.getByText('Location')).toBeInTheDocument();
            expect(screen.getByLabelText(/Zip\/Postal Code/i)).toBeInTheDocument();
        });

        const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
        fireEvent.change(zipInput, { target: { value: '90210' } });

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({ postalCode: '90210' }), expect.any(String), undefined);
        });
    });

    it('can update roof solar panel direction', async () => {
        const user = userEvent.setup();
        const stagingSettings = { release: 'staging', solarAzimuth: 0, solarTilt: 25, ess: 'mock', hasCredentials: { mock: true } };
        (fetchSettings as any).mockResolvedValue(stagingSettings);
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));
        await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
        fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
        await screen.findByRole('heading', { name: /^Settings$/i });

        const directionSelect = await screen.findByLabelText(/Solar Direction/i);
        await user.click(directionSelect);
        const northOption = await screen.findByRole('option', { name: 'North' });
        await user.click(northOption);

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                solarAzimuth: 0,
                solarTilt: 25
            }), expect.any(String), undefined);
        });
    });

    it('can update roof solar panel direction to intermediate directions', async () => {
        const user = userEvent.setup();
        const stagingSettings = { release: 'staging', solarAzimuth: 0, solarTilt: 25, ess: 'mock', hasCredentials: { mock: true } };
        (fetchSettings as any).mockResolvedValue(stagingSettings);
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));
        await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
        fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
        await screen.findByRole('heading', { name: /^Settings$/i });

        const directionSelect = await screen.findByLabelText(/Solar Direction/i);
        await user.click(directionSelect);
        const northeastOption = await screen.findByRole('option', { name: 'Northeast' });
        await user.click(northeastOption);

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                solarAzimuth: 45,
                solarTilt: 25
            }), expect.any(String), undefined);
        });
    });

    it('can update ComEd rate options', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'comed',
            utilityRate: 'comed_besh',
            utilityRateOptions: {}
        });
        await navigateToSettings();

        // Wait for Utility Options section
        await screen.findByText('Configured');
        fireEvent.click(screen.getByRole('button', { name: 'Change Utility Service' }));

        await waitFor(() => expect(screen.getByRole('switch', { name: /Delivery Time-of-Day/i })).toBeInTheDocument());

        // Toggle Delivery Time-of-Day switch
        const dtodSwitch = screen.getByRole('switch', { name: /Delivery Time-of-Day/i });
        fireEvent.click(dtodSwitch);

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                utilityRateOptions: expect.objectContaining({
                    rateClass: "singleFamilyWithoutElectricHeat",
                    variableDeliveryRate: true
                })
            }), expect.any(String), undefined);
        });
    });

    it('can update price threshold fields and shows warning appropriately', async () => {
        await navigateToSettings();

        // Expand advanced tuning settings to see the price threshold
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        // Check fields are accessible
        const priceInput = await screen.findByLabelText(/Always Charge Below/i);

        // At initial value, warning should not be shown
        expect(screen.queryByText(/Are you sure you want to force charging the batteries from the grid when it's below this price/i)).not.toBeInTheDocument();

        fireEvent.change(priceInput, { target: { value: '0.10' } });

        // Warning should be shown now that value > 0.05
        await waitFor(() => {
            expect(screen.getByText(/Are you sure you want to force charging the batteries from the grid when it's below this price/i)).toBeInTheDocument();
        });

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                alwaysChargeUnderDollarsPerKWH: 0.10
            }), expect.any(String), undefined);
        });
    });

    it('can update standby for similar price field', async () => {
        await navigateToSettings();

        // Expand advanced tuning settings to see price threshold fields
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        const holdInput = await screen.findByLabelText(/Standby for Similar Export Price/i);
        fireEvent.change(holdInput, { target: { value: '0.04' } });

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                minExportHoldDifferenceDollarsPerKWH: 0.04
            }), expect.any(String), undefined);
        });
    });

    it('shows solar net metering credits value setting when netMeteringScheme is net', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'srp',
            utilityRate: 'srp_e27',
            utilityRateOptions: {
                netMeteringScheme: 'net'
            }
        });
        await navigateToSettings();

        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        const nmSelect = await screen.findByLabelText(/Solar Net Metering Credits Value/i);
        expect(nmSelect).toBeInTheDocument();
        await user.click(nmSelect);

        const highestOption = await screen.findByRole('option', { name: 'Highest Price' });
        await user.click(highestOption);

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                solarNetMeteringCreditsValue: 'highest'
            }), expect.any(String), undefined);
        });
    });


    it('can select utility provider and then rate', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '',
            utilityRate: '',
            utilityRateOptions: {}
        });
        await navigateToSettings();

        // Select Service (Provider)
        const serviceSelect = await screen.findByRole('combobox', { name: /Service/i });
        await user.click(serviceSelect);
        const comedOption = await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' });
        await user.click(comedOption);

        // Rate/Plan should be auto-selected since ComEd only has one in the mock
        await screen.findByText(/Hourly Pricing Program/i);

        // Verify options appear - wait for the label to be stable
        const switchEl = await screen.findByRole('switch', { name: /Delivery Time-of-Day/i });
        expect(switchEl).toBeInTheDocument();

        // Save and verify
        fireEvent.click(screen.getByText('Save Settings'));
        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                utilityRateOptions: expect.objectContaining({
                    rateClass: "singleFamilyWithoutElectricHeat",
                    variableDeliveryRate: false
                })
            }), expect.any(String), undefined);
        });
    });

    it('can submit ESS credentials and passes raw password', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'franklin',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select ESS
        const essSelect = await screen.findByLabelText(/ESS/i);
        await user.click(essSelect);
        const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        // Fill in credentials based on apiMocks
        const emailInput = await screen.findByLabelText('Email');
        await user.type(emailInput, 'user@example.com');

        const passInput = await screen.findByLabelText(/Password/i, { selector: 'input[type="password"]' });
        await user.type(passInput, 'myrawpassword');

        // Target Gateway ID is optional, we skip it

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();

            // Should pass the credentials untouched
            expect(updateSettings).toHaveBeenCalledWith(expect.anything(), expect.any(String), {
                franklin: {
                    username: 'user@example.com',
                    password: 'myrawpassword'
                }
            });
        });
    });

    it('hides providers with hidden true unless already selected', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '', // Not secret utility
            ess: '', // Not secret ess
        });

        await navigateToSettings();

        // Check utility
        const serviceSelect = await screen.findByRole('combobox', { name: /Service/i });
        await user.click(serviceSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'Commonwealth Edison (ComEd)' })).toBeInTheDocument());
        expect(screen.queryByRole('option', { name: 'Secret Utility' })).not.toBeInTheDocument();
        // Try to close dropdown by clicking document body
        fireEvent.pointerDown(document.body);

        // Check ESS
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'FranklinWH' })).toBeInTheDocument());
        expect(screen.queryByRole('option', { name: 'Secret ESS' })).not.toBeInTheDocument();
    });

    it('shows hidden ESS providers if present in enabled localStorage', async () => {
        const user = userEvent.setup();
        localStorage.setItem('enabled', 'hidden_ess');
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '',
            ess: '',
        });

        await navigateToSettings();

        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'FranklinWH' })).toBeInTheDocument());
        expect(screen.getByRole('option', { name: 'Secret ESS' })).toBeInTheDocument();
    });

    it('shows hidden providers if they are currently configured', async () => {
         const user = userEvent.setup();
         (fetchSettings as any).mockResolvedValue({
             ...defaultSettings,
             utilityProvider: 'hidden_utility',
             utilityRate: 'hidden_rate',
             ess: 'hidden_ess',
         });

         await navigateToSettings();

         // The configured summary should show the hidden provider's name
         await waitFor(() => expect(screen.getByText("Secret Utility")).toBeInTheDocument());
         expect(screen.getByText("Secret ESS")).toBeInTheDocument();

         // In edit mode (change), the option should also be visible in the dropdown
         const utilityChangeBtn = await screen.findByRole('button', { name: 'Edit Utility Service' });
         await user.click(utilityChangeBtn); // click Utility Service "Change"

         const serviceSelect = await screen.findByRole('combobox', { name: /Service/i });
         await user.click(serviceSelect);
         await waitFor(() => expect(screen.getByRole('option', { name: 'Secret Utility' })).toBeInTheDocument());
    });

    it('shows edit ess if they are set without any credentials', async () => {
         const user = userEvent.setup();
         (fetchSettings as any).mockResolvedValue({
             ...defaultSettings,
             ess: 'missing_ess',
             hasCredentials: {
                 missing_ess: false,
             }
         });

         await navigateToSettings();

         // The select should show the ess's name
         await waitFor(() => expect(screen.getByText("Missing ESS")).toBeInTheDocument());

         const usernameInput = await screen.findByLabelText('Username');
         expect(usernameInput).toBeInTheDocument();
         await user.type(usernameInput, 'username');

         const doneBtn = await screen.findByRole('button', { name: 'Finish editing Energy Storage System' });
         expect(doneBtn).toBeInTheDocument();
         await user.click(doneBtn);

         await waitFor(() => expect(screen.getByText("Pending Save")).toBeInTheDocument());
    });

    it('handles oAuthKey dropdown and routes to correct oAuthURL', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select Multi Region ESS
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        const multiRegionOption = await screen.findByRole('option', { name: 'Multi Region ESS' });
        await user.click(multiRegionOption);

        // Verify Region select is shown and defaults to North America
        const regionSelect = await screen.findByLabelText(/Region/i);
        expect(screen.getByText('North America')).toBeInTheDocument();

        // Verify Login button works and goes to NA url
        const windowOpenSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        fireEvent.click(loginBtn);
        expect(windowOpenSpy).toHaveBeenCalledWith('https://na.example.com/?state=site1', 'OAuthLogin', expect.any(String));

        // Change select to EU
        await user.click(regionSelect);
        const euOption = await screen.findByRole('option', { name: 'Europe' });
        await user.click(euOption);

        // Fill in required authCode based on apiMocks
        const passInput = await screen.findByLabelText(/Authorization Code/i, { selector: 'input[type="password"]' });
        await user.type(passInput, 'testauthcode');

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.anything(), expect.any(String), {
                multi_region_ess: {
                    region: 'EU',
                    authCode: 'testauthcode'
                }
            });
        });
        windowOpenSpy.mockRestore();
    });

    it('shows error when required ESS credential field is missing', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: '',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select ESS
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        // Click Save without filling anything
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('The Email field is required.')).toBeInTheDocument();
        });

        // Fill Email, still missing Password
        const emailInput = await screen.findByLabelText('Email');
        await user.type(emailInput, 'user@example.com');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('The Password field is required.')).toBeInTheDocument();
        });
    });

    it('hides options that have hidden true', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'test_provider',
            utilityRate: 'test_rate',
            utilityRateOptions: {}
        });
        (api.fetchUtilities as any).mockResolvedValue([
            {
                id: 'test_provider',
                name: 'Test Provider',
                rates: [
                    {
                        id: 'test_rate',
                        name: 'Test Rate',
                        options: [
                            {
                                field: 'visibleOption',
                                name: 'Visible Option',
                                type: 'switch',
                                default: true
                            },
                            {
                                field: 'hiddenOption',
                                name: 'Hidden Option',
                                type: 'switch',
                                default: true,
                                hidden: true
                            }
                        ]
                    }
                ]
            }
        ] as any);

        await navigateToSettings();

        // Check that the configured rates view shows up
        await screen.findByText('Configured');
        fireEvent.click(screen.getByRole('button', { name: 'Change Utility Service' }));

        // Check that visible option is rendered but hidden option is not
        await waitFor(() => expect(screen.getByRole('switch', { name: /Visible Option/i })).toBeInTheDocument());
        expect(screen.queryByRole('switch', { name: /Hidden Option/i })).not.toBeInTheDocument();
    });

    it('can select SCE utility and net metering options', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '',
            utilityRate: '',
            utilityRateOptions: {}
        });
        (api.fetchUtilities as any).mockResolvedValue([
            {
                id: 'sce',
                name: 'Southern California Edison',
                rates: [
                    {
                        id: 'sce_tou_d_prime',
                        name: 'TOU-D-PRIME',
                        options: [
                            {
                                field: 'netMeteringScheme',
                                name: 'Net Metering / Export Scheme',
                                type: 'select',
                                choices: [
                                    { value: 'nem1', name: 'NEM 1.0 (Full Retail 1:1 Credits)' },
                                    { value: 'nem2', name: 'NEM 2.0 (Retail Credits minus NBCs)' },
                                    { value: 'sbp', name: 'Solar Billing Plan (NEM 3.0)' }
                                ],
                                default: 'sbp'
                            }
                        ]
                    }
                ]
            }
        ] as any);

        await navigateToSettings();

        // Select Service (Provider)
        const serviceSelect = await screen.findByRole('combobox', { name: /Service/i });
        await user.click(serviceSelect);
        const sceOption = await screen.findByRole('option', { name: 'Southern California Edison' });
        await user.click(sceOption);

        // Rate/Plan should be auto-selected since SCE only has one in this mock
        await screen.findByText(/TOU-D-PRIME/i);

        // Verify select option appears for export scheme
        const schemeSelect = await screen.findByLabelText(/Net Metering \/ Export Scheme/i);
        expect(schemeSelect).toBeInTheDocument();
        expect(screen.getByText('Solar Billing Plan (NEM 3.0)')).toBeInTheDocument();

        // Change select option to NEM 2.0
        await user.click(schemeSelect);
        const nem2Option = await screen.findByRole('option', { name: 'NEM 2.0 (Retail Credits minus NBCs)' });
        await user.click(nem2Option);

        // Save
        (updateSettings as any).mockResolvedValue(undefined);
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                utilityProvider: 'sce',
                utilityRate: 'sce_tou_d_prime',
                utilityRateOptions: expect.objectContaining({
                    netMeteringScheme: 'nem2'
                })
            }), expect.any(String), undefined);
        });
    });

    it('supports multi-stage OTP credential staging flow for Enphase', async () => {
        const user = userEvent.setup();
        const enphaseSettings = {
            ...defaultSettings,
            ess: 'enphase',
            hasCredentials: {}
        };
        (fetchSettings as any).mockResolvedValue(enphaseSettings);
        (api.fetchESSList as any).mockResolvedValue([
            ...defaultESSProviders,
            {
                id: 'enphase',
                name: 'Enphase',
                credentials: [
                    { field: 'username', name: 'Email', type: 'string', required: true, stage: 0 },
                    { field: 'code', name: 'Email Code', type: 'string', required: false, stage: 1 }
                ]
            }
        ]);
        (api.submitESSStage as any).mockResolvedValue(undefined);

        await navigateToSettings();

        const emailInput = await screen.findByLabelText('Email');
        expect(emailInput).toBeInTheDocument();
        expect(screen.queryByLabelText(/Email Code/i)).not.toBeInTheDocument();

        await user.type(emailInput, 'test@example.com');

        const continueBtn = screen.getByRole('button', { name: 'Continue' });
        expect(continueBtn).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: 'Save Settings' })).not.toBeInTheDocument();

        await user.click(continueBtn);

        expect(api.submitESSStage).toHaveBeenCalledWith('enphase', { username: 'test@example.com' }, expect.any(String));

        const codeInput = await screen.findByLabelText(/Email Code/i);
        expect(codeInput).toBeInTheDocument();

        expect(screen.queryByRole('button', { name: 'Continue' })).not.toBeInTheDocument();
        const saveBtn = screen.getByRole('button', { name: 'Save Settings' });
        expect(saveBtn).toBeInTheDocument();

        await user.type(codeInput, '654321');

        (updateSettings as any).mockResolvedValue(undefined);
        await user.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.anything(), expect.any(String), {
                enphase: {
                    username: 'test@example.com',
                    code: '654321'
                }
            });
        });
    });

    it('can request a new utility rate/option via the dialog', async () => {
        const user = userEvent.setup();
        (api.fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'comed',
            utilityRate: 'comed_besh'
        });
        await navigateToSettings();

        // 1. Click "Change" in the Utility Service section to open/edit utility configuration
        fireEvent.click(screen.getByRole('button', { name: 'Change Utility Service' }));

        // 2. Click the link to open the dialog
        const unsupportedLink = await screen.findByRole('button', { name: "Don't see your rate or options?" });
        await user.click(unsupportedLink);

        // 3. Verify dialog is open and shows correct title
        const dialogTitle = await screen.findByRole('heading', { name: 'Request a Rate or Option' });
        expect(dialogTitle).toBeInTheDocument();

        // 4. Select utility 'Other'
        const serviceSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
        await user.click(serviceSelect);
        const otherOption = await screen.findByRole('option', { name: 'Other' });
        await user.click(otherOption);

        // 5. Fill out the details
        const providerInput = screen.getByLabelText('Utility Provider Name');
        await user.type(providerInput, 'National Grid');

        const stateInput = screen.getByLabelText('State');
        await user.type(stateInput, 'NY');

        const rateInput = screen.getByLabelText('Rate / Plan Name');
        await user.type(rateInput, 'TOU-123');

        const commentInput = screen.getByLabelText(/Anything else to share or comments?/i);
        await user.type(commentInput, 'Please add this rate!');

        // Mock API submission
        (api.submitInterest as any).mockResolvedValue(undefined);

        // 6. Click submit
        const submitBtn = screen.getByRole('button', { name: 'Express Interest' });
        await user.click(submitBtn);

        // 7. Verify success message
        await waitFor(() => {
            expect(screen.getByText('Success!')).toBeInTheDocument();
            expect(screen.getByText(/We've received your interest!/)).toBeInTheDocument();
        });

        // 8. Verify the submitInterest API was called with the correct arguments
        expect(api.submitInterest).toHaveBeenCalledWith({
            utility: 'other',
            battery: 'none',
            utilityProviderName: 'National Grid',
            state: 'NY',
            planName: 'TOU-123',
            batteryName: '',
            comments: 'Please add this rate!'
        });

        // 9. Close the dialog
        const closeBtn = screen.getByRole('button', { name: 'Close' });
        await user.click(closeBtn);

        // 10. Verify dialog is closed (title is no longer visible)
        await waitFor(() => {
            expect(screen.queryByRole('heading', { name: 'Request a Rate or Option' })).not.toBeInTheDocument();
        });
    });

    it('can update new timing settings: minStartChargeMinutes and bufferProfile', async () => {
        const user = userEvent.setup();
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        await user.click(advancedBtn);

        await waitFor(() => {
            expect(screen.getByLabelText(/Minimum Start Charge Duration/i)).toBeInTheDocument();
            expect(screen.getByRole('combobox', { name: /Overcharge Profile/i })).toBeInTheDocument();
        });

        const minStartInput = screen.getByLabelText(/Minimum Start Charge Duration/i);
        const bufferSelect = screen.getByRole('combobox', { name: /Overcharge Profile/i });

        expect(minStartInput).toHaveValue(5);
        expect(bufferSelect).toHaveTextContent('Default');

        fireEvent.change(minStartInput, { target: { value: '10' } });

        await user.click(bufferSelect);
        const aggressiveOption = await screen.findByRole('option', { name: 'Tiny' });
        await user.click(aggressiveOption);

        (updateSettings as any).mockResolvedValue(undefined);
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                minStartChargeMinutes: 10,
                socBufferPercent: 2,
                peakSurvivalBufferMinutes: 10,
                solarCapacityBufferMinutes: 0,
                vppChargingBufferMinutes: 10
            }), expect.any(String), undefined);
        });
    });

    it('clears one-time credentials on save failure to force restart', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'multi_region_ess',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Fill in required authCode based on apiMocks
        const passInput = await screen.findByLabelText(/Authorization Code/i, { selector: 'input[type="password"]' });
        await user.type(passInput, 'testauthcode');

        // Mock update to fail
        (updateSettings as any).mockRejectedValue(new Error('Failed to verify credentials'));

        // Save
        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        // Expect error to be shown
        await waitFor(() => {
            expect(screen.getByText('Failed to verify credentials')).toBeInTheDocument();
        });

        // Click save again - now it should fail because authCode was cleared and is required
        await user.click(saveBtn);
        await waitFor(() => {
            expect(screen.getByText('Login to connect your energy system.')).toBeInTheDocument();
        });
    });

    it('handles invalid authorization code error and expands ESS section with custom message', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'franklin',
            hasCredentials: { franklin: true }
        });

        await navigateToSettings();

        // Initially ESS is not in edit mode (it's configured)
        expect(screen.queryByLabelText('Email')).not.toBeInTheDocument();

        // Make updateSettings fail with invalid auth code error
        (updateSettings as any).mockRejectedValue(new Error('failed to exchange auth code: upstream error status 400: The authorization code is invalid. It may have already been used or expired.'));

        // Submit form
        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        // Expect custom message and ESS section to be open
        await waitFor(() => {
            expect(screen.getByText("The authorization code expired. Please click 'Login to link account' again and save immediately.")).toBeInTheDocument();
            expect(screen.getByLabelText('Email')).toBeInTheDocument();
        });
    });

    it('handles oauthStatus transitions and popup button disabled/spinner states', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'multi_region_ess',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Mock window.open
        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        // Click login button
        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        await user.click(loginBtn);

        // Expect button to be disabled and show "Awaiting link..." with guidance text
        expect(screen.getByRole('button', { name: /Awaiting link.../i })).toBeDisabled();
        expect(screen.getByText('Please complete authentication in the popup window.')).toBeInTheDocument();

        // Simulate successful OAuth message
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'success_code_123', state: 'site1' }
            }));
        });

        // Expect success badge to render and button to be gone
        await waitFor(() => {
            expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            expect(screen.queryByRole('button', { name: /Login to link account/i })).not.toBeInTheDocument();
        });

        windowOpenSpy.mockRestore();
    });

    it('clears Tesla oauth success message and shows login button on save failure', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Initially we see the Login button, not the success message
        expect(screen.getByRole('button', { name: /Login to link account/i })).toBeInTheDocument();
        expect(screen.queryByText('Received code! Save Settings below to complete.')).not.toBeInTheDocument();

        // Mock window.open
        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        // Click login button
        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        await user.click(loginBtn);

        // Simulate successful OAuth message
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'tesla_success_code', state: 'site1' }
            }));
        });

        // Expect success badge to render and button to be gone
        await waitFor(() => {
            expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            expect(screen.queryByRole('button', { name: /Login to link account/i })).not.toBeInTheDocument();
        });

        // Mock update to fail
        (updateSettings as any).mockRejectedValue(new Error('Failed to verify credentials'));

        // Save
        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        // Expect error to be shown, success banner to be gone, and login button to be back
        await waitFor(() => {
            expect(screen.getByText('Failed to verify credentials')).toBeInTheDocument();
            expect(screen.queryByText('Received code! Save Settings below to complete.')).not.toBeInTheDocument();
            expect(screen.getByRole('button', { name: /Login to link account/i })).toBeInTheDocument();
        });

        windowOpenSpy.mockRestore();
    });

    it('hides grid strategy switches when connecting an ESS for the first time before saving credentials', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: '',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select an ESS for the first time
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        // Grid switches and warning should NOT be displayed before credentials are saved
        expect(screen.queryByRole('switch', { name: /Grid Can Charge Battery/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('switch', { name: /Export Solar to Grid/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('switch', { name: /Export Battery to Grid/i })).not.toBeInTheDocument();
        expect(screen.queryByTestId('grid-restrictions-warning')).not.toBeInTheDocument();
    });

    it('shows a warning below checkboxes when all grid strategy settings are unchecked', async () => {
        await navigateToSettings();

        // Open ESS edit mode to reveal grid strategy settings
        const updateBtn = await screen.findByRole('button', { name: /Update Energy Storage System/i });
        fireEvent.click(updateBtn);

        // Warning should not be shown initially (gridChargeBatteries is true by default)
        expect(screen.queryByTestId('grid-restrictions-warning')).not.toBeInTheDocument();

        // Toggle "Grid Can Charge Battery" to false (so all three grid settings are false)
        const chargeSwitch = await screen.findByRole('switch', { name: /Grid Can Charge Battery/i });
        fireEvent.click(chargeSwitch);

        // Warning should be displayed now
        await waitFor(() => {
            expect(screen.getByTestId('grid-restrictions-warning')).toBeInTheDocument();
            expect(screen.getByText(/Warning: All grid interactions are disabled/i)).toBeInTheDocument();
        });

        // Toggle it back on
        fireEvent.click(chargeSwitch);

        // Warning should disappear
        await waitFor(() => {
            expect(screen.queryByTestId('grid-restrictions-warning')).not.toBeInTheDocument();
        });
    });

    describe('Onboarding Wizard & Checklist Flow', () => {
        it('renders onboarding wizard and walks through steps to completion', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => {
                expect(screen.getByText('Google Sign In')).toBeInTheDocument();
            });
            fireEvent.click(screen.getByText('Google Sign In'));

            // Automatically redirected to settings onboarding wizard Step 1
            await screen.findByRole('heading', { name: /Step 1: Set Location/i });

            // Fill Location
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');

            (updateSettings as any).mockResolvedValue(undefined);

            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Verify updateSettings was not called yet
            expect(updateSettings).not.toHaveBeenCalled();

            // Step 2 Utility should be shown
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });

            // Select Utility Provider
            const providerSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
            await user.click(providerSelect);
            const comedOption = await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' });
            await user.click(comedOption);

            // Select Utility Rate
            const rateSelect = await screen.findByRole('combobox', { name: /Rate Plan/i });
            await user.click(rateSelect);
            const rateOption = await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' });
            await user.click(rateOption);

            const nextBtn2 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn2);

            // Verify updateSettings was still not called
            expect(updateSettings).not.toHaveBeenCalled();

            // Step 3 ESS should be shown
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });

            // Select ESS Provider
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
            await user.click(franklinOption);

            // Type credentials
            const emailInput = await screen.findByPlaceholderText(/Enter Email/i);
            const passInput = await screen.findByPlaceholderText(/Enter Password/i);
            await user.type(emailInput, 'test@franklin.com');
            await user.type(passInput, 'secretPass');

            // Mock refetch return for completion
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'franklin',
                hasCredentials: { franklin: true }
            });

            const doneBtn = screen.getByRole('button', { name: /Complete Setup/i });
            await user.click(doneBtn);

            // Verify final updateSettings call (called once with all collected settings)
            expect(updateSettings).toHaveBeenCalledTimes(1);
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'franklin',
            }), expect.any(String), expect.objectContaining({
                franklin: expect.objectContaining({
                    username: 'test@franklin.com',
                    password: 'secretPass'
                })
            }));

            // Should navigate to dashboard
            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
        });

        it('allows navigating back in the onboarding wizard and retains entered input', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => {
                expect(screen.getByText('Google Sign In')).toBeInTheDocument();
            });
            fireEvent.click(screen.getByText('Google Sign In'));

            // Automatically redirected to settings onboarding wizard Step 1
            await screen.findByRole('heading', { name: /Step 1: Set Location/i });

            // Fill Location
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '12345');

            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Step 2 Utility should be shown
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });

            // Click Back
            const backBtn = screen.getByRole('button', { name: 'Back' });
            await user.click(backBtn);

            // Step 1 Location should be shown again
            await screen.findByRole('heading', { name: /Step 1: Set Location/i });

            // Check that zip code is retained
            expect(screen.getByLabelText(/Zip\/Postal Code/i)).toHaveValue('12345');
        });

        it('shows getting started checklist and highlights incomplete sections in checklist mode', async () => {
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            // Navigate to Settings
            await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
            fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

            // Checklist banner should be visible
            await screen.findByTestId('checklist-banner');
            expect(screen.getByText('Set Location')).toBeInTheDocument();
            expect(screen.getByText('Choose Utility')).toBeInTheDocument();
            expect(screen.getByText('Connect Battery (ESS)')).toBeInTheDocument();

            // Assert Location section is highlighted and incomplete
            expect(screen.getByTestId('location-section')).toHaveClass('highlighted-section');
            // Assert ESS section is highlighted and incomplete
            expect(screen.getByTestId('ess-section')).toHaveClass('highlighted-section');
            // Assert Utility section is NOT highlighted (since it is configured)
            expect(screen.getByTestId('utility-section')).not.toHaveClass('highlighted-section');
        });

        it('does not show checklist or highlighted-section on ESS section when changing ESS on configured site', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'franklin',
                hasCredentials: { franklin: true }
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            // Navigate to Settings
            await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
            fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

            await screen.findByRole('heading', { name: /^Settings$/i });

            // Checklist banner should NOT be visible
            expect(screen.queryByTestId('checklist-banner')).not.toBeInTheDocument();
            expect(screen.getByTestId('ess-section')).not.toHaveClass('highlighted-section');

            // Click Update on ESS section to open fields
            const updateBtn = screen.getByRole('button', { name: /Update Energy Storage System/i });
            await user.click(updateBtn);

            // Change ESS provider in the dropdown
            const essSelect = await screen.findByLabelText(/ESS Type/i);
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            // Checklist banner and highlighted-section should still NOT appear
            expect(screen.queryByTestId('checklist-banner')).not.toBeInTheDocument();
            expect(screen.getByTestId('ess-section')).not.toHaveClass('highlighted-section');
        });

        it('allows skipping onboarding wizard and going directly to all settings', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => {
                expect(screen.getByText('Google Sign In')).toBeInTheDocument();
            });
            fireEvent.click(screen.getByText('Google Sign In'));

            await screen.findByRole('heading', { name: /Step 1: Set Location/i });

            const skipBtn = screen.getByRole('button', { name: /Skip setup wizard/i });
            await user.click(skipBtn);

            // Standard settings form should render now
            await screen.findByLabelText(/Minimum Battery %/i);
            expect(screen.queryByRole('heading', { name: /Step 1: Set Location/i })).not.toBeInTheDocument();
        });

        it('scrolls to the corresponding section when a checklist item is clicked', async () => {
            const scrollIntoViewMock = vi.fn();
            window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
            fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

            // Click "Set Location"
            const locationItem = await screen.findByText('Set Location');
            fireEvent.click(locationItem);
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' });

            scrollIntoViewMock.mockClear();

            // Click "Connect Battery (ESS)"
            const essItem = screen.getByText('Connect Battery (ESS)');
            fireEvent.click(essItem);
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' });
        });

        it('shows Tesla grid settings confirmation dialog after onboarding and saves custom settings', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));
            await waitFor(() => expect(screen.getByText('Google Sign In')).toBeInTheDocument());
            fireEvent.click(screen.getByText('Google Sign In'));

            // Step 1: Location
            await screen.findByRole('heading', { name: /Step 1: Set Location/i });
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');
            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Step 2: Utility
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });
            const providerSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
            await user.click(providerSelect);
            const comedOption = await screen.getByRole('option', { name: 'Commonwealth Edison (ComEd)' });
            await user.click(comedOption);

            const rateSelect = await screen.findByRole('combobox', { name: /Rate Plan/i });
            await user.click(rateSelect);
            const rateOption = await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' });
            await user.click(rateOption);

            const nextBtn2 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn2);

            // Step 3: ESS Tesla
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });
            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'tesla',
                gridChargeBatteries: true,
                gridExportSolar: false,
                gridExportBatteries: false,
                hasCredentials: { tesla: true }
            });

            const doneBtn = screen.getByRole('button', { name: /Complete Setup/i });
            await user.click(doneBtn);

            // Tesla confirmation dialog should be displayed
            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByRole('switch', { name: /Grid Can Charge Battery/i })).toBeInTheDocument();
            });

            const modalChargeSwitch = screen.getByRole('switch', { name: /Grid Can Charge Battery/i });
            expect(modalChargeSwitch).toBeChecked();
            await user.click(modalChargeSwitch);

            const confirmBtn = screen.getByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            // Verify updateSettings was called with customGridSettings: true and gridChargeBatteries: false
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                gridChargeBatteries: false,
                customGridSettings: true
            }), expect.any(String));

            await waitFor(() => {
                expect(screen.getByText('Welcome to RateRudder! 🚀')).toBeInTheDocument();
            });

            const nextBtn = screen.getByRole('button', { name: /Next/i });
            await user.click(nextBtn);

            const gotItBtn = screen.getByRole('button', { name: /Got It/i });
            await user.click(gotItBtn);

            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
        });

        it('skips saving when Tesla grid settings confirmation dialog is confirmed without changes', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));
            await waitFor(() => expect(screen.getByText('Google Sign In')).toBeInTheDocument());
            fireEvent.click(screen.getByText('Google Sign In'));

            // Step 1: Location
            await screen.findByRole('heading', { name: /Step 1: Set Location/i });
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');
            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Step 2: Utility
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });
            const providerSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
            await user.click(providerSelect);
            const comedOption = await screen.getByRole('option', { name: 'Commonwealth Edison (ComEd)' });
            await user.click(comedOption);

            const rateSelect = await screen.findByRole('combobox', { name: /Rate Plan/i });
            await user.click(rateSelect);
            const rateOption = await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' });
            await user.click(rateOption);

            const nextBtn2 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn2);

            // Step 3: ESS Tesla
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });
            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'tesla',
                gridChargeBatteries: true,
                gridExportSolar: false,
                gridExportBatteries: false,
                hasCredentials: { tesla: true }
            });

            const doneBtn = screen.getByRole('button', { name: /Complete Setup/i });
            await user.click(doneBtn);

            // Tesla confirmation dialog should be displayed
            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByRole('switch', { name: /Grid Can Charge Battery/i })).toBeInTheDocument();
            });

            // Clear updateSettings mock call history
            (updateSettings as any).mockClear();

            const confirmBtn = screen.getByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            // Verify updateSettings was NOT called because nothing changed
            expect(updateSettings).not.toHaveBeenCalled();

            await waitFor(() => {
                expect(screen.getByText('Welcome to RateRudder! 🚀')).toBeInTheDocument();
            });

            const nextBtn = screen.getByRole('button', { name: /Next/i });
            await user.click(nextBtn);

            const gotItBtn = screen.getByRole('button', { name: /Got It/i });
            await user.click(gotItBtn);

            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
        });

        it('shows Tesla grid settings confirmation dialog when connecting Tesla from regular settings page', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: '',
                hasCredentials: {}
            });

            await navigateToSettings();

            // Select Tesla
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });
            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'tesla',
                gridChargeBatteries: true,
                gridExportSolar: true,
                gridExportBatteries: false,
                hasCredentials: { tesla: true }
            });

            const saveBtn = screen.getByText('Save Settings');
            await user.click(saveBtn);

            // Tesla confirmation dialog should be displayed
            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByRole('switch', { name: /Grid Can Charge Battery/i })).toBeInTheDocument();
            });

            const confirmBtn = screen.getByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            // Should stay on settings page
            await waitFor(() => {
                expect(screen.queryByText('Confirm Grid Settings')).not.toBeInTheDocument();
            });
        });
    });

    describe('Site and Account Deletion', () => {
        let hrefValue = '';
        const originalLocation = window.location;

        beforeEach(() => {
            (deleteSite as any).mockResolvedValue(undefined);
            (deleteUser as any).mockResolvedValue(undefined);
            hrefValue = '';
            const locationProxy = new Proxy(originalLocation, {
                get(target, prop) {
                    if (prop === 'href') return hrefValue || target.href;
                    return (target as any)[prop];
                },
                set(target, prop, value) {
                    if (prop === 'href') {
                        hrefValue = value;
                        return true;
                    }
                    (target as any)[prop] = value;
                    return true;
                }
            });
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: locationProxy
            });
        });

        afterEach(() => {
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: originalLocation
            });
        });

        it('shows confirmation dialog when Delete Site button is clicked', async () => {
            await navigateToSettings();

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                expect(screen.getByRole('heading', { name: 'Delete Site' })).toBeInTheDocument();
                expect(screen.getByText(/Are you sure you want to delete this site?/i)).toBeInTheDocument();
            });
        });

        it('enables Delete Account checkbox when user has only 1 site', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [{ id: 'site1', name: 'Site 1' }]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                const deleteAccountSwitch = screen.getByRole('switch', { name: /Delete Account/i });
                expect(deleteAccountSwitch).not.toBeDisabled();
            });
        });

        it('disables Delete Account checkbox with hover tooltip when user has multiple sites', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [
                    { id: 'site1', name: 'Site 1' },
                    { id: 'site2', name: 'Site 2' }
                ]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                const deleteAccountSwitch = screen.getByRole('switch', { name: /Delete Account/i });
                expect(deleteAccountSwitch).toHaveAttribute('aria-disabled', 'true');

                const switchRow = deleteAccountSwitch.closest('.switch-row');
                expect(switchRow).toHaveAttribute('title', 'All sites must be deleted first');
            });
        });

        it('calls deleteSite and redirects to dashboard/welcome on success', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [
                    { id: 'site1', name: 'Site 1' },
                    { id: 'site2', name: 'Site 2' }
                ]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Delete$/i })).toBeInTheDocument();
            });

            fireEvent.click(screen.getByRole('button', { name: /^Delete$/i }));

            await waitFor(() => {
                expect(deleteSite).toHaveBeenCalledWith('site1');
                expect(window.location.href).toBe('/dashboard');
            });
        });

        it('calls deleteUser when deleteAccount is checked and redirects to homepage', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [{ id: 'site1', name: 'Site 1' }]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Delete$/i })).toBeInTheDocument();
            });

            const deleteAccountSwitch = screen.getByRole('switch', { name: /Delete Account/i });
            fireEvent.click(deleteAccountSwitch);

            fireEvent.click(screen.getByRole('button', { name: /^Delete$/i }));

            await waitFor(() => {
                expect(deleteSite).toHaveBeenCalledWith('site1');
                expect(deleteUser).toHaveBeenCalled();
                expect(window.location.href).toBe('/');
            });
        });
    });

    describe('ESS Credentials Tutorial Dialog Integration', () => {
        it('shows tutorial dialog when saving ESS credentials for the first time', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: '',
                hasCredentials: {}
            });

            await navigateToSettings();

            const selectEl = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(selectEl);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });

            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);

            const saveBtn = screen.getByText('Save Settings');
            await user.click(saveBtn);

            const confirmBtn = await screen.findByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            await waitFor(() => {
                expect(screen.getByText('Welcome to RateRudder! 🚀')).toBeInTheDocument();
            });

            const nextBtn = screen.getByRole('button', { name: /Next/i });
            await user.click(nextBtn);

            expect(screen.getByText('Manual Charging 💡')).toBeInTheDocument();

            const gotItBtn = screen.getByRole('button', { name: /Got It/i });
            await user.click(gotItBtn);

            await waitFor(() => {
                expect(screen.queryByText('Manual Charging 💡')).not.toBeInTheDocument();
            });
        });

        it('does not show tutorial dialog when ESS credentials are saved but pause is enabled', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: '',
                hasCredentials: {}
            });
            (updateSettings as any).mockResolvedValue(undefined);

            await navigateToSettings();

            const pauseBtn = await screen.findByRole('button', { name: /^Pause$/i });
            fireEvent.click(pauseBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Resume$/i })).toBeInTheDocument();
            });

            const selectEl = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(selectEl);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });

            windowOpenSpy.mockRestore();



            (updateSettings as any).mockResolvedValue(undefined);

            const saveBtn = screen.getByText('Save Settings');
            await user.click(saveBtn);

            await waitFor(() => {
                expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            });
            expect(screen.queryByText('Welcome to RateRudder! 🚀')).not.toBeInTheDocument();
        });

        it('shows Advanced button when in production release without ?variable=true', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                minBatterySOCPeriods: undefined,
            });
            await navigateToSettings();

            expect(screen.getByRole('button', { name: /^Advanced$/i })).toBeInTheDocument();
        });

        it('allows editing existing custom or rate-based schedule even in production release without ?variable=true', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'Peak' },
                { name: 'Off-Peak' }
            ]);
            await navigateToSettings();

            const changeBtn = screen.getByRole('button', { name: /^Change$/i });
            fireEvent.click(changeBtn);

            await waitFor(() => {
                expect(screen.getAllByLabelText(/Peak Reserve %/i)[0]).toBeInTheDocument();
                expect(screen.getAllByLabelText(/Off-Peak Reserve %/i)[0]).toBeInTheDocument();
                expect(screen.getByRole('button', { name: /^Revert to Simple$/i })).toBeInTheDocument();
            });
        });

        it('configures rate period reserve schedule by default when Advanced button clicked', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'On-Peak' },
                { name: 'Off-Peak' }
            ]);
            await navigateToSettings();

            const configBtn = await screen.findByRole('button', { name: /^Advanced$/i });
            fireEvent.click(configBtn);

            await waitFor(() => {
                expect(screen.getByLabelText(/On-Peak Reserve %/i)).toBeInTheDocument();
                expect(screen.getByLabelText(/Off-Peak Reserve %/i)).toBeInTheDocument();
            });

            // Header Advanced button disappears when editing
            expect(screen.queryByRole('button', { name: /^Advanced$/i })).not.toBeInTheDocument();
            expect(screen.getByRole('button', { name: /^Done$/i })).toBeInTheDocument();
            expect(screen.getByRole('button', { name: /^Revert to Simple$/i })).toBeInTheDocument();
        });

        it('switches to custom time reserve schedule when button is clicked', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'On-Peak' },
                { name: 'Off-Peak' }
            ]);
            await navigateToSettings();

            const configBtn = await screen.findByRole('button', { name: /^Advanced$/i });
            fireEvent.click(configBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Custom Mode$/i })).toBeInTheDocument();
            });

            const switchCustomBtn = screen.getByRole('button', { name: /^Custom Mode$/i });
            fireEvent.click(switchCustomBtn);

            await waitFor(() => {
                expect(screen.getByText('From:')).toBeInTheDocument();
                expect(screen.getByText('To:')).toBeInTheDocument();
            });
        });

        it('reverts to simple reserve schedule when Revert to Simple button is clicked', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([]);
            await navigateToSettings();

            const configBtn = await screen.findByRole('button', { name: /^Advanced$/i });
            fireEvent.click(configBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Revert to Simple$/i })).toBeInTheDocument();
            });

            const revertBtn = screen.getByRole('button', { name: /^Revert to Simple$/i });
            fireEvent.click(revertBtn);

            await waitFor(() => {
                expect(screen.getByLabelText(/Minimum Battery %/i)).toBeInTheDocument();
            });
        });

        it('toggles pause and immediately updates settings when Pause/Resume button is clicked', async () => {
            await navigateToSettings();
            const pauseBtn = await screen.findByRole('button', { name: /^Pause$/i });
            fireEvent.click(pauseBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalled();
                expect(screen.getByRole('button', { name: /^Resume$/i })).toBeInTheDocument();
            });

            const resumeBtn = screen.getByRole('button', { name: /^Resume$/i });
            fireEvent.click(resumeBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Pause$/i })).toBeInTheDocument();
            });
        });

        it('skips straight to custom mode without error when utility has no rate periods', async () => {
            (api.fetchUtilities as any).mockResolvedValue([
                { id: 'pg_e', name: 'Pacific Gas & Electric (PG&E)', rates: [{ id: 'pg_e_e_tou_c', name: 'E-TOU-C' }] },
                { id: 'comed', name: 'Commonwealth Edison (ComEd)', rates: [{ id: 'comed_besh', name: 'BESH' }] },
            ]);
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
                utilityProvider: 'pg_e',
                utilityRate: 'pg_e_e_tou_c',
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            });
            (api.fetchUtilityPeriods as any).mockImplementation((_siteID?: string, provider?: string) => {
                if (provider === 'comed') {
                    return Promise.resolve([]);
                }
                return Promise.resolve([
                    { name: 'Peak' },
                    { name: 'Off-Peak' },
                ]);
            });

            await navigateToSettings();

            const editUtilBtn = screen.getByRole('button', { name: 'Change Utility Service' });
            fireEvent.click(editUtilBtn);

            const utilInput = await screen.findByPlaceholderText(/Select a service.../i);
            fireEvent.change(utilInput, { target: { value: 'ComEd' } });
            fireEvent.keyDown(utilInput, { key: 'ArrowDown' });

            const item = await screen.findByText(/ComEd/i);
            fireEvent.click(item);

            await waitFor(() => {
                expect(api.fetchUtilityPeriods).toHaveBeenCalledWith(
                    expect.anything(),
                    'comed',
                    expect.anything(),
                    expect.anything()
                );
            });

            await waitFor(() => {
                expect(screen.queryByTestId('battery-period-error')).not.toBeInTheDocument();
                expect(screen.getByText(/Custom Hours/i)).toBeInTheDocument();
            });

            const summaryBtn = screen.getByRole('button', { name: 'Edit Battery settings' });
            fireEvent.click(summaryBtn);

            await waitFor(() => {
                expect(screen.getByText('From:')).toBeInTheDocument();
                expect(screen.getByText('To:')).toBeInTheDocument();
                expect(screen.queryByRole('button', { name: /^Rates Mode$/i })).not.toBeInTheDocument();
            });
        });

        it('updates period labels automatically when changing to utility with different period names (e.g. FPL On-Peak/Off-Peak)', async () => {
            (api.fetchUtilities as any).mockResolvedValue([
                { id: 'pg_e', name: 'Pacific Gas & Electric (PG&E)', rates: [{ id: 'pg_e_e_tou_c', name: 'E-TOU-C' }] },
                { id: 'fpl', name: 'Florida Power & Light (FPL)', rates: [{ id: 'fpl_tou', name: 'FPL TOU' }] },
            ]);
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                utilityProvider: 'pg_e',
                utilityRate: 'pg_e_e_tou_c',
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            });
            (api.fetchUtilityPeriods as any).mockImplementation((_siteID?: string, provider?: string) => {
                if (provider === 'fpl') {
                    return Promise.resolve([
                        { name: 'On-Peak' },
                        { name: 'Off-Peak' },
                    ]);
                }
                return Promise.resolve([
                    { name: 'Peak' },
                    { name: 'Off-Peak' },
                ]);
            });

            await navigateToSettings();

            const editUtilBtn = screen.getByRole('button', { name: 'Change Utility Service' });
            fireEvent.click(editUtilBtn);

            const utilInput = await screen.findByPlaceholderText(/Select a service.../i);
            fireEvent.change(utilInput, { target: { value: 'Florida' } });
            fireEvent.keyDown(utilInput, { key: 'ArrowDown' });

            const item = await screen.findByText(/Florida Power & Light/i);
            fireEvent.click(item);

            await waitFor(() => {
                expect(screen.getByText(/On-Peak: 50%/i)).toBeInTheDocument();
                expect(screen.getByText(/Off-Peak: 20%/i)).toBeInTheDocument();
            });
        });

        it('opens help dialog when help button next to Minimum Battery % is clicked', async () => {
            const user = userEvent.setup();
            await navigateToSettings();

            const helpBtns = screen.getAllByRole('button', { name: /more info/i });
            const helpBtn = helpBtns[0];
            expect(helpBtn).toBeInTheDocument();

            await user.click(helpBtn);

            expect(await screen.findByRole('dialog')).toBeInTheDocument();
            expect(screen.getByText(/Sets the minimum state-of-charge \(SOC\) level/i)).toBeInTheDocument();
        });

        it('opens help dialog when help button in variable reserve schedule section (advanced mode) is clicked', async () => {
            const user = userEvent.setup();
            const settingsWithPeriods = {
                ...defaultSettings,
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'On-Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            };
            (api.fetchSettings as any).mockResolvedValue(settingsWithPeriods);
            await navigateToSettings();

            expect(screen.getByText(/Variable Reserve Schedule/i)).toBeInTheDocument();
            const batterySection = screen.getByTestId('battery-section');
            const editBtn = within(batterySection).getByText('Change');
            await user.click(editBtn);

            const helpBtns = screen.getAllByRole('button', { name: /more info/i });
            const helpBtn = helpBtns[0];
            expect(helpBtn).toBeInTheDocument();

            await user.click(helpBtn);

            expect(await screen.findByRole('dialog')).toBeInTheDocument();
            expect(screen.getByText(/Rate Period Reserve Schedule/i)).toBeInTheDocument();
        });

        it('clearing variable SOC input does not reset to 0 and allows entering new numbers cleanly (rate periods mode)', async () => {
            const settingsWithPeriods = {
                ...defaultSettings,
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            };
            (api.fetchSettings as any).mockResolvedValue(settingsWithPeriods);
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'Peak' },
                { name: 'Off-Peak' }
            ]);
            (api.updateSettings as any).mockResolvedValue(undefined);
            await navigateToSettings();

            const batterySection = screen.getByTestId('battery-section');
            const editBtn = within(batterySection).getByText('Change');
            fireEvent.click(editBtn);

            await waitFor(() => {
                expect(screen.getAllByLabelText(/Peak Reserve %/i)[0]).toBeInTheDocument();
            });

            const peakInput = screen.getAllByLabelText(/Peak Reserve %/i)[0];
            expect(peakInput).toHaveValue(50);

            // Clear the input
            fireEvent.change(peakInput, { target: { value: '' } });
            expect(peakInput).toHaveValue(null);

            // Type 25
            fireEvent.change(peakInput, { target: { value: '25' } });
            expect(peakInput).toHaveValue(25);

            // Save and verify settings payload
            const saveBtn = screen.getByText('Save Settings');
            fireEvent.click(saveBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledWith(
                    expect.objectContaining({
                        minBatterySOC: 20,
                        minBatterySOCPeriods: [
                            { utilityPeriodName: 'Peak', minBatterySOC: 25 },
                            { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                        ],
                    }),
                    expect.any(String),
                    undefined
                );
            });
        });

        it('clearing variable SOC input does not reset to 0 and allows entering new numbers cleanly (custom hours mode)', async () => {
            const settingsWithPeriods = {
                ...defaultSettings,
                minBatterySOCPeriods: [
                    { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: 50 },
                ],
            };
            (api.fetchSettings as any).mockResolvedValue(settingsWithPeriods);
            (api.fetchUtilityPeriods as any).mockResolvedValue([]);
            (api.updateSettings as any).mockResolvedValue(undefined);
            await navigateToSettings();

            const batterySection = screen.getByTestId('battery-section');
            const editBtn = within(batterySection).getByText('Change');
            fireEvent.click(editBtn);

            await waitFor(() => {
                expect(screen.getByText('SOC %:')).toBeInTheDocument();
            });

            const socInput = screen.getByDisplayValue('50');
            expect(socInput).toHaveValue(50);

            // Clear the input
            fireEvent.change(socInput, { target: { value: '' } });
            expect(socInput).toHaveValue(null);

            // Type 25
            fireEvent.change(socInput, { target: { value: '25' } });
            expect(socInput).toHaveValue(25);

            // Save and verify settings payload
            const saveBtn = screen.getByText('Save Settings');
            fireEvent.click(saveBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledWith(
                    expect.objectContaining({
                        minBatterySOC: 25,
                        minBatterySOCPeriods: [
                            { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: 25 },
                        ],
                    }),
                    expect.any(String),
                    undefined
                );
            });
        });
    });

    describe('EV Charging Battery Protection', () => {
        it('is hidden when query param ?ev=true is not present and release is production', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                evChargingPeriods: undefined,
            });
            await navigateToSettings();
            expect(screen.queryByTestId('ev-charging-section')).not.toBeInTheDocument();
        });

        it('is visible when ?ev=true is present in query param', async () => {
            const originalLocation = window.location;
            delete (window as any).location;
            (window as any).location = new URL('http://localhost/settings?ev=true');

            try {
                (api.fetchSettings as any).mockResolvedValue({
                    ...defaultSettings,
                    release: 'production',
                    evChargingPeriods: undefined,
                });
                await navigateToSettings();
                expect(screen.getByTestId('ev-charging-section')).toBeInTheDocument();
                expect(screen.getByText('Avoid Battery for EV Charging')).toBeInTheDocument();
            } finally {
                (window as any).location = originalLocation;
            }
        });

        it('is visible when settings release is staging', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
                evChargingPeriods: undefined,
            });
            await navigateToSettings();
            expect(screen.getByTestId('ev-charging-section')).toBeInTheDocument();
        });

        it('toggling switch ON calls fetchEstimateEVCharging and populates default hours', async () => {
            const user = userEvent.setup();
            const originalLocation = window.location;
            delete (window as any).location;
            (window as any).location = new URL('http://localhost/settings?ev=true');

            try {
                (api.fetchSettings as any).mockResolvedValue({
                    ...defaultSettings,
                    release: 'production',
                    evChargingPeriods: undefined,
                });
                (api.fetchEstimateEVCharging as any).mockResolvedValue({
                    detected: true,
                    recommendedPeriod: {
                        name: 'Nighttime EV Charging',
                        hours: [{ hourStart: 23, minuteStart: 0, hourEnd: 6, minuteEnd: 0 }],
                    },
                    estimatedRateKW: 11.5,
                    sessionsCount: 11,
                });

                await navigateToSettings();

                const evSwitch = screen.getByRole('switch', { name: /Avoid Battery for EV Charging/i });
                expect(evSwitch).not.toBeChecked();

                await user.click(evSwitch);

                expect(api.fetchEstimateEVCharging).toHaveBeenCalled();
                expect(await screen.findByText(/Auto-detected ~11.5 kW charging based on 11 recent sessions/i)).toBeInTheDocument();

                const startSelect = screen.getByRole('combobox', { name: /Start Time/i });
                const endSelect = screen.getByRole('combobox', { name: /End Time/i });
                expect(startSelect).toHaveTextContent('23:00');
                expect(endSelect).toHaveTextContent('06:00');
            } finally {
                (window as any).location = originalLocation;
            }
        });

        it('shows warning note when auto-estimation fails to detect charging', async () => {
            const user = userEvent.setup();
            const originalLocation = window.location;
            delete (window as any).location;
            (window as any).location = new URL('http://localhost/settings?ev=true');

            try {
                (api.fetchSettings as any).mockResolvedValue({
                    ...defaultSettings,
                    release: 'production',
                    evChargingPeriods: undefined,
                });
                (api.fetchEstimateEVCharging as any).mockResolvedValue({
                    detected: false,
                    message: 'No consistent nighttime EV charging detected.',
                });

                await navigateToSettings();

                const evSwitch = screen.getByRole('switch', { name: /Avoid Battery for EV Charging/i });
                await user.click(evSwitch);

                expect(await screen.findByText(/couldn't detect consistent nighttime EV charging/i)).toBeInTheDocument();
            } finally {
                (window as any).location = originalLocation;
            }
        });

        it('toggling switch OFF clears evChargingPeriods', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
                evChargingPeriods: [
                    {
                        name: 'Nighttime EV Charging',
                        hours: [{ hourStart: 23, minuteStart: 0, hourEnd: 6, minuteEnd: 0 }],
                    },
                ],
            });

            await navigateToSettings();

            const evSwitch = screen.getByRole('switch', { name: /Avoid Battery for EV Charging/i });
            expect(evSwitch).toBeChecked();

            await user.click(evSwitch);

            await waitFor(() => {
                expect(evSwitch).not.toBeChecked();
            });
            expect(screen.queryByRole('combobox', { name: /Start Time/i })).not.toBeInTheDocument();
        });

        it('changing start and end times updates settings', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
                evChargingPeriods: [
                    {
                        name: 'Nighttime EV Charging',
                        hours: [{ hourStart: 23, minuteStart: 0, hourEnd: 6, minuteEnd: 0 }],
                    },
                ],
            });

            await navigateToSettings();

            const startSelect = screen.getByRole('combobox', { name: /Start Time/i });
            const endSelect = screen.getByRole('combobox', { name: /End Time/i });

            expect(startSelect).toHaveTextContent('23:00');
            expect(endSelect).toHaveTextContent('06:00');

            await user.click(startSelect);
            const startOption = await screen.findByRole('option', { name: '00:00' });
            await user.click(startOption);

            await user.click(endSelect);
            const endOption = await screen.findByRole('option', { name: '04:00' });
            await user.click(endOption);

            expect(startSelect).toHaveTextContent('00:00');
            expect(endSelect).toHaveTextContent('04:00');
        });
    });
});
