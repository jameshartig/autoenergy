import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import userEvent from '@testing-library/user-event';
import Dashboard, { whatsNewVersion, whatsNewText, whatsNewLinkText } from './Dashboard';
import { Router } from 'wouter';
import * as api from '../api';
import { setupDefaultApiMocks } from '../test/apiMocks';

const { fetchActionsAndSavings, fetchSettings, ActionReason } = api;

vi.mock('../api');

const renderWithRouter = (component: React.ReactNode) => {
    let element = component;
    if (React.isValidElement(element) && element.type === Dashboard) {
        const props = element.props as any;
        element = React.cloneElement(element, {
            settings: props.settings !== undefined ? props.settings : {
                minBatterySOC: 10,
                ess: 'franklinwh',
                hasCredentials: { franklinwh: true },
                utilityProvider: 'comed_besh',
            }
        } as any);
    }
    return render(
        <Router>
            {element}
        </Router>
    );
};

const mockActionsAndSavings = (actions: any[] = [], savings: any = null) => {
    (fetchActionsAndSavings as any).mockResolvedValue({
        actions,
        savings: savings || {
            batterySavings: 0,
            solarSavings: 0,
            cost: 0,
            credit: 0,
            avoidedCost: 0,
            chargingCost: 0,
            solarGenerated: 0,
            gridImported: 0,
            gridExported: 0,
            homeUsed: 0,
            batteryUsed: 0
        }
    });
};

