import React, { useEffect, useState, useMemo, useRef } from 'react';
import { useLocation, useSearch } from 'wouter';
import { fetchHistoryEnergy, fetchSettings } from '../api';
import type { EnergyStats, Settings as SettingsType } from '../api';
import DateSelector from '../components/DateSelector';
import {
    ResponsiveContainer,
    AreaChart,
    Area,
    XAxis,
    YAxis,
    CartesianGrid,
    Tooltip,
    Line,
} from 'recharts';
import './History.css';

interface HistoryDataPoint extends EnergyStats {
    irradiance?: number;
    improvedSolarGeneration?: number;
    improvedHomeLoad?: number;
}

type ChartConfig = {
    title: string;
    dataKeys: { key: string; color: string; label: string; type: 'area' | 'line'; fill?: string; strokeDasharray?: string }[];
    unit: string;
};

const historyCharts: ChartConfig[] = [
    {
        title: 'Battery (%)',
        unit: '%',
        dataKeys: [
            { key: 'maxBatterySOC', color: 'var(--accent)', label: 'Max SOC', type: 'area', fill: 'var(--accent-variant)' }
        ]
    },
    {
        title: 'Solar Generation (kWh)',
        unit: ' kWh',
        dataKeys: [
            { key: 'solarKWH', color: 'var(--warning)', label: 'Actual Solar', type: 'area' },
            { key: 'improvedSolarGeneration', color: '#ff8a00', label: 'Forecast Solar', type: 'line', strokeDasharray: '4 4' }
        ]
    },
    {
        title: 'Estimated Irradiance',
        unit: ' W/m²',
        dataKeys: [
            { key: 'irradiance', color: '#60a5fa', label: 'Irradiance', type: 'area' }
        ]
    },
    {
        title: 'Home Load (kWh)',
        unit: ' kWh',
        dataKeys: [
            { key: 'homeKWH', color: '#a855f7', label: 'Home Load', type: 'area' }
        ]
    },
    {
        title: 'Grid (kWh)',
        unit: ' kWh',
        dataKeys: [
            { key: 'gridImportKWH', color: '#ef4444', label: 'Import', type: 'line' },
            { key: 'gridExportKWH', color: '#10b981', label: 'Export', type: 'line' }
        ]
    }
];

import { formatTime } from '../utils/dashboardUtils';

function formatHour(ts: string, referenceTs?: string): string {
    if (!ts) return '';
    const formatted = formatTime(ts, referenceTs);
    return formatted.replace(':00 ', ' ');
}

function HistoryChart({ data, config, isMobile }: { data: HistoryDataPoint[]; config: ChartConfig; isMobile: boolean }) {
    return (
        <div className="chart-card">
            <h3>{config.title}</h3>
            <ResponsiveContainer width="100%" height={240}>
                <AreaChart data={data} syncId="history" margin={{ top: 5, right: isMobile ? 0 : 20, left: 0, bottom: 5 }}>
                    <defs>
                        {config.dataKeys.map(dk => (
                             <linearGradient key={dk.key} id={`grad-${dk.key}`} x1="0" y1="0" x2="0" y2="1">
                                <stop offset="5%" stopColor={dk.color} stopOpacity={0.3} />
                                <stop offset="95%" stopColor={dk.color} stopOpacity={0.02} />
                            </linearGradient>
                        ))}
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--outline-variant)" vertical={false} opacity={0.4} />
                    <XAxis
                        dataKey="tsHourStart"
                        tickFormatter={(ts) => formatHour(ts, data[0]?.tsHourStart)}
                        tick={{ fontSize: isMobile ? 10 : 12 }}
                        stroke="var(--outline-variant)"
                        axisLine={false}
                        tickLine={false}
                    />
                    <YAxis
                        tick={{ fontSize: isMobile ? 10 : 12 }}
                        stroke="var(--outline-variant)"
                        width={isMobile ? 35 : 50}
                        axisLine={false}
                        tickLine={false}
                        tickFormatter={(v: number) => v.toFixed(1)}
                    />
                    <Tooltip
                        labelFormatter={(label) => formatHour(String(label), data[0]?.tsHourStart)}
                        contentStyle={{
                            backgroundColor: 'var(--surface-container-high)',
                            border: '1px solid var(--border)',
                            borderRadius: '8px',
                            boxShadow: 'var(--shadow-lg)',
                            color: 'var(--on-surface)',
                            backdropFilter: 'blur(10px)',
                        }}
                        itemStyle={{ color: 'var(--on-surface)' }}
                        labelStyle={{ color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 700 }}
                    />
                    {config.dataKeys.filter(dk => dk.type === 'area').map(dk => (
                        <Area
                            key={dk.key}
                            type="monotone"
                            dataKey={dk.key}
                            stroke={dk.color}
                            name={dk.label}
                            strokeWidth={3}
                            fill={`url(#grad-${dk.key})`}
                            isAnimationActive={true}
                        />
                    ))}
                    {config.dataKeys.filter(dk => dk.type === 'line').map(dk => (
                        <Line
                            key={dk.key}
                            type="monotone"
                            dataKey={dk.key}
                            name={dk.label}
                            stroke={dk.color}
                            strokeWidth={dk.strokeDasharray ? 2 : 3}
                            strokeDasharray={dk.strokeDasharray}
                            dot={false}
                        />
                    ))}
                </AreaChart>
            </ResponsiveContainer>
        </div>
    );
}

