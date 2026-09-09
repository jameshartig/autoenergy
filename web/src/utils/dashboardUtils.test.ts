import { describe, it, expect } from 'vitest';
import {
    getBatteryModeLabel,
    formatPrice,
    formatCurrency,
    gridChargeCost,
    getReasonText,
    formatTime,
    extractOffsetMinutes,
    formatTimeInOffset,
    getActionTimestamp,
    isZeroTime,
    formatHour12
} from './dashboardUtils';
import { BatteryMode, SolarMode, ActionReason, type Action } from '../api';

describe('dashboardUtils', () => {
    describe('getBatteryModeLabel', () => {
        it('returns correct label for standby', () => {
            expect(getBatteryModeLabel(BatteryMode.Standby)).toBe('Hold Battery');
        });
        it('returns Unknown for invalid mode', () => {
            expect(getBatteryModeLabel(999)).toBe('Unknown');
        });
    });

    describe('formatPrice', () => {
        it('formats dollars to price string', () => {
            expect(formatPrice(0.1234)).toBe('$ 0.123/kWh');
        });
    });

    describe('formatCurrency', () => {
        it('formats positive amount', () => {
            expect(formatCurrency(10.5)).toBe('$ 10.50');
        });
        it('formats negative amount', () => {
            expect(formatCurrency(-5.25)).toBe('- $ 5.25');
        });
        it('formats with forceSign', () => {
            expect(formatCurrency(3.21, true)).toBe('+ $ 3.21');
        });
    });

    describe('gridChargeCost', () => {
        it('sums base price and grid use adder', () => {
            expect(gridChargeCost({ dollarsPerKWH: 0.1, gridUseDollarsPerKWH: 0.05 })).toBeCloseTo(0.15);
        });
        it('handles missing grid use adder', () => {
            expect(gridChargeCost({ dollarsPerKWH: 0.2 })).toBeCloseTo(0.2);
        });
    });

    describe('getReasonText', () => {
        const baseAction: Action = {
            description: 'Fallback',
            timestamp: '',
            batteryMode: BatteryMode.Standby,
            solarMode: SolarMode.NoExport
        };

        it('returns description if no reason is present', () => {
            expect(getReasonText(baseAction)).toBe('Fallback');
        });

        it('handles SufficientBattery', () => {
             const action = { ...baseAction, reason: ActionReason.SufficientBattery };
             expect(getReasonText(action)).toContain('battery has enough stored energy');
        });

        it('handles SufficientBatteryTillCharge with prices and savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.SufficientBatteryTillCharge,
                deficitAt: '2026-05-20T19:24:00-05:00',
                currentPrice: { dollarsPerKWH: 0.15, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('If we rely on the battery, it would deplete');
            expect(text).toContain('cheaper charging window is coming up');
            expect(text).toContain('$ 0.050');
            expect(text).toContain('$ 0.150');
            expect(text).toContain('savings: $ 0.100/kWh.');
        });

        it('handles SufficientBatteryTillCharge with identical prices or less than 1 cent margin', () => {
            const action1 = {
                ...baseAction,
                reason: ActionReason.SufficientBatteryTillCharge,
                deficitAt: '2026-05-20T19:24:00-05:00',
                currentPrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text1 = getReasonText(action1);
            expect(text1).toContain('charging window with the same price is coming up');
            expect(text1).toContain('waiting to refill it during that window');
            expect(text1).not.toContain('savings:');

            const action2 = {
                ...baseAction,
                reason: ActionReason.SufficientBatteryTillCharge,
                deficitAt: '2026-05-20T19:24:00-05:00',
                currentPrice: { dollarsPerKWH: 0.060, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text2 = getReasonText(action2);
            expect(text2).toContain('charging window with the same price is coming up');
            expect(text2).toContain('waiting to refill it during that window');
            expect(text2).not.toContain('savings:');
        });

        it('handles DeficitCharge with prices and savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitCharge,
                currentPrice: { dollarsPerKWH: 0.1, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.5, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('Charging now');
            expect(text).toContain('$ 0.100');
            expect(text).toContain('savings: $ 0.400/kWh.');
        });

        it('handles DeficitCharge with identical prices or less than 1 cent margin', () => {
            const action1 = {
                ...baseAction,
                reason: ActionReason.DeficitCharge,
                currentPrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text1 = getReasonText(action1);
            expect(text1).toContain('same price as');
            expect(text1).not.toContain('cheaper than');
            expect(text1).not.toContain('savings:');

            const action2 = {
                ...baseAction,
                reason: ActionReason.DeficitCharge,
                currentPrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.060, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text2 = getReasonText(action2);
            expect(text2).toContain('same price as');
            expect(text2).not.toContain('cheaper than');
            expect(text2).not.toContain('savings:');
        });

        it('handles PreventSolarCurtailment', () => {
            const action = { ...baseAction, reason: ActionReason.PreventSolarCurtailment };
            expect(getReasonText(action)).toContain('exceed battery capacity');
        });

        it('handles HoldSimilarPrice', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.HoldSimilarPrice,
                currentPrice: { dollarsPerKWH: 0.21, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('Current electricity price ($ 0.210/kWh) is comparable to the expected export credit');
            expect(text).toContain('export surplus solar to the grid');
        });

        it('handles GridUnavailable', () => {
            const action = { ...baseAction, reason: ActionReason.GridUnavailable };
            expect(getReasonText(action)).toContain('Grid is currently unavailable');
        });

        it('handles VPPActive', () => {
            const action = { ...baseAction, reason: ActionReason.VPPActive };
            expect(getReasonText(action)).toContain('Virtual Power Plant (VPP) event is currently active');
        });

        it('handles VPPPrep', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.VPPPrep,
                currentPrice: { dollarsPerKWH: 0.08, gridUseDollarsPerKWH: 0.02, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.25, gridUseDollarsPerKWH: 0.05, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('Preparing for upcoming Virtual Power Plant (VPP) event.');
            expect(text).toContain('Charging the battery now at $ 0.100/kWh is cheaper than charging later before the event at $ 0.300/kWh');
        });

        it('handles DeficitSaveForPeak with valid deficitAt', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '2026-06-16T08:33:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('If we rely on the battery, it would deplete around');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
        });

        it('handles DeficitSaveForPeak falling back to hitBufferedDeficitAt when deficitAt is zero', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '0001-01-01T00:00:00Z',
                hitBufferedDeficitAt: '2026-06-16T08:33:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('If we rely on the battery, it would deplete around');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
        });

        it('handles DeficitSaveForPeak with no depletion message when all deficit times are zero', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '0001-01-01T00:00:00Z',
                hitThresholdDeficitAt: '0001-01-01T00:00:00Z',
                hitBufferedDeficitAt: '0001-01-01T00:00:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).not.toContain('If we rely on the battery, it would deplete');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
        });


        it('handles WaitingToCharge with significant savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.WaitingToCharge,
                currentPrice: { dollarsPerKWH: 0.15, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('A cheaper charging window is coming up');
            expect(text).toContain('savings: $ 0.100/kWh.');
        });

        it('handles WaitingToCharge with < $0.01 savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.WaitingToCharge,
                currentPrice: { dollarsPerKWH: 0.092, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.091, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('A charging window is coming up which is similar in price or cheaper than now');
            expect(text).not.toContain('savings:');
            expect(text).not.toContain('$ 0.091');
            expect(text).not.toContain('$ 0.092');
        });

        it('appends NoExport suffix for arbitrage', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.ArbitrageChargeSave,
                solarMode: SolarMode.NoExport,
                currentPrice: { dollarsPerKWH: -0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            expect(getReasonText(action)).toContain('Disabled solar export');
        });

        it('explains EV charging standby correctly', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.EVChargingStandby,
            };
            const text = getReasonText(action);
            expect(text).toContain('EV charging detected');
            expect(text).toContain('preserving battery reserves');
        });
    });

    describe('formatTime & offset helpers', () => {
        it('extracts offset minutes correctly', () => {
            expect(extractOffsetMinutes('2026-07-21T20:39:04-05:00')).toBe(-300);
            expect(extractOffsetMinutes('2026-07-21T20:39:04+02:00')).toBe(120);
            expect(extractOffsetMinutes('2026-07-21T20:39:04Z')).toBeNull();
            expect(extractOffsetMinutes('')).toBeNull();
        });

        it('formats ISO string with explicit offset minutes', () => {
            expect(formatTimeInOffset('2026-07-22T01:39:04Z', -300)).toBe('8:39 PM');
            expect(formatTimeInOffset('2026-07-22T14:30:00Z', 120)).toBe('4:30 PM');
            expect(formatTimeInOffset('2026-07-22T05:05:00Z', -300)).toBe('12:05 AM');
        });

        it('formatTime uses offset embedded in timestamp or reference timestamp', () => {
            expect(formatTime('2026-07-21T20:39:04-05:00')).toBe('8:39 PM');
            expect(formatTime('2026-07-22T01:39:04Z', '2026-07-21T20:39:04-05:00')).toBe('8:39 PM');
            expect(formatTime('')).toBe('');
        });

        it('ignores zero systemTimestamp (0001-01-01) and falls back to systemStatus timestamp offset', () => {
            const action: Action = {
                timestamp: '2026-07-22T04:09:26.947167547Z',
                systemTimestamp: '0001-01-01T00:00:00Z',
                batteryMode: 1,
                solarMode: 2,
                description: 'test',
                systemStatus: {
                    timestamp: '2026-07-22T00:09:26.947167547-04:00'
                }
            };
            const refTs = (action.systemTimestamp && !isZeroTime(action.systemTimestamp)) ? action.systemTimestamp : action.systemStatus?.timestamp;
            const targetTs = getActionTimestamp(action);
            expect(targetTs).toBe('2026-07-22T04:09:26.947167547Z');
            expect(formatTime(targetTs, refTs)).toBe('12:09 AM');
        });
    });

    describe('formatHour12', () => {
        it('formats midnight and midday correctly', () => {
            expect(formatHour12(0)).toBe('12:00 AM');
            expect(formatHour12(12)).toBe('12:00 PM');
        });

        it('formats morning and evening hours in 12-hour time without leading zeros', () => {
            expect(formatHour12(7)).toBe('7:00 AM');
            expect(formatHour12(13)).toBe('1:00 PM');
            expect(formatHour12(20)).toBe('8:00 PM');
            expect(formatHour12(23)).toBe('11:00 PM');
        });
    });
});
