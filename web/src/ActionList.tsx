
import React, { useEffect, useState, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { fetchActions, fetchSavings, type Action, type SavingsStats, BatteryMode, SolarMode } from './api';

const getBatteryModeLabel = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'Hold Battery';
        case BatteryMode.ChargeAny: return 'Charge From Solar+Grid';
        case BatteryMode.ChargeSolar: return 'Charge From Solar';
        case BatteryMode.Load: return 'Use Battery';
        case BatteryMode.NoChange: return 'No Change';
        default: return 'Unknown';
    }
};

const getBatteryModeClass = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'standby';
        case BatteryMode.ChargeAny: return 'charge_any';
        case BatteryMode.ChargeSolar: return 'charge_solar';
        case BatteryMode.Load: return 'load';
        case BatteryMode.NoChange: return 'no_change';
        default: return 'unknown';
    }
};

const getSolarModeLabel = (mode: number) => {
    switch (mode) {
        case SolarMode.NoExport: return 'No Export';
        case SolarMode.Any: return 'Any';
        case SolarMode.NoChange: return 'No Change';
        default: return 'Unknown';
    }
};

const getSolarModeClass = (mode: number) => {
    switch (mode) {
        case SolarMode.NoExport: return 'no_export';
        case SolarMode.Any: return 'any';
        case SolarMode.NoChange: return 'no_change';
        default: return 'unknown';
    }
};

