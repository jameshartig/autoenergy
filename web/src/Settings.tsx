import { useEffect, useState } from 'react';
import { fetchSettings, updateSettings, type Settings as SettingsType } from './api';
import './Settings.css';

const Settings = ({ isAdmin }: { isAdmin: boolean }) => {
    const [settings, setSettings] = useState<SettingsType | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [successMessage, setSuccessMessage] = useState<string | null>(null);

    useEffect(() => {
        loadSettings();
    }, []);

    const loadSettings = async () => {
        try {
            setLoading(true);
            const data = await fetchSettings();
            setSettings(data);
            setError(null);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load settings');
        } finally {
            setLoading(false);
        }
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!settings) return;

        try {
            setError(null);
            setSuccessMessage(null);
            await updateSettings(settings);
            setSuccessMessage('Settings saved successfully');
            setTimeout(() => setSuccessMessage(null), 3000);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to save settings');
        }
    };

    const handleChange = (field: keyof SettingsType, value: any) => {
        if (!settings) return;
        setSettings({ ...settings, [field]: value });
    };

    if (loading) return <div>Loading settings...</div>;
    if (!settings) return <div>Error loading settings</div>;

    return (
        <div className="settings-container">
            <h2>Settings</h2>
            {error && <div className="error-message">{error}</div>}
            {successMessage && <div className="success-message">{successMessage}</div>}

            <form onSubmit={handleSubmit}>
                <div className="form-group checkbox-group">
                    <label>
                        <input
                            type="checkbox"
                            checked={settings.dryRun}
                            onChange={(e) => handleChange('dryRun', e.target.checked)}
                        />
                        Dry Run
                    </label>
                    <span className="help-text">Simulate actions without executing them</span>
                </div>

                <div className="form-group checkbox-group">
                    <label>
                        <input
                            type="checkbox"
                            checked={settings.pause}
                            onChange={(e) => handleChange('pause', e.target.checked)}
                        />
                        Pause Updates
                    </label>
                    <span className="help-text">Stop automatic updates (prices and history will still sync)</span>
                </div>

                <h3>Power History Settings</h3>
                <div className="form-group">
                    <label htmlFor="ignoreHourUsageOverMultiple">Ignore Usage Outlier Multiple</label>
                    <input
                        id="ignoreHourUsageOverMultiple"
                        type="number"
                        step="0.1"
                        min="1"
                        value={settings.ignoreHourUsageOverMultiple}
                        onChange={(e) => handleChange('ignoreHourUsageOverMultiple', parseFloat(e.target.value))}
                    />
                    <span className="help-text">If a single hour's usage is this many times greater than the average of other data points for that hour, ignore it. Must be &ge; 1.</span>
                </div>

                <h3>Price Settings</h3>
                <div className="form-group">
                    <label htmlFor="alwaysChargeUnder">Always Charge Under ($/kWh)</label>
                    <input
                        id="alwaysChargeUnder"
                        type="number"
                        step="0.01"
                        value={settings.alwaysChargeUnderDollarsPerKWH}
                        onChange={(e) => handleChange('alwaysChargeUnderDollarsPerKWH', parseFloat(e.target.value))}
                    />
                    <span className="help-text">Always charge the battery if the price is below this threshold, regardless of forecast.</span>
                </div>
                <div className="form-group">
                    <label htmlFor="additionalFees">Additional Fees ($/kWh)</label>
                    <input
                        id="additionalFees"
                        type="number"
                        step="0.01"
                        value={settings.additionalFeesDollarsPerKWH}
                        onChange={(e) => handleChange('additionalFeesDollarsPerKWH', parseFloat(e.target.value))}
                    />
                    <span className="help-text">Fees added to the base price per kWh (e.g. delivery charges).</span>
                </div>
                <div className="form-group">
                    <label htmlFor="minArbitrage">Min Arbitrage Difference ($/kWh)</label>
                    <input
                        id="minArbitrage"
                        type="number"
                        step="0.01"
                        value={settings.minArbitrageDifferenceDollarsPerKWH}
                        onChange={(e) => handleChange('minArbitrageDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                    />
                    <span className="help-text">Minimum profit required to trigger charging for arbitrage.</span>
                </div>

                <h3>Battery Settings</h3>
                <div className="form-group">
                    <label htmlFor="minBatterySOC">Min Battery SOC (%)</label>
                    <input
                        id="minBatterySOC"
                        type="number"
                        step="1"
                        min="0"
                        max="100"
                        value={settings.minBatterySOC}
                        onChange={(e) => handleChange('minBatterySOC', parseFloat(e.target.value))}
                    />
                    <span className="help-text">Minimum State of Charge to maintain.</span>
                </div>

                <h3>Grid Settings</h3>
                <div className="form-group checkbox-group">
                    <label>
                        <input
                            type="checkbox"
                            checked={settings.gridChargeBatteries}
                            onChange={(e) => handleChange('gridChargeBatteries', e.target.checked)}
                        />
                        Grid Charge Batteries
                    </label>
                    <span className="help-text">Allow charging batteries from the grid.</span>
                </div>
                <div className="form-group checkbox-group">
                    <label>
                        <input
                            type="checkbox"
                            checked={settings.gridExportSolar}
                            onChange={(e) => handleChange('gridExportSolar', e.target.checked)}
                        />
                        Grid Export Solar
                    </label>
                    <span className="help-text">Allow exporting solar generation to the grid.</span>
                </div>

                <button type="submit" className="save-button" disabled={!isAdmin}>
                    {isAdmin ? 'Save Settings' : 'Read Only'}
                </button>
            </form>
        </div>
    );
};

export default Settings;