describe('Dashboard', () => {
    beforeEach(() => {
        window.history.replaceState({}, '', '/');
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('renders loading state initially', () => {
        (fetchActionsAndSavings as any).mockReturnValueOnce(new Promise(() => {}));
        renderWithRouter(<Dashboard />);
        expect(screen.getByText('Loading day...')).toBeInTheDocument();
    });

    it('renders actions with reason-based text', async () => {
        const actions = [{
            reason: 'alwaysChargeBelowThreshold',
            description: 'This is a legacy description',
            timestamp: new Date().toISOString(),
            batteryMode: 2, // ChargeAny
            solarMode: 0,
            currentPrice: { dollarsPerKWH: 0.04, tsStart: '', tsEnd: '' },
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show reason-based text, not description
            expect(screen.getByText(/Current price.*\$ 0\.040/)).toBeInTheDocument();
            // Legacy description should NOT be shown
            expect(screen.queryByText('This is a legacy description')).not.toBeInTheDocument();
        });
    });

    it('falls back to description when reason is empty (legacy actions)', async () => {
        const actions = [{
            description: 'This is a test',
            timestamp: new Date().toISOString(),
            batteryMode: 1,
            solarMode: 1,
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            const standbyElements = screen.getAllByText('Hold Battery');
            expect(standbyElements.length).toBeGreaterThan(0);
            expect(screen.getByText('This is a test')).toBeInTheDocument();
        });
    });

    it('renders no actions message when empty', async () => {
        mockActionsAndSavings([]);
        renderWithRouter(<Dashboard />);
        await waitFor(() => {
            expect(screen.getByText('No actions recorded for this day.')).toBeInTheDocument();
        });
    });

    it('navigates to previous day', async () => {
         const user = userEvent.setup();
         mockActionsAndSavings([]);
         renderWithRouter(<Dashboard />);

         await waitFor(() => {
             expect(screen.getByText(/Prev/)).toBeInTheDocument();
             expect(screen.getByText(/Prev/)).not.toBeDisabled();
         });

         const prevButton = screen.getByText(/Prev/);
         await user.click(prevButton);

         await waitFor(() => {
             const calls = (fetchActionsAndSavings as any).mock.calls;
             if (calls.length < 2) throw new Error('fetchActionsAndSavings not called twice');
             const lastCall = calls[calls.length - 1];
             const startArg = lastCall[0] as Date;
             const now = new Date();
             const expectedDate = new Date(now);
             expectedDate.setDate(expectedDate.getDate() - 1);
             expect(startArg.getDate()).toBe(expectedDate.getDate());
             expect((fetchSettings as any).mock.calls.length).toBe(0);
         });
    });

    it('renders battery SOC in footer', async () => {
        const actions = [{
            reason: ActionReason.AlwaysChargeBelowThreshold,
            description: 'SOC test',
            timestamp: new Date().toISOString(),
            batteryMode: 1,
            solarMode: 1,
            currentPrice: { dollarsPerKWH: 0.05, tsStart: '2026-02-20T19:00:00Z', tsEnd: '' },
            systemStatus: {
                batterySOC: 42.5,
                alarms: [],
                storms: [],
            }
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getAllByText(/Battery/).length).toBeGreaterThan(0);
            expect(screen.getAllByText(/42.5%/).length).toBeGreaterThan(0);
        });
    });

    it('renders dry run badge', async () => {
        const actions = [{
            reason: ActionReason.AlwaysChargeBelowThreshold,
            description: 'Dry run test',
            timestamp: new Date().toISOString(),
            batteryMode: 1, // Standby
            solarMode: 1, // NoExport
            dryRun: true,
            currentPrice: { dollarsPerKWH: 0.01, tsStart: '', tsEnd: '' },
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText('Dry Run')).toBeInTheDocument();
            expect(screen.getByText('Dry Run')).toHaveClass('tag', 'dry-run');
        });
    });

    it('renders VPP active state in current status and timeline', async () => {
        const actions = [{
            reason: ActionReason.VPPActive,
            description: 'VPP Active test',
            timestamp: new Date().toISOString(),
            batteryMode: 1, // Standby
            solarMode: 1, // NoExport
            currentPrice: { dollarsPerKWH: 0.12, tsStart: '', tsEnd: '' },
            systemStatus: {
                batterySOC: 75.0,
                vppActive: true,
                vppKW: 5.4,
            }
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Verify current status card
            const statusCard = document.querySelector('.current-status-card.vpp');
            expect(statusCard).toBeInTheDocument();
            expect(statusCard).toHaveTextContent('VPP Event Active');
            expect(statusCard).toHaveTextContent('Grid Services active (5.4 kW)');

            // Verify timeline item
            const timelineItem = document.querySelector('.timeline-item.mode-vpp');
            expect(timelineItem).toBeInTheDocument();
            expect(timelineItem).toHaveTextContent('VPP Event Active');
            expect(timelineItem).toHaveTextContent('Virtual Power Plant (VPP) event is currently active. Automation is temporarily disabled to allow grid services to run.');
        });
    });

    it('hides no change badges', async () => {
        const actions = [{
            description: 'Mixed modes test',
            timestamp: new Date().toISOString(),
            batteryMode: 0, // NoChange
            solarMode: 1, // NoExport
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Solar mode should be visible
            expect(screen.getByText('Use & No Export')).toBeInTheDocument();
            // Battery mode (NoChange) should NOT be visible as a badge/tag
            const badges = screen.queryAllByText((content, element) => {
                return element !== null && element.classList.contains('tag') && content === 'No Change';
            });
            expect(badges.length).toBe(0);
        });
    });

    it('groups consecutive fault actions into summary', async () => {
        const actions = [
            {
                description: 'Fault 1',
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                fault: true,
                systemStatus: {
                    alarms: [{ name: 'GridFault' }]
                },
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
            },
            {
                description: 'Fault 2',
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                fault: true,
                systemStatus: {
                    alarms: [{ name: 'InverterFault' }]
                },
                currentPrice: { dollarsPerKWH: 0.20, tsStart: '', tsEnd: '' }
            }
        ];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "System Fault" title/header
            expect(screen.getByRole('heading', { name: /System Fault/ })).toBeInTheDocument();
            // Should show alarm names - the order depends on Set iteration, usually insertion order
            // Since we add GridFault then InverterFault, it should be "GridFault, InverterFault"
            // However, regex is safer if order is not guaranteed, but usually it is for Sets of strings added in order.
            expect(screen.getByText(/Alarms: GridFault, InverterFault/)).toBeInTheDocument();
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
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "No Change" title/header
            expect(screen.getByRole('heading', { name: /No Change/ })).toBeInTheDocument();
            // Should show the latest action's description/reason
            expect(screen.getByText('No change 2')).toBeInTheDocument();
            expect(screen.queryByText('No change 1')).not.toBeInTheDocument();
            // Should show average price: (0.10 + 0.20) / 2 = 0.15
            expect(screen.getByText(/Avg Price:/)).toBeInTheDocument();
            expect(screen.getByText(/\$ 0.150\/kWh/)).toBeInTheDocument();
            // Should show range: 0.10 - 0.20
            expect(screen.getByText(/Range: \$ 0.100 - \$ 0.200/)).toBeInTheDocument();
        });
    });

    it('renders daily savings summary', async () => {
        const savingsData = {
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
            batteryUsed: 11,
        };
        mockActionsAndSavings([], savingsData);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText('Savings')).toBeInTheDocument();
            // Net savings: 5.50 (battery) + 5.00 (solar) + 1.00 (credit) = 11.50
            expect(screen.getByText(/\$ 11\.50/)).toBeInTheDocument();
            expect(screen.getByText('Solar')).toBeInTheDocument();
            expect(screen.getByText('+ $ 5.00')).toBeInTheDocument();
            expect(screen.getByText('Battery')).toBeInTheDocument();
            expect(screen.getByText('+ $ 5.50')).toBeInTheDocument();
            expect(screen.getByText('Export')).toBeInTheDocument();
            expect(screen.getByText('+ $ 1.00')).toBeInTheDocument();
        });

        await waitFor(() => {
            expect(screen.getByText('Home Usage')).toBeInTheDocument();
            expect(screen.getByText(/25\.0/)).toBeInTheDocument();
            expect(screen.getByText('Solar Gen')).toBeInTheDocument();
            expect(screen.getByText(/20\.0/)).toBeInTheDocument();
            expect(screen.getByText('Battery Use')).toBeInTheDocument();
            expect(screen.getByText(/11\.0/)).toBeInTheDocument();
            expect(screen.getByText(/Grid \(In\/Out\)/)).toBeInTheDocument();
            expect(screen.getByText('10.0')).toBeInTheDocument();
            expect(screen.getByText('5.0')).toBeInTheDocument();
            expect(screen.getAllByText(/kWh/).length).toBeGreaterThanOrEqual(3);
            expect(screen.getByText('Total Credit')).toBeInTheDocument();
            expect(screen.getAllByText('$ 1.00').length).toBeGreaterThanOrEqual(1);
            expect(screen.getByText('Total Cost')).toBeInTheDocument();
            expect(screen.getByText(/\$ 2\.00/)).toBeInTheDocument();
        });
    });

    it('renders negative savings correctly', async () => {
        const savingsData = {
            batterySavings: -2.50,
            solarSavings: 1.00,
            cost: 10.00,
            credit: -0.50, // Negative credit due to negative price
            avoidedCost: 0.50,
            chargingCost: 3.00,
            solarGenerated: 5,
            gridImported: 20,
            gridExported: 2,
            homeUsed: 23,
            batteryUsed: 5
        };
        mockActionsAndSavings([], savingsData);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Net savings: 1.00 (solar) - 2.50 (battery) - 0.50 (credit) = -2.00
            expect(screen.getByText('Savings')).toBeInTheDocument();
            const netValue = screen.getByText(/- \$ 2\.00/);
            expect(netValue).toBeInTheDocument();
            expect(netValue).toHaveClass('negative');

            expect(screen.getByText('Solar')).toBeInTheDocument();
            expect(screen.getByText('+ $ 1.00')).toBeInTheDocument();

            expect(screen.getByText('Battery')).toBeInTheDocument();
            const batteryValue = screen.getByText('- $ 2.50');
            expect(batteryValue).toBeInTheDocument();
            expect(batteryValue).toHaveClass('negative');

            expect(screen.getByText('Export')).toBeInTheDocument();
            const exportValues = screen.getAllByText('- $ 0.50');
            expect(exportValues.length).toBe(2);
            expect(exportValues[0]).toHaveClass('negative');
        });
    });


    it('shows banner when ESS credentials are missing', async () => {
        mockActionsAndSavings([], null);
        const settings = {
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: false },
            utilityProvider: 'comed_besh',
            countryCode: 'US',
            postalCode: '60601'
        };

        renderWithRouter(<Dashboard settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByText(/Energy Storage System is not connected/i)).toBeInTheDocument();
            expect(screen.getByText(/Configure it in Settings/i)).toBeInTheDocument();
        });
    });

    it('shows ESS authentication warning banner when consecutive failures >= 3', async () => {
        mockActionsAndSavings([]);
        const settings = {
            minBatterySOC: 10,
            utilityProvider: 'test',
            ess: 'franklinwh',
            hasCredentials: { franklinwh: true },
            essAuthStatus: {
                consecutiveFailures: 3,
                lastAttempt: '2023-01-01T12:00:00Z'
            }
        };

        renderWithRouter(<Dashboard settings={settings as any} />);

        await waitFor(() => {
            const warningTags = screen.getAllByText(/Warning:/);
            expect(warningTags.length).toBeGreaterThan(0);
            const failureTags = screen.getAllByText(/Energy Storage System authentication failed 3 time\(s\)\./);
            expect(failureTags.length).toBeGreaterThan(0);
        });
    });

    it('does not show ESS authentication warning banner when consecutive failures < 3', async () => {
        mockActionsAndSavings([]);
        const settings = {
            minBatterySOC: 10,
            utilityProvider: 'test',
            ess: 'franklinwh',
            hasCredentials: { franklinwh: true },
            essAuthStatus: {
                consecutiveFailures: 2,
                lastAttempt: '2023-01-01T12:00:00Z'
            }
        };

        renderWithRouter(<Dashboard settings={settings as any} />);

        await waitFor(() => {
            expect(screen.queryByText(/Warning:/)).not.toBeInTheDocument();
            expect(screen.queryByText(/Energy Storage System authentication failed/)).not.toBeInTheDocument();
        });
    });

    it('does not show banner when ESS credentials are present', async () => {
        mockActionsAndSavings([], null);
        const settings = {
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: true },
            utilityProvider: 'comed_besh'
        };

        renderWithRouter(<Dashboard settings={settings as any} />);

        await waitFor(() => {
            // Need to wait for loading to finish
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        expect(screen.queryByText(/Energy Storage System is not connected/i)).not.toBeInTheDocument();
    });

    it('shows banner when Utility Provider is missing', async () => {
        mockActionsAndSavings([], null);
        const settings = {
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: true },
            utilityProvider: ''
        };

        renderWithRouter(<Dashboard settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByText(/Utility Provider is not configured/i)).toBeInTheDocument();
            expect(screen.getByText(/Configure it in Settings/i)).toBeInTheDocument();
        });
    });

    it('does not show banner when Utility Provider is present', async () => {
        mockActionsAndSavings([], null);
        const settings = {
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: true },
            utilityProvider: 'comed_besh'
        };

        renderWithRouter(<Dashboard settings={settings as any} />);

        await waitFor(() => {
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        expect(screen.queryByText(/Utility Provider is not configured/i)).not.toBeInTheDocument();

    });

    it('renders manual emergency mode correctly', async () => {
        const actions = [{
            reason: 'emergencyMode',
            description: 'Emergency Mode Active',
            timestamp: new Date().toISOString(),
            batteryMode: 0,
            solarMode: 0,
            fault: true,
            systemStatus: {
                alarms: [],
                storm: []
            },
            currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Emergency Mode/ })).toBeInTheDocument();
            expect(screen.getByText('System manually put into emergency mode. Skipping automation.')).toBeInTheDocument();
        });
    });

    it('renders storm protection mode correctly with times', async () => {
        const stormStart = new Date('2023-01-01T12:00:00');
        const stormEnd = new Date('2023-01-01T15:00:00');
        const actions = [{
            reason: 'emergencyMode',
            description: 'Storm Prep',
            timestamp: new Date('2023-01-01T10:00:00').toISOString(),
            batteryMode: 0,
            solarMode: 0,
            fault: true,
            systemStatus: {
                alarms: [],
                storms: [{
                    description: 'Thunderstorm',
                    tsStart: stormStart.toISOString(),
                    tsEnd: stormEnd.toISOString(),
                }]
            },
            currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Storm Hedge Mode/ })).toBeInTheDocument();
            expect(screen.getByText('Charging the battery to prepare for the storm.')).toBeInTheDocument();
            expect(screen.getByText(/Storm Duration: 12:00 PM - 3:00 PM/)).toBeInTheDocument();
        });
    });

    it('hides price footer in summary when no price data is available', async () => {
        const actions = [{
            reason: 'emergencyMode',
            description: 'Emergency Mode Active',
            timestamp: new Date().toISOString(),
            batteryMode: 0,
            solarMode: 0,
            fault: true,
            systemStatus: {
                alarms: [],
                storms: []
            },
            // currentPrice is undefined
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Emergency Mode/ })).toBeInTheDocument();
            // Price label should not be present
            expect(screen.queryByText('Avg Price:')).not.toBeInTheDocument();
        });
    });

    it('hides CurrentStatus on previous dates', async () => {
        const user = userEvent.setup();
        const now = new Date();
        const yesterday = new Date(now);
        yesterday.setDate(yesterday.getDate() - 1);

        mockActionsAndSavings([]);
        renderWithRouter(<Dashboard />);

        // Wait for initial load
        await waitFor(() => {
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        // Click Prev button to go to yesterday
        const prevButton = screen.getByText(/Prev/);
        await user.click(prevButton);

        // Verification of navigation happened
        await waitFor(() => {
            const hasDateParam = window.location.search.includes('date=');
            expect(hasDateParam).toBe(true);
        });

        // Mock actions for yesterday
        const actions = [{
            timestamp: yesterday.toISOString(),
            batteryMode: 0,
            solarMode: 0,
            targetBatteryMode: 2,
            systemStatus: {
                batterySOC: 50,
                batteryKW: -5.0,
            },
            currentPrice: { dollarsPerKWH: 0.05, tsStart: '', tsEnd: '' },
        }];
        mockActionsAndSavings(actions);

        await waitFor(() => {
            // Should NOT show CurrentStatus card on a non-today date
            expect(document.querySelector('.current-status-card')).not.toBeInTheDocument();
        });
    });

    it('renders CurrentStatus using targetBatteryMode when batteryMode is NoChange', async () => {
        const now = new Date();
        const actions = [{
            timestamp: now.toISOString(),
            batteryMode: 0, // NoChange
            solarMode: 0,
            targetBatteryMode: 2, // ChargeAny
            systemStatus: {
                batterySOC: 50,
                batteryKW: -5.0, // Charging
            },
            currentPrice: { dollarsPerKWH: 0.05, tsStart: '', tsEnd: '' },
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "System Charging" because targetBatteryMode is ChargeAny
            expect(screen.getByText('System Charging')).toBeInTheDocument();

            // Should show the specific label in the CurrentStatus component
            const statusCard = document.querySelector('.current-status-card');
            expect(statusCard).toBeInTheDocument();
            expect(statusCard?.textContent).toContain('Charge From Solar+Grid');
        });
    });

    it('shows pills for targetBatteryMode even when batteryMode is NoChange', async () => {
        const actions = [{
            description: 'Target mode test',
            timestamp: new Date().toISOString(),
            batteryMode: 0, // NoChange
            solarMode: 0,
            targetBatteryMode: -1, // Load
            targetSolarMode: 1, // NoExport
        }];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "Solar first, then battery" and "No Export" pills
            const useBatteryPills = screen.getAllByText('Solar first, then battery');
            expect(useBatteryPills.some(p => p.classList.contains('tag'))).toBe(true);

            const noExportPills = screen.getAllByText('Use & No Export');
            expect(noExportPills.some(p => p.classList.contains('tag'))).toBe(true);
        });
    });

    it('shows target mode pills in summaries', async () => {
        const actions = [
            {
                description: 'No change 1',
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                targetBatteryMode: 1, // Standby
                targetSolarMode: 1, // NoExport
            },
            {
                description: 'No change 2',
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                targetBatteryMode: 1, // Standby
                targetSolarMode: 1, // NoExport
            }
        ];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /No Change/ })).toBeInTheDocument();
            // Should show pills in the summary item
            const holdBatteryPills = screen.getAllByText('Hold Battery');
            expect(holdBatteryPills.some(p => p.classList.contains('tag'))).toBe(true);

            const noExportPills = screen.getAllByText('Use & No Export');
            expect(noExportPills.some(p => p.classList.contains('tag'))).toBe(true);
        });
    });

    it('renders action summaries with price range, latest battery charge, and info tags', async () => {
        const actions = [
            {
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '2023-01-01T10:00:00Z', tsEnd: '' },
                systemStatus: { batterySOC: 40 },
                deficitAt: '2023-01-01T15:00:00Z',
            },
            {
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.20, tsStart: '2023-01-01T10:30:00Z', tsEnd: '' },
                systemStatus: { batterySOC: 45 },
                deficitAt: '2023-01-01T16:00:00Z',
                capacityAt: '2023-01-01T18:00:00Z',
            }
        ];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /No Change/ })).toBeInTheDocument();

            // Should show average price and range
            expect(screen.getByText(/Avg Price:/)).toBeInTheDocument();
            expect(screen.getByText(/\$ 0\.150\/kWh/)).toBeInTheDocument();
            expect(screen.getByText(/Range: \$ 0\.100 - \$ 0\.200/)).toBeInTheDocument();

            // Should show average SOC and range
            expect(screen.getByText(/Battery:/)).toBeInTheDocument();
            expect(screen.getByText(/42\.5%/)).toBeInTheDocument();
            expect(screen.getByText(/Range: 40% - 45%/)).toBeInTheDocument();

            // Should show Empty and Full tags from the LATEST action
            // Using a more flexible regex for time as it depends on local timezone
            expect(screen.getByText(/Empty:/)).toBeInTheDocument();
            expect(screen.getByText(/Full:/)).toBeInTheDocument();

            // Check that some element contains the Empty/Full text with some time format
            const tags = document.querySelectorAll('.tag-info');
            const tagTexts = Array.from(tags).map(t => t.textContent);
            expect(tagTexts.some(t => t?.includes('Empty:'))).toBe(true);
            expect(tagTexts.some(t => t?.includes('Full:'))).toBe(true);
        });
    });

    it('hides paused actions from the dashboard timeline', async () => {
        const now = new Date();
        const actions = [
            {
                reason: 'sufficientBattery',
                description: 'Normal action',
                timestamp: new Date(now.getTime() - 60000).toISOString(),
                batteryMode: -1, // Load
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' },
                paused: false,
            },
            {
                description: 'Automation is paused',
                timestamp: now.toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.12, tsStart: '', tsEnd: '' },
                paused: true,
            }
        ];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // The paused action description should NOT appear in the action list
            expect(screen.queryByText('Automation is paused')).not.toBeInTheDocument();
        });
    });

    it('does not group VPPActive actions with sufficientBattery actions', async () => {
        const now = new Date();
        const actions = [
            {
                reason: 'sufficientBattery',
                description: 'Normal action',
                timestamp: new Date(now.getTime() - 60000).toISOString(),
                batteryMode: 4, // Load
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' },
                paused: false,
            },
            {
                reason: 'vppActive',
                description: 'VPP event active',
                timestamp: now.toISOString(),
                batteryMode: 0, // NoChange
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.12, tsStart: '', tsEnd: '' },
                paused: false,
            }
        ];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText(/The battery has enough stored energy/)).toBeInTheDocument();
            expect(screen.getByText(/Virtual Power Plant/)).toBeInTheDocument();
        });
    });


    it('shows paused indicator in CurrentStatus when last action is paused', async () => {
        const now = new Date();
        const actions = [
            {
                description: 'Automation is paused',
                timestamp: now.toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.12, tsStart: '', tsEnd: '' },
                systemStatus: {
                    batterySOC: 65,
                    batteryKW: 0,
                },
                paused: true,
            }
        ];
        mockActionsAndSavings(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // CurrentStatus card should show "System Paused"
            expect(screen.getByText('System Paused')).toBeInTheDocument();
            expect(screen.getByText('Automation is currently paused')).toBeInTheDocument();

            // The status card should have the "paused" CSS class
            const statusCard = document.querySelector('.current-status-card');
            expect(statusCard).toBeInTheDocument();
            expect(statusCard?.classList.contains('paused')).toBe(true);

            // Should still show battery SOC and price
            expect(screen.getAllByText(/65\.0%/).length).toBeGreaterThan(0);
        });
    });


    it('renders only savings in overview mode (siteID=ALL)', async () => {
        const savingsData = {
            batterySavings: 10,
            solarSavings: 20,
            cost: 5,
            credit: 2,
            avoidedCost: 12,
            chargingCost: 2,
            solarGenerated: 30,
            gridImported: 15,
            gridExported: 5,
            homeUsed: 40,
            batteryUsed: 10,
        };
        mockActionsAndSavings([{ description: 'Should not show' }], savingsData);

        renderWithRouter(<Dashboard siteID="ALL" settings={null} />);

        await waitFor(() => {
            expect(screen.getByText('Savings')).toBeInTheDocument();
            expect(screen.getByText(/\$ 32\.00/)).toBeInTheDocument(); // 10+20+2
        });

        // Should NOT show current status or actions
        expect(document.querySelector('.current-status-card')).not.toBeInTheDocument();
        expect(screen.queryByText('Should not show')).not.toBeInTheDocument();
        expect(document.querySelector('.action-list')).not.toBeInTheDocument();
    });

    it("shows What's New banner when not dismissed", async () => {
        localStorage.clear();
        mockActionsAndSavings([]);
        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText("What's New:")).toBeInTheDocument();
            expect(screen.getByText((content) => content.includes(whatsNewText))).toBeInTheDocument();
            if (whatsNewLinkText) {
                const link = screen.getByRole('link', { name: whatsNewLinkText });
                expect(link).toBeInTheDocument();
                expect(link).toHaveAttribute('href', '/settings');
            }
        });
    });

    it("can dismiss the What's New banner", async () => {
        const user = userEvent.setup();
        localStorage.clear();
        mockActionsAndSavings([]);
        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText("What's New:")).toBeInTheDocument();
        });

        const dismissBtn = screen.getByRole('button', { name: /Dismiss What's New banner/i });
        await user.click(dismissBtn);

        await waitFor(() => {
            expect(screen.queryByText("What's New:")).not.toBeInTheDocument();
        });
        expect(localStorage.getItem('whats_new_banner_version')).toBe(whatsNewVersion.toString());
    });

    it('shows grid restrictions warning banner when all features are unchecked and utility is present', async () => {
        localStorage.clear();
        mockActionsAndSavings([]);
        const settings = {
            utilityProvider: 'comed',
            gridChargeBatteries: false,
            gridExportSolar: false,
            gridExportBatteries: false,
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByTestId('grid-restrictions-warning-banner')).toBeInTheDocument();
            expect(screen.getByText(/All grid features are disabled/i)).toBeInTheDocument();
        });
    });

    it('does not show grid restrictions warning banner when utility is missing', async () => {
        localStorage.clear();
        mockActionsAndSavings([]);
        const settings = {
            utilityProvider: '',
            gridChargeBatteries: false,
            gridExportSolar: false,
            gridExportBatteries: false,
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        expect(screen.queryByTestId('grid-restrictions-warning-banner')).not.toBeInTheDocument();
    });

    it('can dismiss the grid restrictions warning banner', async () => {
        const user = userEvent.setup();
        localStorage.clear();
        mockActionsAndSavings([]);
        const settings = {
            utilityProvider: 'comed',
            gridChargeBatteries: false,
            gridExportSolar: false,
            gridExportBatteries: false,
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByTestId('grid-restrictions-warning-banner')).toBeInTheDocument();
        });

        const dismissBtn = screen.getByRole('button', { name: /Dismiss grid restrictions warning/i });
        await user.click(dismissBtn);

        await waitFor(() => {
            expect(screen.queryByTestId('grid-restrictions-warning-banner')).not.toBeInTheDocument();
        });
        expect(localStorage.getItem('grid_restrictions_warning_dismissed_site1')).toBe('true');
    });

    it('shows location warning banner when location is missing and utility is present', async () => {
        localStorage.clear();
        mockActionsAndSavings([]);
        const settings = {
            utilityProvider: 'comed',
            countryCode: '',
            postalCode: '',
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByTestId('location-warning-banner')).toBeInTheDocument();
            expect(screen.getByText(/Location is not specified/i)).toBeInTheDocument();
        });
    });

    it('does not show location warning banner when utility is missing', async () => {
        localStorage.clear();
        mockActionsAndSavings([]);
        const settings = {
            utilityProvider: '',
            countryCode: '',
            postalCode: '',
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        expect(screen.queryByTestId('location-warning-banner')).not.toBeInTheDocument();
    });

    it('can dismiss the location warning banner', async () => {
        const user = userEvent.setup();
        localStorage.clear();
        mockActionsAndSavings([]);
        const settings = {
            utilityProvider: 'comed',
            countryCode: '',
            postalCode: '',
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByTestId('location-warning-banner')).toBeInTheDocument();
        });

        const dismissBtn = screen.getByRole('button', { name: /Dismiss location warning/i });
        await user.click(dismissBtn);

        await waitFor(() => {
            expect(screen.queryByTestId('location-warning-banner')).not.toBeInTheDocument();
        });
        expect(localStorage.getItem('location_warning_dismissed_site1')).toBe('true');
    });

    it('shows warning when ESS is connected and automation is paused', async () => {
        mockActionsAndSavings([]);
        const settings = {
            ess: 'franklin',
            hasCredentials: { franklin: true },
            pause: true,
            dryRun: false,
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByTestId('automation-paused-dryrun-warning-banner')).toBeInTheDocument();
            expect(screen.getByText(/Automation is paused\. The system will not perform any automated actions/i)).toBeInTheDocument();
        });
    });

    it('shows warning when ESS is connected and dry run is enabled', async () => {
        mockActionsAndSavings([]);
        const settings = {
            ess: 'franklin',
            hasCredentials: { franklin: true },
            pause: false,
            dryRun: true,
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByTestId('automation-paused-dryrun-warning-banner')).toBeInTheDocument();
            expect(screen.getByText(/Dry Run is enabled\. The system will simulate actions but will not write/i)).toBeInTheDocument();
        });
    });

    it('shows combined warning when ESS is connected and both pause and dry run are active', async () => {
        mockActionsAndSavings([]);
        const settings = {
            ess: 'franklin',
            hasCredentials: { franklin: true },
            pause: true,
            dryRun: true,
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.getByTestId('automation-paused-dryrun-warning-banner')).toBeInTheDocument();
            expect(screen.getByText(/Automation is paused and Dry Run is enabled/i)).toBeInTheDocument();
        });
    });

    it('does not show paused/dryrun warning when ESS is not connected', async () => {
        mockActionsAndSavings([]);
        const settings = {
            ess: 'franklin',
            hasCredentials: { franklin: false },
            pause: true,
            dryRun: true,
        };

        renderWithRouter(<Dashboard siteID="site1" settings={settings as any} />);

        await waitFor(() => {
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        expect(screen.queryByTestId('automation-paused-dryrun-warning-banner')).not.toBeInTheDocument();
    });
});