const ActionList: React.FC = () => {
    const [searchParams, setSearchParams] = useSearchParams();
    const dateQuery = searchParams.get('date');
    const [actions, setActions] = useState<Action[]>([]);
    const [savings, setSavings] = useState<SavingsStats | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    const currentDate = useMemo(() => {
        if (dateQuery) {
            const parts = dateQuery.split('-');
            if (parts.length === 3) {
                const year = parseInt(parts[0], 10);
                const month = parseInt(parts[1], 10) - 1;
                const day = parseInt(parts[2], 10);
                return new Date(year, month, day);
            }
        }
        return new Date();
    }, [dateQuery]);

    useEffect(() => {
        const loadData = async () => {
            setLoading(true);
            setError(null);
            try {
                // Calculate start and end of the day in local time
                const start = new Date(currentDate);
                start.setHours(0, 0, 0, 0);
                const end = new Date(currentDate);
                end.setHours(23, 59, 59, 999);

                // Fetch both actions and savings in parallel
                const [actionsData, savingsData] = await Promise.all([
                    fetchActions(start, end),
                    fetchSavings(start, end)
                ]);

                setActions(actionsData || []);
                setSavings(savingsData);
            } catch (err) {
                console.error(err);
                setError('Failed to load data');
            } finally {
                setLoading(false);
            }
        };

        loadData();
    }, [currentDate]);

    const handleDateChange = (days: number) => {
        const newDate = new Date(currentDate);
        newDate.setDate(newDate.getDate() + days);
        const year = newDate.getFullYear();
        const month = String(newDate.getMonth() + 1).padStart(2, '0');
        const day = String(newDate.getDate()).padStart(2, '0');
        setSearchParams({ date: `${year}-${month}-${day}` });
    };

    // Format date for display
    const formattedDate = currentDate.toLocaleDateString(undefined, {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric'
    });

    const netSavings = savings ? savings.batterySavings + savings.solarSavings : 0;

    const groupedActions = useMemo(() => {
        const result: (Action | { isSummary: true; startTime: string; avgPrice: number; min: number; max: number; count: number })[] = [];
        let currentSummary: { startTime: string; count: number; total: number; min: number; max: number } | null = null;

        for (const action of actions) {
            const isNoChange = action.batteryMode === BatteryMode.NoChange && action.solarMode === SolarMode.NoChange;

            if (isNoChange) {
                const price = action.currentPrice ? action.currentPrice.dollarsPerKWH : 0;
                if (!currentSummary) {
                    currentSummary = {
                        startTime: action.timestamp,
                        count: 1,
                        total: price,
                        min: price,
                        max: price
                    };
                } else {
                    currentSummary.count++;
                    currentSummary.total += price;
                    currentSummary.min = Math.min(currentSummary.min, price);
                    currentSummary.max = Math.max(currentSummary.max, price);
                }
            } else {
                if (currentSummary) {
                    const summary = currentSummary;
                    result.push({
                        isSummary: true,
                        startTime: summary.startTime,
                        count: summary.count,
                        avgPrice: summary.total / summary.count,
                        min: summary.min,
                        max: summary.max
                    });
                    currentSummary = null;
                }
                result.push(action);
            }
        }

        if (currentSummary) {
            const summary = currentSummary;
            result.push({
                isSummary: true,
                startTime: summary.startTime,
                count: summary.count,
                avgPrice: summary.total / summary.count,
                min: summary.min,
                max: summary.max
            });
        }

        return result;
    }, [actions]);

    return (
        <div className="action-list-container">
            <header className="header">
                <button onClick={() => handleDateChange(-1)} disabled={loading}>&lt; Prev</button>
                <h2>{formattedDate}</h2>
                <button onClick={() => handleDateChange(1)} disabled={loading || currentDate.toDateString() === new Date().toDateString()}>Next &gt;</button>
            </header>

            {loading && <p>Loading day...</p>}
            {error && <p className="error">{error}</p>}

            {!loading && !error && (
                <>
                    {savings && (
                        <div className="savings-summary">
                            <div className="savings-header">
                                <h3>Daily Overview</h3>
                            </div>
                            <div className="savings-grid">
                                <div className="savings-item">
                                    <span className="savings-label">Net Savings</span>
                                    <span className={`savings-value ${netSavings > 0 ? 'positive' : netSavings < 0 ? 'negative' : 'neutral'}`}>
                                        ${netSavings.toFixed(2)}
                                    </span>
                                </div>
                                <div className="savings-item">
                                    <span className="savings-label">Solar Savings</span>
                                    <span className="savings-value positive">
                                        ${savings.solarSavings.toFixed(2)}
                                    </span>
                                </div>
                                <div className="savings-item">
                                    <span className="savings-label">Battery Savings</span>
                                    <span className={`savings-value ${savings.batterySavings > 0 ? 'positive' : savings.batterySavings < 0 ? 'negative' : 'neutral'}`}>
                                        ${savings.batterySavings.toFixed(2)}
                                    </span>
                                </div>
                                <div className="savings-item">
                                    <span className="savings-label">Total Cost</span>
                                    <span className="savings-value">
                                        ${savings.cost.toFixed(2)}
                                    </span>
                                </div>
                                <div className="savings-item">
                                    <span className="savings-label">Credit</span>
                                    <span className="savings-value">
                                        ${savings.credit.toFixed(2)}
                                    </span>
                                </div>
                                <div className="savings-item">
                                    <span className="savings-label">Avoided Cost</span>
                                    <span className="savings-value">
                                        ${savings.avoidedCost.toFixed(2)}
                                    </span>
                                </div>
                            </div>
                            <div className="savings-details">
                                <span><strong>Home:</strong> {savings.homeUsed.toFixed(2)} kWh</span>
                                <span><strong>Solar:</strong> {savings.solarGenerated.toFixed(2)} kWh</span>
                                <span><strong>Grid Import:</strong> {savings.gridImported.toFixed(2)} kWh</span>
                                <span><strong>Grid Export:</strong> {savings.gridExported.toFixed(2)} kWh</span>
                                <span><strong>Battery Use:</strong> {savings.batteryUsed.toFixed(2)} kWh</span>
                            </div>
                        </div>
                    )}

                    {actions && actions.length === 0 && <p className="no-actions">No actions recorded for this day.</p>}

                    <ul className="action-list">
                        {groupedActions.map((item, index) => {
                            if ('isSummary' in item) {
                                const summary = item as any;
                                const showRange = summary.min !== summary.max;
                                return (
                                    <li key={index} className="action-item summary-item">
                                        <div className="action-time">
                                            {new Date(summary.startTime).toLocaleTimeString()}
                                        </div>
                                        <div className="action-details">
                                            <h3>No Change {summary.count > 1 && <span>({summary.count}x)</span>}</h3>
                                            <div className="action-footer">
                                                <span className="price-label">Avg Price:</span> ${summary.avgPrice.toFixed(3)}/kWh
                                                {showRange && <span className="price-range"> (Range: ${summary.min.toFixed(3)} - ${summary.max.toFixed(3)})</span>}
                                            </div>
                                        </div>
                                    </li>
                                );
                            }
                            const action = item as Action;
                            return (
                                <li key={index} className="action-item">
                                    <div className="action-time">
                                        {new Date(action.timestamp).toLocaleTimeString()}
                                    </div>
                                    <div className="action-details">
                                        <h3>{getBatteryModeLabel(action.batteryMode)}</h3>
                                        <p>{action.description}</p>
                                        <div className="tags">
                                            {action.batteryMode !== BatteryMode.NoChange && (
                                                <span className={`tag mode-${getBatteryModeClass(action.batteryMode)}`}>{getBatteryModeLabel(action.batteryMode)}</span>
                                            )}
                                            {action.solarMode !== SolarMode.NoChange && (
                                                <span className={`tag solar-${getSolarModeClass(action.solarMode)}`}>{getSolarModeLabel(action.solarMode)}</span>
                                            )}
                                            {action.dryRun && (
                                                <span className="tag dry-run">Dry Run</span>
                                            )}
                                        </div>
                                        {action.currentPrice && (
                                            <div className="action-footer">
                                                <span className="price-label">Price:</span> ${action.currentPrice.dollarsPerKWH.toFixed(3)}/kWh
                                            </div>
                                        )}
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                </>
            )}
        </div>
    );
};

export default ActionList;