const History: React.FC<{ siteID?: string }> = ({ siteID }) => {
    const [location, setLocation] = useLocation();
    const searchString = useSearch();
    const searchParams = new URLSearchParams(searchString);
    const dateParam = searchParams.get('date');

    const currentDate = useMemo(() => {
        if (!dateParam) return new Date();
        const d = new Date(dateParam + 'T12:00:00'); // Use noon to avoid TZ issues
        return isNaN(d.getTime()) ? new Date() : d;
    }, [dateParam]);

    const [data, setData] = useState<HistoryDataPoint[]>([]);
    const [settings, setSettings] = useState<SettingsType | null>(null);
    const settingsRef = useRef<SettingsType | null>(null);
    const loadedSiteIDRef = useRef<string | undefined>(undefined);
    const isFirstMount = useRef(true);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    useEffect(() => {
        let isCancelled = false;
        const delay = isFirstMount.current ? 0 : 200;
        isFirstMount.current = false;

        const timer = setTimeout(async () => {
            setLoading(true);
            setError(null);
            try {
                const shouldFetchSettings = settingsRef.current === null || loadedSiteIDRef.current !== siteID;
                const fetchSettingsPromise = shouldFetchSettings
                    ? fetchSettings(siteID)
                    : Promise.resolve(settingsRef.current);

                const [res, s] = await Promise.all([
                    fetchHistoryEnergy(currentDate, siteID),
                    fetchSettingsPromise
                ]);
                if (isCancelled) return;
                setSettings(s);
                settingsRef.current = s;
                loadedSiteIDRef.current = siteID;

                // Merge energy and weather
                const merged: HistoryDataPoint[] = res.energy.map(e => {
                    const w = res.weather.find(weather =>
                        new Date(weather.tsHourStart).getTime() === new Date(e.tsHourStart).getTime()
                    );
                    return {
                        ...e,
                        irradiance: w?.irradiance,
                        improvedSolarGeneration: w?.improvedSolarGeneration,
                        improvedHomeLoad: w?.improvedHomeLoad !== undefined ? Math.floor(w.improvedHomeLoad * 10) / 10 : undefined,
                        homeKWH: Math.floor((e.homeKWH || 0) * 10) / 10,
                        gridImportKWH: Math.floor((e.gridImportKWH || 0) * 10) / 10,
                        gridExportKWH: Math.floor((e.gridExportKWH || 0) * 10) / 10,
                    };
                });

                setData(merged.sort((a, b) => new Date(a.tsHourStart).getTime() - new Date(b.tsHourStart).getTime()));
            } catch (err) {
                if (!isCancelled) {
                    setError(err instanceof Error ? err.message : 'Failed to load history');
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

    const handleDateChange = (days: number) => {
        const newDate = new Date(currentDate);
        newDate.setDate(newDate.getDate() + days);
        const year = newDate.getFullYear();
        const month = String(newDate.getMonth() + 1).padStart(2, '0');
        const day = String(newDate.getDate()).padStart(2, '0');
        setLocation(`${location}?date=${year}-${month}-${day}`);
    };

    const chartsToRender = useMemo(() => {
        const filtered = historyCharts.map(c => {
            if (c.title === 'Home Load (kWh)' && settings?.release === 'staging') {
                return {
                    ...c,
                    dataKeys: [
                        { key: 'homeKWH', color: '#a855f7', label: 'Home Load', type: 'area' as const },
                        { key: 'improvedHomeLoad', color: '#ec4899', label: 'Forecast Load', type: 'line' as const, strokeDasharray: '4 4' }
                    ]
                };
            }
            return c;
        }).filter(c => {
            if (c.dataKeys.some(dk => dk.key === 'irradiance') && settings?.release !== 'staging') return false;
            return true;
        });

        return filtered;
    }, [settings]);

    const isToday = currentDate.toDateString() === new Date().toDateString();

    return (
        <div className="content-container history-page">
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
                    Loading history...
                </div>
            )}
            {error && <div className="error">Error: {error}</div>}
            {!loading && !error && data.length === 0 && (
                <div className="no-data">No history found for this date.</div>
            )}

            {!loading && !error && data.length > 0 && (
                <div className="history-charts">
                    {chartsToRender.map(config => (
                        <HistoryChart key={config.title} data={data} config={config} isMobile={isMobile} />
                    ))}
                </div>
            )}
        </div>
    );
};

export default History;
