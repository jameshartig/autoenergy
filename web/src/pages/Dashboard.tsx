import React, { useEffect, useState, useMemo, useCallback, useRef } from 'react';
import { useLocation, useSearch, Link } from 'wouter';
import { type Action, type SavingsStats, type Settings, fetchActionsAndSavings, BatteryMode, ActionReason } from '../api';
import CurrentStatus from '../components/CurrentStatus';
import SavingsHero from '../components/SavingsHero';
import ActionTimeline from '../components/ActionTimeline';
import DateSelector from '../components/DateSelector';
import {
    gridChargeCost,
    getActionTimestamp,
    type ActionSummary,
    type ActionSummaryAccumulator,
    type SummaryType
} from '../utils/dashboardUtils';

export const whatsNewVersion = 10;
export const whatsNewText = "New: variable battery reserve and you no longer need to login so often.";
export const whatsNewLinkText = "";

const Dashboard: React.FC<{ siteID?: string, settings?: Settings | null }> = ({ siteID, settings = null }) => {
    const [location, navigate] = useLocation();
    const search = useSearch();
    const searchParams = useMemo(() => new URLSearchParams(search), [search]);

    const setSearchParams = useCallback((params: Record<string, string>) => {
        const p = new URLSearchParams(search);
        Object.entries(params).forEach(([k, v]) => p.set(k, v));
        navigate(location + "?" + p.toString());
    }, [search, location, navigate]);

    const dateQuery = searchParams.get('date');
    const [actions, setActions] = useState<Action[]>([]);
    const [savings, setSavings] = useState<SavingsStats | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    const [showWhatsNew, setShowWhatsNew] = useState(false);
    const [showGridWarning, setShowGridWarning] = useState(false);
    const [showLocationWarning, setShowLocationWarning] = useState(false);

    useEffect(() => {
        const storedVersion = localStorage.getItem('whats_new_banner_version');
        if (!storedVersion || parseInt(storedVersion, 10) < whatsNewVersion) {
            setShowWhatsNew(true);
        }
    }, []);

    const dismissWhatsNew = () => {
        localStorage.setItem('whats_new_banner_version', whatsNewVersion.toString());
        setShowWhatsNew(false);
    };

    useEffect(() => {
        if (settings) {
            const hasUtility = !!settings.utilityProvider && settings.utilityProvider !== "";

            const gridKey = siteID ? `grid_restrictions_warning_dismissed_${siteID}` : 'grid_restrictions_warning_dismissed';
            const isGridDismissed = localStorage.getItem(gridKey) === 'true';
            const allUnchecked = !settings.gridChargeBatteries && !settings.gridExportSolar && !settings.gridExportBatteries;
            setShowGridWarning(hasUtility && allUnchecked && !isGridDismissed);

            const locationKey = siteID ? `location_warning_dismissed_${siteID}` : 'location_warning_dismissed';
            const isLocationDismissed = localStorage.getItem(locationKey) === 'true';
            const missingLocation = !settings.countryCode || !settings.postalCode;
            setShowLocationWarning(hasUtility && missingLocation && !isLocationDismissed);
        } else {
            setShowGridWarning(false);
            setShowLocationWarning(false);
        }
    }, [settings, siteID]);

    const dismissGridWarning = () => {
        const key = siteID ? `grid_restrictions_warning_dismissed_${siteID}` : 'grid_restrictions_warning_dismissed';
        localStorage.setItem(key, 'true');
        setShowGridWarning(false);
    };

    const dismissLocationWarning = () => {
        const key = siteID ? `location_warning_dismissed_${siteID}` : 'location_warning_dismissed';
        localStorage.setItem(key, 'true');
        setShowLocationWarning(false);
    };

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

    const isFirstMount = useRef(true);

    useEffect(() => {
        let isCancelled = false;
        const delay = isFirstMount.current ? 0 : 200;
        isFirstMount.current = false;

        const timer = setTimeout(async () => {
            setLoading(true);
            setError(null);
            try {
                // Calculate start and end of the day in local time
                const start = new Date(currentDate);
                start.setHours(0, 0, 0, 0);
                const end = new Date(currentDate);
                end.setHours(23, 59, 59, 999);

                const actionsAndSavingsData = await fetchActionsAndSavings(start, end, siteID);

                if (!isCancelled) {
                    setActions(actionsAndSavingsData.actions || []);
                    setSavings(actionsAndSavingsData.savings);
                }
            } catch (err) {
                if (!isCancelled) {
                    console.error(err);
                    setError(err instanceof Error ? err.message : 'Failed to load data');
                }
            } finally {
                if (!isCancelled) {
                    setLoading(false);
                }
            }
        }, delay);

        return () => {
            isCancelled = true;
            clearTimeout(timer);
        };
    }, [currentDate, siteID]);

    const handleDateChange = useCallback((days: number) => {
        const newDate = new Date(currentDate);
        newDate.setDate(newDate.getDate() + days);
        const year = newDate.getFullYear();
        const month = String(newDate.getMonth() + 1).padStart(2, '0');
        const day = String(newDate.getDate()).padStart(2, '0');
        setSearchParams({ date: `${year}-${month}-${day}` });
    }, [currentDate, setSearchParams]);

    const isToday = currentDate.toDateString() === new Date().toDateString();


    const latestAction = actions.length > 0 ? actions[actions.length - 1] : null;
    // Filter out paused actions from the displayed timeline — they are captured for
    // status tracking only and should not appear as regular action items.
    const visibleActions = actions.filter(a => !a.paused);

    const groupedActions = useMemo(() => {
        const accumulator: (Action | ActionSummaryAccumulator)[] = [];
        let currentSummary: ActionSummaryAccumulator | null = null;

        for (const action of visibleActions) {
            const isFault = !!action.fault || action.reason === ActionReason.VPPActive || !!action.systemStatus?.vppActive;
            const hasPrice = !!action.currentPrice && action.currentPrice.tsStart !== "0001-01-01T00:00:00Z";
            const price = action.currentPrice ? gridChargeCost(action.currentPrice) : 0;

            const updateSummary = (summary: ActionSummaryAccumulator) => {
                summary.count++;
                if (hasPrice) {
                    summary.hasPrice = true;
                    summary.priceTotal += price;
                    summary.priceCount++;
                    if (summary.min === undefined || price < summary.min) summary.min = price;
                    if (summary.max === undefined || price > summary.max) summary.max = price;
                }
                if (action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0) {
                    summary.hasSOC = true;
                    const soc = action.systemStatus.batterySOC;
                    summary.socTotal += soc;
                    summary.socCount++;
                    if (summary.minSOC === undefined || soc < summary.minSOC) summary.minSOC = soc;
                    if (summary.maxSOC === undefined || soc > summary.maxSOC) summary.maxSOC = soc;
                }
            };

            const createSummary = (type: SummaryType): ActionSummaryAccumulator => {
                 const hasSOC = !!(action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0);
                 const soc = (action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0) ? action.systemStatus.batterySOC : 0;
                 return {
                    isSummary: true,
                    type: type,
                    reason: action.reason,
                    latestAction: action,
                    startTime: getActionTimestamp(action),
                    count: 1,
                    alarms: new Set<string>(),
                    storms: new Set<string>(),
                    hasPrice: hasPrice,
                    priceTotal: hasPrice ? price : 0,
                    priceCount: hasPrice ? 1 : 0,
                    min: hasPrice ? price : Infinity,
                    max: hasPrice ? price : -Infinity,
                    hasSOC: hasSOC,
                    socTotal: hasSOC ? soc : 0,
                    socCount: hasSOC ? 1 : 0,
                    minSOC: hasSOC ? soc : Infinity,
                    maxSOC: hasSOC ? soc : -Infinity
                };
            };

            if (isFault) {
                if (!action.reason) {
                    if (action.systemStatus && action.systemStatus.alarms) {
                        action.reason = "hasAlarms" as any;
                    }
                    if (action.systemStatus && action.systemStatus.storms) {
                        action.reason = "emergencyMode" as any;
                    }
                    if (action.systemStatus && action.systemStatus.gridUnavailable) {
                        action.reason = "gridUnavailable" as any;
                    }
                    if (action.systemStatus && action.systemStatus.vppActive) {
                        action.reason = ActionReason.VPPActive;
                    }
                }
                if (currentSummary && currentSummary.type === 'fault' && currentSummary.reason === action.reason) {
                    updateSummary(currentSummary);
                    currentSummary.latestAction = action;
                } else {
                    if (currentSummary) {
                        accumulator.push(currentSummary);
                    }
                    currentSummary = createSummary('fault');
                }

                if (action.systemStatus && action.systemStatus.alarms) {
                    action.systemStatus.alarms.forEach(alarm => {
                       if (currentSummary) {
                        currentSummary.alarms.add(alarm.name);
                       }
                    });
                }
                if (action.systemStatus && action.systemStatus.storms) {
                    action.systemStatus.storms.forEach(storm => {
                       if (currentSummary) {
                        currentSummary.storms.add(storm.description);
                        const start = new Date(storm.tsStart);
                        const end = new Date(storm.tsEnd);
                        if (!currentSummary.stormStart || start < currentSummary.stormStart) {
                            currentSummary.stormStart = start;
                        }
                        if (!currentSummary.stormEnd || end > currentSummary.stormEnd) {
                            currentSummary.stormEnd = end;
                        }
                       }
                    });
                }
            } else {
                // Group non-fault actions
                const effectiveBatteryMode = action.batteryMode === BatteryMode.NoChange && currentSummary
                    ? currentSummary.latestAction.batteryMode
                    : action.batteryMode;

                if (
                    currentSummary &&
                    currentSummary.type === 'grouped' &&
                    (
                        (currentSummary.reason === action.reason && currentSummary.latestAction.batteryMode === effectiveBatteryMode) ||
                        (action.batteryMode === BatteryMode.NoChange)
                    )
                ) {
                    updateSummary(currentSummary);
                    currentSummary.latestAction = {
                        ...action,
                        batteryMode: effectiveBatteryMode
                    };
                } else {
                    if (currentSummary) {
                        accumulator.push(currentSummary);
                    }
                    currentSummary = createSummary('grouped');
                }
            }
        }

        if (currentSummary) {
            accumulator.push(currentSummary);
        }

        return accumulator.map(item => {
            if ('isSummary' in item) {
                const summary = item as ActionSummaryAccumulator;
                if (summary.type === 'grouped' && summary.count === 1) {
                    return summary.latestAction;
                }

                const { priceTotal, priceCount, socTotal, socCount, ...rest } = summary;
                return {
                    ...rest,
                    endTime: getActionTimestamp(summary.latestAction),
                    avgPrice: priceCount > 0 ? priceTotal / priceCount : 0,
                    avgSOC: socCount > 0 ? socTotal / socCount : 0
                } as ActionSummary;
            }
            return item;
        }).reverse();
    }, [visibleActions]);

    return (
        <div className="content-container action-list-container">
            <header className="header">
                <DateSelector
                    currentDate={currentDate}
                    onDateChange={handleDateChange}
                    loading={loading}
                    isToday={isToday}
                />
            </header>

            {loading && (
                <div className="loading-screen">
                    <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
                    Loading day...
                </div>
            )}
            {error && <p className="error">{error}</p>}

            {!loading && !error && (
                <>
                    {showWhatsNew && (
                        <div className="banner info-banner" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                            <p>
                                <strong>What's New:</strong> {whatsNewText}{whatsNewLinkText && <>{' '}<Link href="/settings">{whatsNewLinkText}</Link></>}
                            </p>
                            <button onClick={dismissWhatsNew} className="banner-dismiss-btn" aria-label="Dismiss What's New banner">
                                <span aria-hidden="true">&times;</span>
                            </button>
                        </div>
                    )}
                    {settings && (!settings.utilityProvider || settings.utilityProvider === "") && (
                        <div className="banner warning-banner">
                            <p>
                                <strong>Setup Required:</strong> Utility Provider is not configured. <Link href="/settings">Configure it in Settings</Link> to enable automation.
                            </p>
                        </div>
                    )}
                    {settings && (!settings.ess || !settings.hasCredentials?.[settings.ess]) && (
                        <div className="banner warning-banner">
                            <p>
                                <strong>Setup Required:</strong> Energy Storage System is not connected. <Link href="/settings">Configure it in Settings</Link> to enable automation.
                            </p>
                        </div>
                    )}
                    {settings && settings.essAuthStatus && settings.essAuthStatus.consecutiveFailures >= 3 && (
                        <div className="banner warning-banner">
                            <p>
                                <span><strong>Warning:</strong> Energy Storage System authentication failed {settings.essAuthStatus.consecutiveFailures} time(s).{' '}
                                <Link href="/settings">Update your credentials in Settings</Link> to ensure automation continues.</span>
                            </p>
                        </div>
                    )}
                    {showGridWarning && (
                        <div className="banner warning-banner" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }} data-testid="grid-restrictions-warning-banner">
                            <p>
                                <span><strong>Warning:</strong> All grid features are disabled. The battery can only charge from solar. <Link href="/settings">Change this in Settings</Link></span>
                            </p>
                            <button onClick={dismissGridWarning} className="banner-dismiss-btn" aria-label="Dismiss grid restrictions warning">
                                <span aria-hidden="true">&times;</span>
                            </button>
                        </div>
                    )}
                    {showLocationWarning && (
                        <div className="banner warning-banner" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }} data-testid="location-warning-banner">
                            <p>
                                <span><strong>Warning:</strong> Location is not specified. <Link href="/settings">Configure it in Settings</Link> to improve solar forecasting accuracy.</span>
                            </p>
                            <button onClick={dismissLocationWarning} className="banner-dismiss-btn" aria-label="Dismiss location warning">
                                <span aria-hidden="true">&times;</span>
                            </button>
                        </div>
                    )}
                    {settings && settings.ess && settings.hasCredentials?.[settings.ess] && (settings.pause || settings.dryRun) && (
                        <div className="banner warning-banner" data-testid="automation-paused-dryrun-warning-banner">
                            <p>
                                <span><strong>Warning:</strong> {
                                    settings.pause && settings.dryRun
                                        ? "Automation is paused and Dry Run is enabled. The system will not write states to your hardware."
                                        : settings.pause
                                            ? "Automation is paused. The system will not perform any automated actions."
                                            : "Dry Run is enabled. The system will simulate actions but will not write them to your hardware."
                                } <Link href="/settings">Change this in Settings</Link></span>
                            </p>
                        </div>
                    )}
                    {siteID !== 'ALL' && isToday && latestAction && (
                        <CurrentStatus action={latestAction} />
                    )}

                    <SavingsHero savings={savings} />

                    {siteID !== 'ALL' && (
                        <>
                            {visibleActions.length === 0 && <p className="no-actions">No actions recorded for this day.</p>}
                            <ActionTimeline groupedActions={groupedActions} />
                        </>
                    )}
                </>
            )}
        </div>
    );
};

export default Dashboard;
