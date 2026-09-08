import React, { useState, useEffect } from 'react';
import { Collapsible } from '@base-ui/react/collapsible';
import { type SavingsStats } from '../api';
import { formatCurrency } from '../utils/dashboardUtils';

interface SavingsHeroProps {
    savings: SavingsStats | null;
}

const SavingsHero: React.FC<SavingsHeroProps> = ({ savings }) => {
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);
    const [open, setOpen] = useState(false);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    if (!savings) return null;

    const netSavings = savings.batterySavings + savings.solarSavings + savings.credit;

    return (
        <div className="savings-hero">
            <Collapsible.Root open={isMobile ? open : true} onOpenChange={setOpen}>
                <div className="overview-hero">
                    <div className="net-savings-panel">
                        <span className="hero-label">Savings</span>
                        <div className="hero-value-group">
                            <span className={`hero-value ${netSavings >= 0 ? 'positive' : 'negative'}`}>
                                {formatCurrency(netSavings)}
                            </span>
                        </div>
                        <div className="hero-breakdown">
                            <div className="breakdown-item">
                                <span className="dot solar" aria-hidden="true"></span>
                                <span className="label">Solar</span>
                                <span className={`value ${savings.solarSavings >= 0 ? 'positive' : 'negative'}`}>
                                    {formatCurrency(savings.solarSavings, true)}
                                </span>
                            </div>
                            <div className="breakdown-item">
                                <span className="dot battery" aria-hidden="true"></span>
                                <span className="label">Battery</span>
                                <span className={`value ${savings.batterySavings >= 0 ? 'positive' : 'negative'}`}>
                                    {formatCurrency(savings.batterySavings, true)}
                                </span>
                            </div>
                            {Math.abs(savings.credit) > 0.01 && (
                                <div className="breakdown-item">
                                    <span className="dot credit" aria-hidden="true"></span>
                                    <span className="label">Export</span>
                                    <span className={`value ${savings.credit >= 0 ? 'positive' : 'negative'}`}>
                                        {formatCurrency(savings.credit, true)}
                                    </span>
                                </div>
                            )}
                        </div>
                    </div>

                    {isMobile && (
                        <Collapsible.Trigger className="stats-toggle-btn">
                            {open ? 'Hide Details' : 'Show Details'}
                            <span className={`arrow ${open ? 'up' : 'down'}`} />
                        </Collapsible.Trigger>
                    )}

                    <Collapsible.Panel className="usage-stats-panel-wrapper">
                        <div className="usage-stats-panel">
                            <div className="stats-row">
                                <div className="stat-card">
                                    <span className="stat-label">Home Usage</span>
                                    <span className="stat-value">{savings.homeUsed.toFixed(1)} <small>kWh</small></span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Solar Gen</span>
                                    <span className="stat-value">{savings.solarGenerated.toFixed(1)} <small>kWh</small></span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Battery Use</span>
                                    <span className="stat-value">{savings.batteryUsed.toFixed(1)} <small>kWh</small></span>
                                </div>
                            </div>
                            <div className="stats-row grid-metrics">
                                <div className="stat-card">
                                    <span className="stat-label">Grid (In/Out)</span>
                                    <span className="stat-value traffic-value">
                                        <span className="traffic-in">{savings.gridImported.toFixed(1)}</span>
                                        <span className="traffic-sep">/</span>
                                        <span className="traffic-out">{savings.gridExported.toFixed(1)}</span>
                                        <small>kWh</small>
                                    </span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Total Credit</span>
                                    <span className={`stat-value ${savings.credit > 0 ? 'positive' : savings.credit < 0 ? 'negative' : ''}`}>
                                        {formatCurrency(savings.credit)}
                                    </span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Total Cost</span>
                                    <span className="stat-value">{formatCurrency(savings.cost)}</span>
                                </div>
                            </div>
                        </div>
                    </Collapsible.Panel>
                </div>
            </Collapsible.Root>
        </div>
    );
};

export default SavingsHero;
