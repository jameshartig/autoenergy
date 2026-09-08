import { type Action, BatteryMode, SolarMode, ActionReason } from '../api';
import {
    getBatteryModeLabel,
    getBatteryModeClass,
    getSolarModeLabel,
    getSolarModeClass,
    formatPrice,
    formatTime,
    getActionTimestamp,
    isZeroTime,
    getReasonText,
    gridChargeCost,
    type ActionSummary
} from '../utils/dashboardUtils';
import './ActionTimeline.css';

interface ActionTimelineProps {
    groupedActions: (Action | ActionSummary)[];
}

const ActionTimeline: React.FC<ActionTimelineProps> = ({ groupedActions }) => {
    return (
        <ul className="timeline">
            {groupedActions.map((item, index) => {
                const isSummary = 'isSummary' in item;
                const action = isSummary ? (item as ActionSummary).latestAction : (item as Action);

                // For summaries, we might have multiple actions in one card
                const summary = isSummary ? (item as ActionSummary) : null;
                const isFault = !!action.fault || (summary?.type === 'fault');
                const hasStorms = action.systemStatus?.storms && action.systemStatus.storms.length > 0;
                const isEmergency = hasStorms || action.reason === ActionReason.EmergencyMode;

                const isVPP = !!action.systemStatus?.vppActive || action.reason === ActionReason.VPPActive;

                const reasonText = getReasonText(action);
                let batteryModeClass = getBatteryModeClass(action.batteryMode);
                if (isVPP) {
                    batteryModeClass = 'vpp';
                }
                const isNegPrice = action.currentPrice && (action.currentPrice.dollarsPerKWH + (action.currentPrice.gridUseDollarsPerKWH || 0)) < 0;

                // Determine Title
                let title = getBatteryModeLabel(action.batteryMode);
                if (isFault) title = 'System Fault';
                if (isEmergency) {
                    title = hasStorms ? 'Storm Hedge Mode' : 'Emergency Mode';
                }
                if (action.reason === ActionReason.BatteryAtReserve) {
                    title = 'Battery At Reserve';
                }
                if (action.reason === ActionReason.GridUnavailable || action.systemStatus?.gridUnavailable) {
                    title = 'Grid Unavailable';
                }
                if (action.reason === ActionReason.ArbitrageHoldExport || action.reason === ActionReason.ArbitrageHoldSave || action.reason === ActionReason.ArbitrageHold) {
                    title = 'Hold for Arbitrage';
                }
                if (action.reason === ActionReason.HoldSimilarPrice) {
                    title = 'Standby for Similar Price';
                }
                if (action.reason === ActionReason.VPPPrep) {
                    title = 'VPP Pre-Charging';
                }
                if (isVPP) {
                    title = 'VPP Event Active';
                }

                const showDeficitTag = !isZeroTime(action.deficitAt);
                const showCapacityTag = !isZeroTime(action.capacityAt);
                const refTs = (action.systemTimestamp && !isZeroTime(action.systemTimestamp)) ? action.systemTimestamp : action.systemStatus?.timestamp;

                return (
                    <li key={index} className={`timeline-item mode-${(isFault && !isVPP) ? 'fault' : batteryModeClass} ${summary ? 'is-grouped' : ''}`}>
                        <div className="timeline-marker" aria-hidden="true"></div>

                        <div className="timeline-time">
                            {isSummary && summary!.endTime && formatTime(summary!.endTime, refTs) !== formatTime(summary!.startTime, refTs) ? (
                                <div className="time-range">
                                    <span className="time-end">{formatTime(summary!.endTime, refTs)}</span>
                                    <span className="time-start">{formatTime(summary!.startTime, refTs)}</span>
                                </div>
                            ) : (
                                formatTime(isSummary ? summary!.startTime : getActionTimestamp(action), refTs)
                            )}
                        </div>

                        <div className="timeline-content">
                            <h3>
                                {title}
                            </h3>

                            <div className="reason">
                                {isEmergency ? (
                                    <>
                                        {action.reason === ActionReason.EmergencyMode && !hasStorms && <p>System manually put into emergency mode. Skipping automation.</p>}
                                        {hasStorms && <p>Charging the battery to prepare for the storm.</p>}
                                        {hasStorms && summary && Array.from(summary.storms).length > 0 && (
                                            <p className="storm-details">Storms: {Array.from(summary.storms).join(', ')}</p>
                                        )}
                                        {hasStorms && (
                                            <p className="storm-time">
                                                Storm Duration: {formatTime(isSummary && summary ? summary.stormStart?.toISOString() || '' : action.systemStatus?.storms?.[0]?.tsStart || '', refTs)} - {formatTime(isSummary && summary ? summary.stormEnd?.toISOString() || '' : action.systemStatus?.storms?.[0]?.tsEnd || '', refTs)}
                                            </p>
                                        )}
                                    </>
                                ) : isFault ? (
                                    <div className="fault-details">
                                        {action.reason === ActionReason.GridUnavailable || action.systemStatus?.gridUnavailable || isVPP ? (
                                            <p>{reasonText}</p>
                                        ) : (
                                            <p className="fault-alarms">
                                                Alarms: {summary ? Array.from(summary.alarms).join(', ') : action.systemStatus?.alarms?.map(a => a.name).join(', ')}
                                            </p>
                                        )}
                                    </div>
                                ) : (
                                    <p>{reasonText}</p>
                                )}
                            </div>

                            <div className="tags">
                                {(action.batteryMode !== BatteryMode.NoChange || (action.targetBatteryMode !== undefined && action.targetBatteryMode !== BatteryMode.NoChange)) && (
                                    <span className={`tag mode-${getBatteryModeClass(action.targetBatteryMode || action.batteryMode)}`}>
                                        {getBatteryModeLabel(action.targetBatteryMode || action.batteryMode)}
                                    </span>
                                )}
                                {(action.solarMode !== SolarMode.NoChange || (action.targetSolarMode !== undefined && action.targetSolarMode !== SolarMode.NoChange)) && (
                                    <span className={`tag solar-${getSolarModeClass(action.targetSolarMode || action.solarMode)}`}>
                                        {getSolarModeLabel(action.targetSolarMode || action.solarMode)}
                                    </span>
                                )}
                                {showDeficitTag && (
                                    <span className="tag tag-info">Empty: {formatTime(action.deficitAt!, refTs)}</span>
                                )}
                                {showCapacityTag && (
                                    <span className="tag tag-info">Full: {formatTime(action.capacityAt!, refTs)}</span>
                                )}
                                {isNegPrice && (
                                    <span className="tag tag-warning">Negative Price</span>
                                )}
                                {action.dryRun && (
                                    <span className="tag dry-run">Dry Run</span>
                                )}
                            </div>

                            <div className="timeline-footer">
                                <div className="timeline-metrics">
                                    {isSummary ? (
                                        <>
                                            {summary!.hasPrice && (
                                                <div className="timeline-metric">
                                                    <span className="label">Avg Price:</span>
                                                    <span className="value">
                                                        {formatPrice(summary!.avgPrice)}
                                                        {summary!.min !== summary!.max && (
                                                            <small className="range"> (Range: $ {summary!.min.toFixed(3)} - $ {summary!.max.toFixed(3)})</small>
                                                        )}
                                                    </span>
                                                </div>
                                            )}
                                            {summary!.hasSOC && (
                                                <div className="timeline-metric">
                                                    <span className="label">Battery:</span>
                                                    <span className="value">
                                                        {summary!.avgSOC.toFixed(1)}%
                                                        {summary!.minSOC !== summary!.maxSOC && (
                                                            <small className="range"> (Range: {summary!.minSOC.toFixed(0)}% - {summary!.maxSOC.toFixed(0)}%)</small>
                                                        )}
                                                    </span>
                                                </div>
                                            )}
                                        </>
                                    ) : (
                                        <>
                                            {action.currentPrice && (
                                                <div className="timeline-metric">
                                                    <span className="label">Price:</span>
                                                    <span className="value">{formatPrice(gridChargeCost(action.currentPrice))}</span>
                                                </div>
                                            )}
                                            {action.systemStatus?.batterySOC !== undefined && (
                                                <div className="timeline-metric">
                                                    <span className="label">Battery SOC:</span>
                                                    <span className="value">{action.systemStatus.batterySOC.toFixed(1)}%</span>
                                                </div>
                                            )}
                                        </>
                                    )}
                                </div>
                            </div>
                        </div>
                    </li>
                );
            })}
        </ul>
    );
};

export default ActionTimeline;
