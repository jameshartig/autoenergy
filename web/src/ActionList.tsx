
import React, { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { fetchActions, type Action, BatteryMode, SolarMode } from './api';

const getBatteryModeLabel = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'Standby';
        case BatteryMode.ChargeAny: return 'Charge Any';
        case BatteryMode.ChargeSolar: return 'Charge Solar';
        case BatteryMode.Load: return 'Load';
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
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    const currentDate = dateQuery ? new Date(dateQuery) : new Date();

    useEffect(() => {
        const loadActions = async () => {
            setLoading(true);
            setError(null);
            try {
                // Calculate start and end of the day in local time
                const start = new Date(currentDate);
                start.setHours(0, 0, 0, 0);
                const end = new Date(currentDate);
                end.setHours(23, 59, 59, 999);

                const data = await fetchActions(start, end);
                setActions(data);
            } catch (err) {
                setError('Failed to load actions');
            } finally {
                setLoading(false);
            }
        };

        loadActions();
    }, [dateQuery]);

    const handleDateChange = (days: number) => {
        const newDate = new Date(currentDate);
        newDate.setDate(newDate.getDate() + days);
        setSearchParams({ date: newDate.toISOString().split('T')[0] });
    };

    // Format date for display
    const formattedDate = currentDate.toLocaleDateString(undefined, {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric'
    });

    return (
        <div className="action-list-container">
            <header className="header">
                <button onClick={() => handleDateChange(-1)} disabled={loading}>&lt; Prev</button>
                <h2>{formattedDate}</h2>
                <button onClick={() => handleDateChange(1)} disabled={loading || currentDate.toDateString() === new Date().toDateString()}>Next &gt;</button>
            </header>

            {loading && <p>Loading actions...</p>}
            {error && <p className="error">{error}</p>}

            {!loading && !error && actions && actions.length === 0 && <p className="no-actions">No actions recorded for this day.</p>}

            <ul className="action-list">
                {actions && actions.filter(action => !(action.batteryMode === BatteryMode.NoChange && action.solarMode === SolarMode.NoChange)).map((action, index) => (
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
                ))}
            </ul>
        </div>
    );
};

export default ActionList;
