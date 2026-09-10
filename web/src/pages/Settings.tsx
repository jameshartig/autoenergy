import { useEffect, useState, useRef } from 'react';
import { useLocation } from 'wouter';
import { updateSettings, fetchUtilities, fetchESSList, submitESSStage, deleteSite, deleteUser, fetchUtilityPeriods, fetchEstimateEVCharging, type Settings as SettingsType, type UtilityProviderInfo, type UtilityRateOption, type ESSProviderInfo, type ESSCredentialField, type CredentialsPayload, type UserSite, type TimePeriod, type MinBatterySOCPeriod } from '../api';
import { Field } from '@base-ui/react/field';
import { Input } from '@base-ui/react/input';
import { Button } from '@base-ui/react/button';
import { Switch } from '@base-ui/react/switch';
import { Select } from '@base-ui/react/select';
import { Combobox } from '@base-ui/react/combobox';
import { Dialog } from '@base-ui/react/dialog';
import { InterestForm } from '../components/InterestForm';
import { HelpButton } from '../components/HelpButton';
import { isESSEnabled } from '../utils/enabledProviders';
import './Settings.css';

const countries = [
    { label: 'United States', value: 'US' },
    { label: 'United Kingdom', value: 'GB' },
    { label: 'Canada', value: 'CA' },
    { label: 'Australia', value: 'AU' },
    { label: 'Germany', value: 'DE' },
    { label: 'France', value: 'FR' },
    { label: 'Italy', value: 'IT' },
    { label: 'Spain', value: 'ES' },
    { label: 'Netherlands', value: 'NL' },
    { label: 'Belgium', value: 'BE' },
    { label: 'Switzerland', value: 'CH' },
    { label: 'Austria', value: 'AT' },
    { label: 'Sweden', value: 'SE' },
    { label: 'Norway', value: 'NO' },
    { label: 'Denmark', value: 'DK' },
    { label: 'Finland', value: 'FI' },
    { label: 'Ireland', value: 'IE' },
    { label: 'New Zealand', value: 'NZ' },
    { label: 'Japan', value: 'JP' },
    { label: 'South Korea', value: 'KR' },
    { label: 'Singapore', value: 'SG' },
    { label: 'Brazil', value: 'BR' },
    { label: 'Mexico', value: 'MX' },
    { label: 'India', value: 'IN' },
    { label: 'South Africa', value: 'ZA' }
];

interface LocationFormProps {
    settings: SettingsType;
    onChange: <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => void;
}

const LocationForm = ({ settings, onChange }: LocationFormProps) => {
    return (
        <>
            <Field.Root className="form-group">
                <Field.Label htmlFor="countryCode">Country</Field.Label>
                <Combobox.Root
                    value={settings.countryCode || ''}
                    onValueChange={(val) => onChange("countryCode", val as string)}
                    items={countries}
                    itemToStringLabel={(val) => countries.find(c => c.value === val)?.label || val}
                >
                    <div className="combobox-input-wrapper select-trigger">
                        <Combobox.Input placeholder="Select a country..." id="countryCode" className="combobox-input" />
                        <Combobox.Trigger className="combobox-trigger">
                            <svg width="15" height="15" viewBox="0 0 15 15" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                <path d="M4.18179 6.18181C4.35753 6.00608 4.64245 6.00608 4.81819 6.18181L7.49999 8.86362L10.1818 6.18181C10.3575 6.00608 10.6424 6.00608 10.8182 6.18181C10.9939 6.35755 10.9939 6.64247 10.8182 6.81821L7.81819 9.81821C7.73379 9.9026 7.61934 9.95001 7.49999 9.95001C7.38064 9.95001 7.26618 9.9026 7.18179 9.81821L4.18179 6.81821C4.00605 6.64247 4.00605 6.35755 4.18179 6.18181Z" fill="currentColor" fillRule="evenodd" clipRule="evenodd"></path>
                            </svg>
                        </Combobox.Trigger>
                    </div>
                    <Combobox.Portal>
                        <Combobox.Positioner className="select-positioner">
                            <Combobox.Popup className="select-popup">
                                <Combobox.List>
                                    {(item: { label: string, value: string }) => (
                                        <Combobox.Item key={item.value} value={item.value} className="select-item">
                                            {item.label}
                                        </Combobox.Item>
                                    )}
                                </Combobox.List>
                            </Combobox.Popup>
                        </Combobox.Positioner>
                    </Combobox.Portal>
                </Combobox.Root>
            </Field.Root>

            <Field.Root className="form-group">
                <Field.Label htmlFor="postalCode">Zip/Postal Code</Field.Label>
                <Input
                    id="postalCode"
                    type="text"
                    value={settings.postalCode || ''}
                    onChange={(e) => onChange("postalCode", e.target.value)}
                    placeholder="e.g. 90210"
                />
            </Field.Root>

            <Field.Root className="form-group" style={{ display: 'none' }}>
                <Field.Label htmlFor="solarDirection">Roof Solar Panel Direction</Field.Label>
                <Select.Root
                    value={(settings.solarTilt && settings.solarTilt > 0) ? (settings.solarAzimuth?.toString() || "") : ""}
                    onValueChange={(val) => {
                        const azimuth = parseInt(val as string, 10);
                        onChange("solarAzimuth", azimuth);
                        onChange("solarTilt", 25);
                    }}
                >
                    <Select.Trigger className="select-trigger" aria-label="Solar Direction">
                        <Select.Value placeholder="Select direction...">
                            {settings.solarTilt && settings.solarTilt > 0 ? (
                                ({
                                    "0": "North",
                                    "45": "Northeast",
                                    "90": "East",
                                    "135": "Southeast",
                                    "180": "South",
                                    "225": "Southwest",
                                    "270": "West",
                                    "315": "Northwest"
                                } as Record<string, string>)[settings.solarAzimuth?.toString() || ""]
                            ) : null}
                        </Select.Value>
                        <Select.Icon className="select-icon">
                            <svg width="15" height="15" viewBox="0 0 15 15" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                <path d="M4.18179 6.18181C4.35753 6.00608 4.64245 6.00608 4.81819 6.18181L7.49999 8.86362L10.1818 6.18181C10.3575 6.00608 10.6424 6.00608 10.8182 6.18181C10.9939 6.35755 10.9939 6.64247 10.8182 6.81821L7.81819 9.81821C7.73379 9.9026 7.61934 9.95001 7.49999 9.95001C7.38064 9.95001 7.26618 9.9026 7.18179 9.81821L4.18179 6.81821C4.00605 6.64247 4.00605 6.35755 4.18179 6.18181Z" fill="currentColor" fillRule="evenodd" clipRule="evenodd"></path>
                            </svg>
                        </Select.Icon>
                    </Select.Trigger>
                    <Select.Portal>
                        <Select.Positioner className="select-positioner">
                            <Select.Popup className="select-popup">
                                <Select.List>
                                    <Select.Item className="select-item" value="0"><Select.ItemText>North</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="45"><Select.ItemText>Northeast</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="90"><Select.ItemText>East</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="135"><Select.ItemText>Southeast</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="180"><Select.ItemText>South</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="225"><Select.ItemText>Southwest</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="270"><Select.ItemText>West</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="315"><Select.ItemText>Northwest</Select.ItemText></Select.Item>
                                </Select.List>
                            </Select.Popup>
                        </Select.Positioner>
                    </Select.Portal>
                </Select.Root>
            </Field.Root>
        </>
    );
};

interface UtilityFormProps {
    settings: SettingsType;
    onChange: <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => void;
    utilities: UtilityProviderInfo[];
    isWizard?: boolean;
    editUtility?: boolean;
    setEditUtility?: (val: boolean) => void;
    isUtilityDirty?: boolean;
    setIsUtilityDirty?: (val: boolean) => void;
    onUtilityChange?: (provider: string, rate: string, options: Record<string, any>) => void;
}

const UtilityForm = ({
    settings,
    onChange,
    utilities,
    isWizard = false,
    editUtility = false,
    setEditUtility,
    isUtilityDirty = false,
    setIsUtilityDirty,
    onUtilityChange
}: UtilityFormProps) => {
    const sortedUtilities = utilities
        .filter(u => !u.hidden || u.id === settings.utilityProvider)
        .map(u => ({ label: u.name, value: u.id }))
        .sort((a, b) => a.label.localeCompare(b.label));

    const renderFormFields = () => (
        <>
            <Field.Root className="form-group">
                <Field.Label htmlFor={isWizard ? "wizardUtilityService" : "utilityService"}>
                    {isWizard ? "Utility Provider" : "Service"}
                </Field.Label>
                <Combobox.Root
                    value={settings.utilityProvider || ''}
                    onValueChange={(value) => {
                        if (setEditUtility) setEditUtility(true);
                        if (setIsUtilityDirty) setIsUtilityDirty(true);
                        const providerID = value as string;
                        const provider = utilities.find(u => u.id === providerID);
                        const newSettings = {
                            ...settings,
                            utilityProvider: providerID,
                            utilityRate: "",
                            utilityRateOptions: {}
                        };

                        if (provider?.rates && provider.rates.length === 1) {
                            const rate = provider.rates[0];
                            newSettings.utilityRate = rate.id;
                            const newOpts: Record<string, string | number | boolean> = {};
                            (rate.options || []).forEach((opt: UtilityRateOption) => {
                                newOpts[opt.field] = opt.default;
                            });
                            newSettings.utilityRateOptions = newOpts;
                        }

                        onChange('utilityProvider', newSettings.utilityProvider);
                        onChange('utilityRate', newSettings.utilityRate);
                        onChange('utilityRateOptions', newSettings.utilityRateOptions);
                        if (onUtilityChange) {
                            onUtilityChange(newSettings.utilityProvider, newSettings.utilityRate, newSettings.utilityRateOptions);
                        }
                    }}
                    items={sortedUtilities}
                    itemToStringLabel={(val) => utilities.find(u => u.id === val)?.name || val}
                >
                    <div className="combobox-input-wrapper select-trigger">
                        <Combobox.Input
                            placeholder="Select a service..."
                            id={isWizard ? "wizardUtilityService" : "utilityService"}
                            className="combobox-input"
                        />
                        <Combobox.Trigger className="combobox-trigger" aria-label={isWizard ? "Utility Provider" : "Open popup"}>
                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                            </svg>
                        </Combobox.Trigger>
                    </div>
                    <Combobox.Portal>
                        <Combobox.Positioner className="select-positioner">
                            <Combobox.Popup className="select-popup">
                                <Combobox.Empty>
                                    <div className="select-item" style={{ pointerEvents: 'none' }}>No services found.</div>
                                </Combobox.Empty>
                                <Combobox.List>
                                    {(item: { label: string, value: string }) => (
                                        <Combobox.Item key={item.value} value={item.value} className="select-item">
                                            {item.label}
                                        </Combobox.Item>
                                    )}
                                </Combobox.List>
                            </Combobox.Popup>
                        </Combobox.Positioner>
                    </Combobox.Portal>
                </Combobox.Root>
            </Field.Root>

            {settings.utilityProvider && (
                <Field.Root className="form-group">
                    <Field.Label htmlFor={isWizard ? "wizardUtilityRate" : "utilityRate"}>{isWizard ? "Rate Plan" : "Rate/Plan"}</Field.Label>
                    <Select.Root
                        value={settings.utilityRate || ""}
                        onValueChange={(value) => {
                            if (setIsUtilityDirty) setIsUtilityDirty(true);
                            const rateID = value as string;
                            const provider = utilities.find(u => u.id === settings.utilityProvider);
                            const rate = (provider?.rates || []).find(r => r.id === rateID);
                            onChange('utilityRate', rateID);
                            if (rate) {
                                const newOpts: Record<string, string | number | boolean> = {};
                                (rate.options || []).forEach((opt: UtilityRateOption) => {
                                    newOpts[opt.field] = opt.default;
                                });
                                onChange('utilityRateOptions', newOpts);
                                if (onUtilityChange) onUtilityChange(settings.utilityProvider, rateID, newOpts);
                            } else {
                                onChange('utilityRateOptions', {});
                                if (onUtilityChange) onUtilityChange(settings.utilityProvider, rateID, {});
                            }
                        }}
                    >
                        <Select.Trigger className="select-trigger" id={isWizard ? "wizardUtilityRate" : "utilityRate"} aria-label={isWizard ? "Rate Plan" : "Rate/Plan"}>
                            <Select.Value placeholder="Select rate plan...">
                                {(utilities.find(u => u.id === settings.utilityProvider)?.rates || []).find(r => r.id === settings.utilityRate)?.name || 'Select rate plan...'}
                            </Select.Value>
                            <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                    <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                </svg>
                            </Select.Icon>
                        </Select.Trigger>
                        <Select.Portal>
                            <Select.Positioner className="select-positioner">
                                <Select.Popup className="select-popup">
                                    <Select.Item className="select-item" value="">
                                        <Select.ItemText>Select a rate/plan...</Select.ItemText>
                                    </Select.Item>
                                    {(utilities.find(u => u.id === settings.utilityProvider)?.rates || []).map(r => (
                                        <Select.Item key={r.id} className="select-item" value={r.id}>
                                            <Select.ItemText>{r.name}</Select.ItemText>
                                        </Select.Item>
                                    ))}
                                </Select.Popup>
                            </Select.Positioner>
                        </Select.Portal>
                    </Select.Root>
                </Field.Root>
            )}

            {settings.utilityProvider && settings.utilityRate && (() => {
                const provider = utilities.find(u => u.id === settings.utilityProvider);
                const rate = (provider?.rates || []).find(r => r.id === settings.utilityRate);
                if (!rate || !rate.options) return null;
                const visibleOptions = rate.options.filter((opt: UtilityRateOption) => !opt.hidden);
                if (visibleOptions.length === 0) return null;

                return (
                    <div className="sub-section">
                        {visibleOptions.map((opt: UtilityRateOption) => (
                            <Field.Root key={opt.field} className={`form-group ${opt.type === 'switch' ? 'switch-group' : ''}`}>
                                {opt.type === 'select' && (
                                    <>
                                        <Field.Label htmlFor={`opt-${opt.field}`}>{opt.name}</Field.Label>
                                        <Select.Root
                                            value={settings.utilityRateOptions?.[opt.field] || opt.default}
                                            onValueChange={(value) => {
                                                const newOpts = {
                                                    ...settings.utilityRateOptions,
                                                    [opt.field]: value
                                                };
                                                onChange('utilityRateOptions', newOpts);
                                                if (setIsUtilityDirty) setIsUtilityDirty(true);
                                            }}
                                        >
                                            <Select.Trigger className="select-trigger" id={`opt-${opt.field}`}>
                                                <Select.Value>
                                                    {opt.choices?.find(c => c.value === (settings.utilityRateOptions?.[opt.field] || opt.default))?.name || (settings.utilityRateOptions?.[opt.field] || opt.default)}
                                                </Select.Value>
                                                <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                    </svg>
                                                </Select.Icon>
                                            </Select.Trigger>
                                            <Select.Portal>
                                                <Select.Positioner className="select-positioner">
                                                    <Select.Popup className="select-popup">
                                                        {opt.choices?.map((choice) => (
                                                            <Select.Item key={choice.value} className="select-item" value={choice.value}>
                                                                <Select.ItemText>{choice.name}</Select.ItemText>
                                                            </Select.Item>
                                                        ))}
                                                    </Select.Popup>
                                                </Select.Positioner>
                                            </Select.Portal>
                                        </Select.Root>
                                    </>
                                )}
                                {opt.type === 'switch' && (
                                    <>
                                        <div className="switch-row">
                                            <Switch.Root
                                                id={`opt-${opt.field}`}
                                                checked={settings.utilityRateOptions?.[opt.field] ?? !!opt.default}
                                                onCheckedChange={(checked) => {
                                                    const newOpts = {
                                                        ...settings.utilityRateOptions,
                                                        [opt.field]: checked
                                                    };
                                                    onChange('utilityRateOptions', newOpts);
                                                    if (setIsUtilityDirty) setIsUtilityDirty(true);
                                                }}
                                                className="switch-root"
                                            >
                                                <Switch.Thumb className="switch-thumb" />
                                            </Switch.Root>
                                            <Field.Label htmlFor={`opt-${opt.field}`}>{opt.name}</Field.Label>
                                        </div>
                                    </>
                                )}
                                {opt.description && <Field.Description>{opt.description}</Field.Description>}
                            </Field.Root>
                        ))}
                    </div>
                );
            })()}

            {!isWizard && (
                <div className="utility-rate-unsupported-link">
                    <Dialog.Root>
                        <Dialog.Trigger className="inline-link" type="button">
                            Don't see your rate or options?
                        </Dialog.Trigger>
                        <Dialog.Portal>
                            <Dialog.Backdrop className="dialog-backdrop" />
                            <Dialog.Popup className="dialog-popup">
                                <Dialog.Title className="dialog-title">Request a Rate or Option</Dialog.Title>
                                <Dialog.Description className="dialog-description">
                                    Let us know which utility provider, state, or rate plan options you need.
                                </Dialog.Description>
                                <InterestForm
                                    utilitiesList={utilities}
                                    hideBattery={true}
                                    alwaysShowDetails={true}
                                />
                                <Dialog.Close className="btn btn-secondary dialog-close-btn" type="button">
                                    Close
                                </Dialog.Close>
                            </Dialog.Popup>
                        </Dialog.Portal>
                    </Dialog.Root>
                </div>
            )}

            {!isWizard && editUtility && setEditUtility && (
                <button type="button" className="text-button cancel-button" onClick={() => setEditUtility(false)} aria-label="Finish editing Utility Service">Done</button>
            )}
        </>
    );

    if (isWizard) {
        return renderFormFields();
    }

    return (
        <>
            {settings.utilityProvider && settings.utilityRate && !editUtility && setEditUtility ? (
                <button type="button" className="configured-summary" onClick={() => setEditUtility(true)} aria-label="Edit Utility Service">
                    <div className="summary-info">
                        <span className="summary-label">
                            {utilities.find(u => u.id === settings.utilityProvider)?.name || settings.utilityProvider}
                        </span>
                        <span className="summary-sublabel">
                            {utilities.find(u => u.id === settings.utilityProvider)?.rates?.find(r => r.id === settings.utilityRate)?.name || settings.utilityRate}
                        </span>
                    </div>
                    <div className={`summary-status ${isUtilityDirty ? 'pending' : ''}`}>
                        {isUtilityDirty ? 'Pending Save' : 'Configured'}
                    </div>
                </button>
            ) : (
                <div className={editUtility ? "edit-section" : ""}>
                    {renderFormFields()}
                </div>
            )}
        </>
    );
};

interface ESSFormProps {
    settings: SettingsType;
    onChange: <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => void;
    essProviders: ESSProviderInfo[];
    essCredentials: Record<string, string>;
    setEssCredentials: React.Dispatch<React.SetStateAction<Record<string, string>>>;
    isESSDirty: boolean;
    setIsESSDirty: (val: boolean) => void;
    isSaving: boolean;
    isStaging: boolean;
    oauthStatus: 'idle' | 'popup_open' | 'success';
    setOauthStatus: (status: 'idle' | 'popup_open' | 'success') => void;
    currentStage: number;
    setCurrentStage: (stage: number | ((prev: number) => number)) => void;
    handleOAuthLogin: (url: string, fieldName: string) => void;
    handleESSContinue: (e?: React.FormEvent | React.MouseEvent) => Promise<void>;
    isWizard?: boolean;
    editESS?: boolean;
    setEditESS?: (val: boolean) => void;
}

const getESSGridSummary = (gridCharge?: boolean, exportSolar?: boolean, exportBatteries?: boolean, hasCredentials?: boolean) => {
    if (!hasCredentials) {
        return 'Grid settings will be automatically detected on save';
    }
    const chargeText = gridCharge ? 'Grid Charging' : 'No Grid Charging';
    let exportText = 'No Export';
    if (exportSolar && exportBatteries) {
        exportText = 'Export: Solar & Battery';
    } else if (exportSolar) {
        exportText = 'Export: Solar Only';
    } else if (exportBatteries) {
        exportText = 'Export: Battery Only';
    }
    return `${chargeText} • ${exportText}`;
};

const ESSForm = ({
    settings,
    onChange,
    essProviders,
    essCredentials,
    setEssCredentials,
    isESSDirty,
    setIsESSDirty,
    isSaving,
    isStaging,
    oauthStatus,
    setOauthStatus,
    currentStage,
    setCurrentStage,
    handleOAuthLogin,
    handleESSContinue,
    isWizard = false,
    editESS = false,
    setEditESS
}: ESSFormProps) => {
    const provider = essProviders.find(p => p.id === settings.ess);
    const maxStage = provider
        ? (provider.credentials || []).reduce((max, cred) => {
              const stage = cred.stage ?? 0;
              return stage > max ? stage : max;
          }, 0)
        : 0;

    const renderFormFields = () => (
        <>
            <Field.Root className="form-group">
                <Field.Label htmlFor={isWizard ? "wizardESS" : "ess"}>System Type</Field.Label>
                <Select.Root
                    value={settings.ess || ""}
                    onValueChange={(value) => {
                        if (setEditESS) setEditESS(true);
                        setIsESSDirty(true);
                        onChange('ess', value as string);
                        onChange('customGridSettings', false);
                        setEssCredentials({});
                        setOauthStatus('idle');
                        setCurrentStage(0);
                    }}
                >
                    <Select.Trigger className="select-trigger" id={isWizard ? "wizardESS" : "ess"} aria-label="ESS Type">
                        <Select.Value placeholder="Select a system type...">
                            {essProviders.find(u => u.id === settings.ess)?.name || 'Select a system type...'}
                        </Select.Value>
                        <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                            </svg>
                        </Select.Icon>
                    </Select.Trigger>
                    <Select.Portal>
                        <Select.Positioner className="select-positioner">
                            <Select.Popup className="select-popup">
                                <Select.Item className="select-item" value="">
                                    <Select.ItemText>Select a system type...</Select.ItemText>
                                </Select.Item>
                                {essProviders.filter(p => !p.hidden || p.id === settings.ess || isESSEnabled(p.id)).map(p => (
                                    <Select.Item key={p.id} className="select-item" value={p.id}>
                                        <Select.ItemText>{p.name}</Select.ItemText>
                                    </Select.Item>
                                ))}
                            </Select.Popup>
                        </Select.Positioner>
                    </Select.Portal>
                </Select.Root>
            </Field.Root>

            {settings.ess && (
                <div style={{ padding: 0 }}>
                    {(() => {
                        const provider = essProviders.find(p => p.id === settings.ess);
                        if (!provider) return null;

                        return (
                            <>
                                {provider.oAuthURLs && Object.keys(provider.oAuthURLs).length > 0 && (
                                    <div style={{ marginBottom: '1rem' }}>
                                        {provider.oAuthKey && provider.oAuthKey.choices && (
                                            <Field.Root className="form-group" style={{ marginBottom: '1rem' }}>
                                                <Field.Label htmlFor={isWizard ? "wizard-oauth-key" : "oauth-key"}>{provider.oAuthKey.name}</Field.Label>
                                                <Select.Root
                                                    value={essCredentials[provider.oAuthKey.field] || provider.oAuthKey.default || ""}
                                                    onValueChange={(value) => {
                                                        setEssCredentials({ ...essCredentials, [provider.oAuthKey!.field]: value as string });
                                                        setIsESSDirty(true);
                                                    }}
                                                >
                                                    <Select.Trigger className="select-trigger" id={isWizard ? "wizard-oauth-key" : "oauth-key"} aria-label={provider.oAuthKey.name}>
                                                        <Select.Value>
                                                            {provider.oAuthKey.choices?.find(c => c.value === (essCredentials[provider.oAuthKey!.field] || provider.oAuthKey!.default))?.name || (essCredentials[provider.oAuthKey!.field] || provider.oAuthKey!.default)}
                                                        </Select.Value>
                                                        <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                            </svg>
                                                        </Select.Icon>
                                                    </Select.Trigger>
                                                    <Select.Portal>
                                                        <Select.Positioner className="select-positioner">
                                                            <Select.Popup className="select-popup">
                                                                {provider.oAuthKey.choices.map(c => (
                                                                    <Select.Item key={c.value} className="select-item" value={c.value}>
                                                                        <Select.ItemText>{c.name}</Select.ItemText>
                                                                    </Select.Item>
                                                                ))}
                                                            </Select.Popup>
                                                        </Select.Positioner>
                                                    </Select.Portal>
                                                </Select.Root>
                                                {provider.oAuthKey.description && <Field.Description>{provider.oAuthKey.description}</Field.Description>}
                                            </Field.Root>
                                        )}
                                        {essCredentials.authCode ? (
                                            <div className="success-message" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: 0 }}>
                                                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ marginRight: '0.25rem' }}>
                                                    <polyline points="20 6 9 17 4 12" />
                                                </svg>
                                                Received code! Save Settings below to complete.
                                            </div>
                                        ) : (
                                            <>
                                                <Button
                                                    className="save-button"
                                                    style={{ width: '100%' }}
                                                    disabled={oauthStatus === 'popup_open'}
                                                    onClick={() => {
                                                        const keyVal = provider.oAuthKey ? (essCredentials[provider.oAuthKey.field] || provider.oAuthKey.default || Object.keys(provider.oAuthURLs!)[0]) : Object.keys(provider.oAuthURLs!)[0];
                                                        const url = provider.oAuthURLs![keyVal];
                                                        if (url) {
                                                            handleOAuthLogin(url, "authCode");
                                                        }
                                                    }}
                                                    type="button"
                                                >
                                                    {oauthStatus === 'popup_open' && <span className="loading-spinner" aria-hidden="true"></span>}
                                                    {oauthStatus === 'popup_open' ? 'Awaiting link...' : 'Login to link account'}
                                                </Button>
                                                {oauthStatus === 'popup_open' && (
                                                    <div className="warning-notice" style={{ marginTop: '0.5rem' }}>
                                                        Please complete authentication in the popup window.
                                                    </div>
                                                )}
                                            </>
                                        )}
                                    </div>
                                )}
                                {(provider.credentials || []).filter(cred => ((cred.stage ?? 0) <= currentStage) && !cred.hidden).map(cred => (
                                    <Field.Root key={cred.field} className="form-group">
                                        <Field.Label htmlFor={`ess-${cred.field}`}>{cred.name}</Field.Label>
                                        {cred.type === 'select' ? (
                                            <Select.Root
                                                value={essCredentials[cred.field] || cred.default || ""}
                                                disabled={isSaving || isStaging}
                                                onValueChange={(value) => {
                                                    setEssCredentials({ ...essCredentials, [cred.field]: value as string });
                                                    setIsESSDirty(true);
                                                }}
                                            >
                                                <Select.Trigger className="select-trigger" id={`ess-${cred.field}`} aria-label={cred.name}>
                                                    <Select.Value placeholder={`Select ${cred.name}`}>
                                                        {cred.choices?.find(c => c.value === (essCredentials[cred.field] || cred.default))?.name || (essCredentials[cred.field] || cred.default)}
                                                    </Select.Value>
                                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                        </svg>
                                                    </Select.Icon>
                                                </Select.Trigger>
                                                <Select.Portal>
                                                    <Select.Positioner className="select-positioner">
                                                        <Select.Popup className="select-popup">
                                                            {cred.choices?.map((choice) => (
                                                                <Select.Item key={choice.value} className="select-item" value={choice.value}>
                                                                    <Select.ItemText>{choice.name}</Select.ItemText>
                                                                </Select.Item>
                                                            ))}
                                                        </Select.Popup>
                                                    </Select.Positioner>
                                                </Select.Portal>
                                            </Select.Root>
                                        ) : (
                                            <Input
                                                id={`ess-${cred.field}`}
                                                value={essCredentials[cred.field] || cred.default || ""}
                                                type={cred.type === 'password' ? 'password' : 'text'}
                                                disabled={isSaving || isStaging}
                                                onChange={(e) => {
                                                    setEssCredentials({ ...essCredentials, [cred.field]: e.target.value });
                                                    setIsESSDirty(true);
                                                }}
                                                placeholder={`Enter ${cred.name}`}
                                            />
                                        )}
                                        {cred.description && <Field.Description>{cred.description}</Field.Description>}
                                    </Field.Root>
                                ))}
                            </>
                        );
                    })()}
                </div>
            )}
        </>
    );

    if (isWizard) {
        return (
            <>
                {renderFormFields()}
                <Field.Root className="form-group" style={{ marginTop: '1.25rem' }}>
                    <Field.Label htmlFor="wizard-minBatterySOC">
                        Emergency Backup Reserve %
                        <HelpButton
                            title="Emergency Backup Reserve %"
                            description="Sets the minimum state-of-charge (SOC) level that RateRudder must preserve in your battery. RateRudder will avoid discharging the battery below this reserve to protect against power outages and grid disruptions."
                        />
                    </Field.Label>
                    <Input
                        id="wizard-minBatterySOC"
                        type="number"
                        step="1"
                        min="0"
                        max="100"
                        value={settings.minBatterySOC ?? 20}
                        onChange={(e) => onChange('minBatterySOC', e.target.value === '' ? ('' as any) : parseFloat(e.target.value))}
                    />
                    <Field.Description>Reserve kept in the battery for emergency backup during outages.</Field.Description>
                </Field.Root>
            </>
        );
    }

    return (
        <>
            {settings.ess && (settings.hasCredentials?.[settings.ess] || isESSDirty) && !editESS ? (
                <button type="button" className="configured-summary" onClick={() => setEditESS && setEditESS(true)} aria-label="Edit Energy Storage System">
                    <div className="summary-info">
                        <span className="summary-label">{essProviders.find(p => p.id === settings.ess)?.name || settings.ess || 'Unknown System'}</span>
                        <span className="summary-sublabel">
                            {getESSGridSummary(settings.gridChargeBatteries, settings.gridExportSolar, settings.gridExportBatteries, !!settings.hasCredentials?.[settings.ess])}
                        </span>
                    </div>
                    <div className={`summary-status ${isESSDirty ? 'pending' : ''}`}>
                        {isESSDirty ? 'Pending Save' : 'Connected'}
                    </div>
                </button>
            ) : (
                <div className={editESS ? "edit-section" : ""}>
                    {renderFormFields()}

                    {!isWizard && settings.ess && settings.hasCredentials?.[settings.ess] && (
                        <div className="grid-strategy-grid" style={{ marginTop: '1rem', borderTop: '1px solid var(--outline-variant)', paddingTop: '1rem' }}>
                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="gridChargeBatteries"
                                        checked={settings.gridChargeBatteries}
                                        onCheckedChange={(checked) => {
                                            onChange('gridChargeBatteries', checked);
                                            onChange('customGridSettings', true);
                                        }}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="gridChargeBatteries">Grid Can Charge Battery</Field.Label>
                                </div>
                            </Field.Root>

                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="gridExportSolar"
                                        checked={settings.gridExportSolar}
                                        onCheckedChange={(checked) => {
                                            onChange('gridExportSolar', checked);
                                            onChange('customGridSettings', true);
                                        }}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="gridExportSolar">Export Solar to Grid</Field.Label>
                                </div>
                            </Field.Root>

                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="gridExportBatteries"
                                        checked={settings.gridExportBatteries}
                                        onCheckedChange={(checked) => {
                                            onChange('gridExportBatteries', checked);
                                            onChange('customGridSettings', true);
                                        }}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="gridExportBatteries">Export Battery to Grid</Field.Label>
                                </div>
                            </Field.Root>

                            {!settings.gridChargeBatteries && !settings.gridExportSolar && !settings.gridExportBatteries && (
                                <div className="warning-notice" style={{ gridColumn: '1 / -1', marginTop: 0 }} data-testid="grid-restrictions-warning">
                                    Warning: All grid interactions are disabled. The system will only charge from solar and will not charge from the grid or export any energy.
                                </div>
                            )}
                        </div>
                    )}

                    {editESS && (
                        <div className="ess-actions" style={{ display: 'flex', gap: '1rem', marginTop: '1.5rem', alignItems: 'center' }}>
                            {maxStage > currentStage && (
                                <button
                                    type="button"
                                    className="save-button"
                                    style={{ width: 'auto', padding: '0.5rem 1.5rem', marginTop: 0 }}
                                    onClick={handleESSContinue}
                                    disabled={isStaging}
                                >
                                    {isStaging && <span className="loading-spinner" aria-hidden="true"></span>}
                                    {isStaging ? 'Submitting...' : 'Continue'}
                                </button>
                            )}
                            <button
                                type="button"
                                className="text-button cancel-button"
                                style={{ marginTop: 0 }}
                                onClick={() => {
                                    if (setEditESS) setEditESS(false);
                                    setCurrentStage(0);
                                }}
                                aria-label="Finish editing Energy Storage System"
                            >
                                {maxStage > currentStage ? 'Cancel' : 'Done'}
                            </button>
                        </div>
                    )}
                </div>
            )}
        </>
    );
};

const Settings = ({
    siteID,
    settings: parentSettings,
    onSettingsSaved,
    onShowTutorial,
    sites = []
}: {
    siteID?: string,
    settings: SettingsType | null,
    onSettingsSaved?: () => Promise<void>,
    onShowTutorial?: () => void,
    sites?: UserSite[]
}) => {
    const [settings, setSettings] = useState<SettingsType | null>(null);
    const [isUtilityDirty, setIsUtilityDirty] = useState(false);
    const [isESSDirty, setIsESSDirty] = useState(false);
    const [utilities, setUtilities] = useState<UtilityProviderInfo[]>([]);
    const [essProviders, setEssProviders] = useState<ESSProviderInfo[]>([]);
    const [loadingLists, setLoadingLists] = useState(true);
    const [isSaving, setIsSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [successMessage, setSuccessMessage] = useState<string | null>(null);
    const [currentStage, setCurrentStage] = useState(0);
    const [isStaging, setIsStaging] = useState(false);

    const loading = loadingLists || !settings;

    const [, navigate] = useLocation();
    const [forceFullSettings, setForceFullSettings] = useState(false);
    const [wizardStep, setWizardStep] = useState(1);
    const [isInWizard, setIsInWizard] = useState<boolean | null>(null);
    const [showTeslaGridModal, setShowTeslaGridModal] = useState(false);
    const [teslaGridDirty, setTeslaGridDirty] = useState(false);

    useEffect(() => {
        if (settings && isInWizard === null) {
            const isUtility = !!settings.utilityProvider && settings.utilityProvider !== "";
            const isESS = !!settings.ess && settings.ess !== "" && !!settings.hasCredentials?.[settings.ess];
            setIsInWizard(!isUtility && !isESS);
        }
    }, [settings, isInWizard]);

    const [utilityPeriods, setUtilityPeriods] = useState<TimePeriod[] | null>(null);
    const [loadingPeriods, setLoadingPeriods] = useState(false);
    const [scheduleMode, setScheduleMode] = useState<'named' | 'custom'>('named');
    const [editBattery, setEditBattery] = useState(false);
    const [batteryError, setBatteryError] = useState<string | null>(null);

    const isEVFeatureEnabled = settings?.release === 'staging' || (typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('ev') === 'true') || (!!settings?.evChargingPeriods && settings.evChargingPeriods.length > 0);
    const [estimatingEV, setEstimatingEV] = useState(false);
    const [evEstimationNote, setEVEstimationNote] = useState<string | null>(null);
    const [evEstimationError, setEVEstimationError] = useState<string | null>(null);
    const hasNamedRatePeriods = utilityPeriods !== null ? utilityPeriods.some(p => p.name && p.name !== '') : true;

    const validateUtilityAndPeriods = async (
        newProvider: string,
        newRate: string,
        newOpts: Record<string, any>
    ) => {
        if (!newProvider || !newRate) {
            setBatteryError(null);
            return;
        }
        try {
            const fetched = await fetchUtilityPeriods(siteID, newProvider, newRate, newOpts);
            setUtilityPeriods(fetched || []);

            setSettings(prevSettings => {
                if (!prevSettings) return prevSettings;
                const hasExistingPeriods = prevSettings.minBatterySOCPeriods && prevSettings.minBatterySOCPeriods.length > 0;
                if (!hasExistingPeriods) {
                    setBatteryError(null);
                    return prevSettings;
                }

                const isRateBased = scheduleMode === 'named' || prevSettings.minBatterySOCPeriods?.some((p: MinBatterySOCPeriod) => !!p.utilityPeriodName);
                if (isRateBased) {
                    const fetchedNames = fetched ? fetched.filter((p: TimePeriod) => p.name && p.name !== '').map((p: TimePeriod) => p.name!) : [];
                    const uniqueFetchedNames = Array.from(new Set(fetchedNames));

                    if (uniqueFetchedNames.length === 0) {
                        setBatteryError(null);
                        setScheduleMode('custom');
                        const custom: MinBatterySOCPeriod[] = [
                            { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: prevSettings.minBatterySOC ?? 20 }
                        ];
                        return {
                            ...prevSettings,
                            minBatterySOCPeriods: custom,
                            minBatterySOC: prevSettings.minBatterySOC ?? 20,
                        };
                    } else {
                        const currentPeriods = prevSettings.minBatterySOCPeriods || [];
                        const reconciledPeriods: MinBatterySOCPeriod[] = uniqueFetchedNames.map(name => {
                            const exact = currentPeriods.find(p => p.utilityPeriodName === name);
                            if (exact) {
                                return { utilityPeriodName: name, minBatterySOC: exact.minBatterySOC };
                            }
                            const fuzzy = currentPeriods.find(p =>
                                p.utilityPeriodName && (
                                    p.utilityPeriodName.toLowerCase().includes(name.toLowerCase()) ||
                                    name.toLowerCase().includes(p.utilityPeriodName.toLowerCase())
                                )
                            );
                            return {
                                utilityPeriodName: name,
                                minBatterySOC: fuzzy ? fuzzy.minBatterySOC : (prevSettings.minBatterySOC ?? 20),
                            };
                        });

                        setBatteryError(null);
                        const minVal = Math.min(...reconciledPeriods.map(p => p.minBatterySOC ?? 0));
                        return {
                            ...prevSettings,
                            minBatterySOCPeriods: reconciledPeriods,
                            minBatterySOC: isFinite(minVal) ? minVal : (prevSettings.minBatterySOC ?? 20),
                        };
                    }
                } else {
                    setBatteryError(null);
                    return prevSettings;
                }
            });
        } catch (err) {
            console.error("Failed to re-validate utility periods", err);
        }
    };

    useEffect(() => {
        if (settings?.minBatterySOCPeriods && settings.minBatterySOCPeriods.length > 0) {
            const hasNamed = settings.minBatterySOCPeriods.some((p: MinBatterySOCPeriod) => p.utilityPeriodName);
            setScheduleMode(hasNamed ? 'named' : 'custom');
        }
    }, [settings?.minBatterySOCPeriods]);

    const rateOptionsKey = JSON.stringify(settings?.utilityRateOptions);
    useEffect(() => {
        if (siteID && settings?.utilityProvider && settings?.utilityRate) {
            fetchUtilityPeriods(siteID, settings.utilityProvider, settings.utilityRate, settings.utilityRateOptions)
                .then(periods => setUtilityPeriods(periods || []))
                .catch(() => setUtilityPeriods([]));
        }
    }, [siteID, settings?.utilityProvider, settings?.utilityRate, settings?.utilityRateOptions, rateOptionsKey]);

    const handleOpenReserveSchedule = async () => {
        setLoadingPeriods(true);
        try {
            const fetched = await fetchUtilityPeriods(siteID);
            setUtilityPeriods(fetched || []);
            const fetchedNames = fetched ? fetched.filter((p: TimePeriod) => p.name && p.name !== '').map((p: TimePeriod) => p.name!) : [];
            if (fetchedNames.length === 0) {
                setBatteryError(null);
                setScheduleMode('custom');
                const custom: MinBatterySOCPeriod[] = [
                    { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: settings?.minBatterySOC ?? 20 }
                ];
                updateMinBatterySOCPeriods(custom);
                return;
            }
            const uniqueNames = Array.from(new Set(fetchedNames));
            const initialPeriods: MinBatterySOCPeriod[] = uniqueNames.map(name => ({
                utilityPeriodName: name,
                minBatterySOC: settings?.minBatterySOC ?? 20,
            }));
            updateMinBatterySOCPeriods(initialPeriods);
            setScheduleMode('named');
            setBatteryError(null);
        } catch {
            setScheduleMode('custom');
            const custom: MinBatterySOCPeriod[] = [
                { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: settings?.minBatterySOC ?? 20 }
            ];
            updateMinBatterySOCPeriods(custom);
        } finally {
            setLoadingPeriods(false);
        }
    };

    const updateMinBatterySOCPeriods = (newPeriods: MinBatterySOCPeriod[] | undefined) => {
        if (!newPeriods || newPeriods.length === 0) {
            handleChange('minBatterySOCPeriods', undefined);
            return;
        }
        const validSOCs = newPeriods
            .map(p => typeof p.minBatterySOC === 'number' ? p.minBatterySOC : parseFloat(p.minBatterySOC as any))
            .filter(v => !isNaN(v));
        const minVal = validSOCs.length > 0 ? Math.min(...validSOCs) : (settings?.minBatterySOC ?? 20);
        const updated = {
            ...settings!,
            minBatterySOCPeriods: newPeriods,
            minBatterySOC: isFinite(minVal) ? minVal : (settings?.minBatterySOC ?? 20),
        };
        setSettings(updated);
    };

    const validate24HourCoverage = (periods: MinBatterySOCPeriod[]) => {
        const hasCustomHours = periods.some(p => !p.utilityPeriodName && p.hours && p.hours.length > 0);
        if (!hasCustomHours) {
            return null;
        }
        const counts = new Array(24).fill(0);
        for (const p of periods) {
            if (p.utilityPeriodName) continue;
            for (const hp of p.hours || []) {
                const start = hp.hourStart;
                const end = hp.hourEnd;
                if (start === end) continue;
                for (let h = 0; h < 24; h++) {
                    if (start < end) {
                        if (h >= start && h < end) counts[h]++;
                    } else if (start > end) {
                        if (h >= start || h < end) counts[h]++;
                    }
                }
            }
        }
        const uncovered = counts.some(c => c === 0);
        const overlapping = counts.some(c => c > 1);
        if (uncovered || overlapping) {
            return "⚠️ Reserve SOC periods must cover all 24 hours without gaps or overlaps.";
        }
        return null;
    };

    const handleTogglePause = async () => {
        if (!settings) return;
        const newPause = !settings.pause;
        const nextSettings = { ...settings, pause: newPause };
        setSettings(nextSettings);
        try {
            setIsSaving(true);
            setError(null);
            await updateSettings(nextSettings, siteID);
            setSuccessMessage(newPause ? 'Automation paused' : 'Automation resumed');
            if (onSettingsSaved) {
                await onSettingsSaved();
            }
            setTimeout(() => setSuccessMessage(null), 3000);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to update pause state');
            setSettings(prev => prev ? ({ ...prev, pause: !newPause }) : null);
        } finally {
            setIsSaving(false);
        }
    };

    const [essCredentials, setEssCredentials] = useState<Record<string, string>>({});
    const [oauthStatus, setOauthStatus] = useState<'idle' | 'popup_open' | 'success'>('idle');
    const savingRef = useRef(false);
    const originalSettingsRef = useRef<string | null>(null);
    const oauthTimerRef = useRef<any>(null);
    const oauthListenerRef = useRef<((event: MessageEvent) => void) | null>(null);

    useEffect(() => {
        return () => {
            if (oauthTimerRef.current) {
                clearInterval(oauthTimerRef.current);
            }
            if (oauthListenerRef.current) {
                window.removeEventListener('message', oauthListenerRef.current);
            }
        };
    }, []);

    // UI State for consolidated views
    const [editUtility, setEditUtility] = useState(false);
    const [editESS, setEditESS] = useState(false);
    const [showAdvanced, setShowAdvanced] = useState(false);

    const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
    const [deleteAccountChecked, setDeleteAccountChecked] = useState(false);
    const [isDeleting, setIsDeleting] = useState(false);
    const [deleteError, setDeleteError] = useState<string | null>(null);

    const isLastSite = sites.length <= 1;

    const handleConfirmDelete = async () => {
        if (!siteID) return;
        setIsDeleting(true);
        setDeleteError(null);
        try {
            await deleteSite(siteID);

            if (deleteAccountChecked && isLastSite) {
                await deleteUser();
                window.location.href = '/';
                return;
            }

            const remainingSites = sites.filter(s => s.id !== siteID);
            if (remainingSites.length === 0) {
                window.location.href = '/welcome';
            } else {
                window.location.href = '/dashboard';
            }
        } catch (err: any) {
            console.error("Deletion failed", err);
            setDeleteError(err.message || "An error occurred during deletion");
            setIsDeleting(false);
        }
    };

    useEffect(() => {
        if (!siteID) return;
        let active = true;

        const loadLists = async () => {
            try {
                setLoadingLists(true);
                setCurrentStage(0);
                const [utilitiesData, essProvidersData] = await Promise.all([
                    fetchUtilities(siteID),
                    fetchESSList(siteID)
                ]);
                if (active) {
                    setUtilities(utilitiesData);
                    setEssProviders(essProvidersData);
                    setError(null);
                }
            } catch (err) {
                if (active) {
                    setError(err instanceof Error ? err.message : 'Failed to load options');
                }
            } finally {
                if (active) {
                    setLoadingLists(false);
                }
            }
        };

        loadLists();

        return () => {
            active = false;
        };
    }, [siteID]);

    useEffect(() => {
        if (parentSettings) {
            const initial = {
                ...parentSettings,
                gridChargeBatteries: parentSettings.gridChargeBatteries ?? true,
                gridExportSolar: parentSettings.gridExportSolar ?? false,
                gridExportBatteries: parentSettings.gridExportBatteries ?? false
            };
            setSettings(initial);
            if (!originalSettingsRef.current) {
                originalSettingsRef.current = JSON.stringify(initial);
            }
            if (parentSettings.ess && !parentSettings.hasCredentials?.[parentSettings.ess]) {
                setEditESS(true);
                setIsESSDirty(true);
            }
        }
    }, [parentSettings]);

    const isDirty = Boolean(
        settings &&
        originalSettingsRef.current &&
        JSON.stringify(settings) !== originalSettingsRef.current
    );

    useEffect(() => {
        const handleBeforeUnload = (e: BeforeUnloadEvent) => {
            if (isDirty && !savingRef.current) {
                e.preventDefault();
                e.returnValue = '';
            }
        };
        window.addEventListener('beforeunload', handleBeforeUnload);
        return () => window.removeEventListener('beforeunload', handleBeforeUnload);
    }, [isDirty]);

    const handleDiscard = () => {
        if (originalSettingsRef.current) {
            setSettings(JSON.parse(originalSettingsRef.current));
            setIsUtilityDirty(false);
            setIsESSDirty(false);
            setError(null);
        }
    };

    const handleESSContinue = async (e?: React.FormEvent | React.MouseEvent) => {
        if (e) {
            e.preventDefault();
            e.stopPropagation();
        }
        if (!settings || !settings.ess) return;
        setIsStaging(true);
        setError(null);
        try {
            const provider = essProviders.find(p => p.id === settings.ess);
            if (!provider) throw new Error('Selected ESS provider not found');

            const payload: Record<string, string> = {};
            const stageFields = (provider.credentials || []).filter(cred => (cred.stage ?? 0) <= currentStage);
            for (const cred of stageFields) {
                payload[cred.field] = essCredentials[cred.field] || cred.default || "";
            }

            await submitESSStage(settings.ess, payload, siteID);
            setCurrentStage(prev => prev + 1);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to advance stage');
        } finally {
            setIsStaging(false);
        }
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!settings || savingRef.current) return;

        if (editESS && maxStage > currentStage) {
            handleESSContinue(e);
            return;
        }

        const isESSAlreadyConfigured = settings ? (!!settings.ess && settings.ess !== "" && !!settings.hasCredentials?.[settings.ess]) : false;

        if (settings.minBatterySOCPeriods && settings.minBatterySOCPeriods.length > 0) {
            const coverageError = validate24HourCoverage(settings.minBatterySOCPeriods);
            if (coverageError) {
                setError(coverageError);
                return;
            }
        }

        try {
            savingRef.current = true;
            setIsSaving(true);
            setError(null);
            setSuccessMessage(null);

            // Merge default values for utility rate options
            const utilityProvider = utilities.find(u => u.id === settings.utilityProvider);
            const utilityRate = (utilityProvider?.rates || []).find(r => r.id === settings.utilityRate);
            const finalSettings = { ...settings };
            if (utilityRate) {
                const finalOpts = { ...finalSettings.utilityRateOptions };
                (utilityRate.options || []).forEach(opt => {
                    if (finalOpts[opt.field] === undefined || finalOpts[opt.field] === null || finalOpts[opt.field] === "") {
                        if (opt.default !== undefined) {
                            finalOpts[opt.field] = opt.default;
                        }
                    }
                });
                finalSettings.utilityRateOptions = finalOpts;
            }

            let credentialsPayload: CredentialsPayload | undefined = undefined;
            if (finalSettings.minBatterySOCPeriods && finalSettings.minBatterySOCPeriods.length > 0) {
                finalSettings.minBatterySOCPeriods = finalSettings.minBatterySOCPeriods.map(p => {
                    const parsed = typeof p.minBatterySOC === 'number' ? p.minBatterySOC : parseFloat(p.minBatterySOC as any);
                    return {
                        ...p,
                        minBatterySOC: !isNaN(parsed) ? parsed : (typeof finalSettings.minBatterySOC === 'number' && !isNaN(finalSettings.minBatterySOC) ? finalSettings.minBatterySOC : 20),
                    };
                });
            }
            if (finalSettings.minBatterySOC !== undefined && finalSettings.minBatterySOC !== null) {
                const parsed = typeof finalSettings.minBatterySOC === 'number' ? finalSettings.minBatterySOC : parseFloat(finalSettings.minBatterySOC as any);
                finalSettings.minBatterySOC = !isNaN(parsed) ? parsed : 20;
            }
            const essProvider = essProviders.find(p => p.id === settings.ess);
            if (essProvider && (isESSDirty || Object.keys(essCredentials).length > 0)) {
                credentialsPayload = { [essProvider.id]: {} };
                const processCred = (cred: ESSCredentialField) => {
                    let val = essCredentials[cred.field];
                    if (val === undefined || val === null || val === "") {
                        val = cred.default;
                    }

                    if (cred.required && (val === undefined || val === null || val === "")) {
                        // TODO: we should probably have the backend define better errors
                        if (cred.field === 'authCode') {
                            setEditESS(true);
                            throw new Error('Login to connect your energy system.');
                        }
                        throw new Error(`The ${cred.name} field is required.`);
                    }
                    if (val !== undefined && val !== null && val !== "") {
                        credentialsPayload![essProvider.id][cred.field] = val;
                    }
                };

                for (const cred of (essProvider.credentials || [])) {
                    processCred(cred);
                }
                if (essProvider.oAuthKey) {
                    processCred(essProvider.oAuthKey);
                }
            }

            await updateSettings(finalSettings, siteID, credentialsPayload);
            setSuccessMessage('Settings saved successfully');

            if (onSettingsSaved) {
                await onSettingsSaved();
            }

            const updatedSettings = credentialsPayload && finalSettings.ess ? {
                ...finalSettings,
                hasCredentials: {
                    ...finalSettings.hasCredentials,
                    [finalSettings.ess]: true
                }
            } : finalSettings;

            const isESSNowConfigured = updatedSettings ? (!!updatedSettings.ess && updatedSettings.ess !== "" && !!updatedSettings.hasCredentials?.[updatedSettings.ess]) : false;

            const isTeslaSetup = !!credentialsPayload?.['tesla'] || (finalSettings.ess === 'tesla' && !finalSettings.customGridSettings && !isESSAlreadyConfigured && isESSNowConfigured);
            if (isTeslaSetup) {
                setTeslaGridDirty(false);
                setShowTeslaGridModal(true);
            } else {
                if (!isESSAlreadyConfigured && isESSNowConfigured && !updatedSettings.pause) {
                    if (onShowTutorial) {
                        onShowTutorial();
                    }
                }
            }

            if (credentialsPayload && settings.ess) {
                setSettings(updatedSettings);
                setEditESS(false);
                setEssCredentials({});
                setOauthStatus('idle');
            }
            setIsUtilityDirty(false);
            setIsESSDirty(false);
            originalSettingsRef.current = JSON.stringify(updatedSettings);

            setTimeout(() => setSuccessMessage(null), 3000);
        } catch (err) {
            const errMsg = err instanceof Error ? err.message : 'Failed to save settings';
            if (errMsg.toLowerCase().includes("authorization code is invalid") || errMsg.toLowerCase().includes("code is invalid")) {
                setError("The authorization code expired. Please click 'Login to link account' again and save immediately.");
                setEditESS(true);
            } else {
                setError(errMsg);
            }
            setOauthStatus('idle');

            const provider = essProviders.find(p => p.id.toLowerCase() === settings.ess?.toLowerCase());
            const oneTimeFields = new Set<string>(['authCode']);
            if (provider) {
                const fields = (provider.credentials || [])
                    .filter(c => c.oneTime)
                    .map(c => c.field);
                fields.forEach(f => oneTimeFields.add(f));
                if (provider.oAuthKey && provider.oAuthKey.oneTime) {
                    oneTimeFields.add(provider.oAuthKey.field);
                }
            }

            setEssCredentials(prev => {
                const next = { ...prev };
                oneTimeFields.forEach(field => {
                    delete next[field];
                });
                return next;
            });
        } finally {
            savingRef.current = false;
            setIsSaving(false);
        }
    };

    const handleChange = <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => {
        setSettings(prev => prev ? ({ ...prev, [field]: value }) : null);
    };

    const scrollToSection = (testId: string) => {
        const element = document.querySelector(`[data-testid="${testId}"]`);
        if (element) {
            element.scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
    };

    const handleWizardSave = async (e: React.FormEvent, step: number) => {
        e.preventDefault();
        if (!settings || savingRef.current) return;

        if (step === 3 && editESS && maxStage > currentStage) {
            handleESSContinue(e);
            return;
        }

        if (step < 3) {
            if (step === 2) {
                const utilityProvider = utilities.find(u => u.id === settings.utilityProvider);
                const utilityRate = (utilityProvider?.rates || []).find(r => r.id === settings.utilityRate);
                if (utilityRate) {
                    const finalOpts = { ...settings.utilityRateOptions };
                    (utilityRate.options || []).forEach(opt => {
                        if (finalOpts[opt.field] === undefined || finalOpts[opt.field] === null || finalOpts[opt.field] === "") {
                            if (opt.default !== undefined) {
                                finalOpts[opt.field] = opt.default;
                            }
                        }
                    });
                    setSettings(prev => prev ? ({ ...prev, utilityRateOptions: finalOpts }) : null);
                }
            }
            setWizardStep(prev => prev + 1);
            return;
        }

        const isESSAlreadyConfigured = settings ? (!!settings.ess && settings.ess !== "" && !!settings.hasCredentials?.[settings.ess]) : false;

        let success = false;
        try {
            savingRef.current = true;
            setIsSaving(true);
            setError(null);
            setSuccessMessage(null);

            let credentialsPayload: CredentialsPayload | undefined = undefined;
            const finalSettings = { ...settings };
            if (finalSettings.minBatterySOCPeriods && finalSettings.minBatterySOCPeriods.length > 0) {
                finalSettings.minBatterySOCPeriods = finalSettings.minBatterySOCPeriods.map(p => {
                    const parsed = typeof p.minBatterySOC === 'number' ? p.minBatterySOC : parseFloat(p.minBatterySOC as any);
                    return {
                        ...p,
                        minBatterySOC: !isNaN(parsed) ? parsed : (typeof finalSettings.minBatterySOC === 'number' && !isNaN(finalSettings.minBatterySOC) ? finalSettings.minBatterySOC : 20),
                    };
                });
            }
            if (finalSettings.minBatterySOC !== undefined && finalSettings.minBatterySOC !== null) {
                const parsed = typeof finalSettings.minBatterySOC === 'number' ? finalSettings.minBatterySOC : parseFloat(finalSettings.minBatterySOC as any);
                finalSettings.minBatterySOC = !isNaN(parsed) ? parsed : 20;
            }

            const essProvider = essProviders.find(p => p.id === settings.ess);
            if (essProvider && (isESSDirty || Object.keys(essCredentials).length > 0)) {
                credentialsPayload = { [essProvider.id]: {} };
                const processCred = (cred: ESSCredentialField) => {
                    let val = essCredentials[cred.field];
                    if (val === undefined || val === null || val === "") {
                        val = cred.default;
                    }

                    if (cred.required && (val === undefined || val === null || val === "")) {
                        if (cred.field === 'authCode') {
                            setEditESS(true);
                            throw new Error('Login to connect your energy system.');
                        }
                        throw new Error(`The ${cred.name} field is required.`);
                    }
                    if (val !== undefined && val !== null && val !== "") {
                        credentialsPayload![essProvider.id][cred.field] = val;
                    }
                };

                for (const cred of (essProvider.credentials || [])) {
                    processCred(cred);
                }
                if (essProvider.oAuthKey) {
                    processCred(essProvider.oAuthKey);
                }
            }

            await updateSettings(finalSettings, siteID, credentialsPayload);
            setSuccessMessage('Setup complete!');

            if (onSettingsSaved) {
                await onSettingsSaved();
            }

            const isESSNowConfigured = !!finalSettings.ess && finalSettings.ess !== "" && (!!finalSettings.hasCredentials?.[finalSettings.ess] || !!credentialsPayload?.[finalSettings.ess]);

            if (credentialsPayload && finalSettings.ess) {
                setEditESS(false);
                setEssCredentials({});
                setOauthStatus('idle');
            }
            setIsUtilityDirty(false);
            setIsESSDirty(false);
            if (finalSettings.ess === 'tesla') {
                savingRef.current = false;
                setIsSaving(false);
                setTeslaGridDirty(false);
                setShowTeslaGridModal(true);
            } else {
                if (!isESSAlreadyConfigured && isESSNowConfigured && !finalSettings.pause) {
                    if (onShowTutorial) {
                        onShowTutorial();
                    }
                }
                setTimeout(() => {
                    navigate('/dashboard');
                }, 1000);
            }
            success = true;
        } catch (err) {
            const errMsg = err instanceof Error ? err.message : 'Failed to save settings';
            setError(errMsg);
        } finally {
            if (!success) {
                savingRef.current = false;
                setIsSaving(false);
            }
        }
    };

    const handleConfirmTeslaGridSettings = async () => {
        if (!settings) return;
        if (!teslaGridDirty) {
            setShowTeslaGridModal(false);
            if (onShowTutorial && !settings.pause) {
                onShowTutorial();
            }
            if (isInWizard) {
                navigate('/dashboard');
            }
            return;
        }

        try {
            setIsSaving(true);
            await updateSettings({ ...settings, customGridSettings: true }, siteID);
            setShowTeslaGridModal(false);
            if (onSettingsSaved) {
                await onSettingsSaved();
            }
            if (onShowTutorial && !settings.pause) {
                onShowTutorial();
            }
            if (isInWizard) {
                navigate('/dashboard');
            }
        } catch (err) {
            const errMsg = err instanceof Error ? err.message : 'Failed to save grid settings';
            setError(errMsg);
        } finally {
            setIsSaving(false);
        }
    };

    const handleOAuthLogin = (url: string, fieldName: string) => {
        const width = 500;
        const height = 600;
        const left = window.screenX + (window.outerWidth - width) / 2;
        const top = window.screenY + (window.outerHeight - height) / 2;

        const oauthUrl = new URL(url);
        // add the siteID as the state parameter
        if (siteID) {
            oauthUrl.searchParams.set('state', siteID);
        }

        const popup = window.open(
            oauthUrl.toString(),
            'OAuthLogin',
            `width=${width},height=${height},left=${left},top=${top},status=yes,scrollbars=yes`
        );

        if (!popup) {
            setError('Please allow popups for this site to log in.');
            return;
        }

        // Clear any existing listener/timer before starting a new one
        if (oauthTimerRef.current) {
            clearInterval(oauthTimerRef.current);
            oauthTimerRef.current = null;
        }
        if (oauthListenerRef.current) {
            window.removeEventListener('message', oauthListenerRef.current);
            oauthListenerRef.current = null;
        }

        setOauthStatus('popup_open');

        const listener = (event: MessageEvent) => {
            if (event.origin !== window.location.origin) return;

            if (event.data && event.data.type === 'OAUTH_CODE') {
                if (siteID && event.data.state !== siteID) {
                    setError('Authentication state mismatch. Please try again.');
                    window.removeEventListener('message', listener);
                    oauthListenerRef.current = null;
                    return;
                }

                setEssCredentials(prev => ({
                    ...prev,
                    [fieldName]: event.data.code
                }));
                setOauthStatus('success');
                setIsESSDirty(true);
                window.removeEventListener('message', listener);
                oauthListenerRef.current = null;
                if (oauthTimerRef.current) {
                    clearInterval(oauthTimerRef.current);
                    oauthTimerRef.current = null;
                }
            }
        };

        window.addEventListener('message', listener);
        oauthListenerRef.current = listener;

        const timer = setInterval(() => {
            if (popup.closed) {
                clearInterval(timer);
                oauthTimerRef.current = null;
                window.removeEventListener('message', listener);
                oauthListenerRef.current = null;
                setOauthStatus(current => {
                    if (current === 'popup_open') {
                        return 'idle';
                    }
                    return current;
                });
            }
        }, 500);
        oauthTimerRef.current = timer;
    };

    if (loading) return (
        <div className="loading-screen">
            <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
            Loading settings...
        </div>
    );
    if (!settings) return <div>Error loading settings</div>;

    const provider = essProviders.find(p => p.id === settings.ess);
    const maxStage = provider
        ? (provider.credentials || []).reduce((max, cred) => {
              const stage = cred.stage ?? 0;
              return stage > max ? stage : max;
          }, 0)
        : 0;

    const initialSettingsObj: SettingsType | null = (() => {
        if (parentSettings) return parentSettings;
        if (originalSettingsRef.current) {
            try {
                return JSON.parse(originalSettingsRef.current);
            } catch {
                return null;
            }
        }
        return settings;
    })();

    const isLocationConfigured = !!(initialSettingsObj?.countryCode && initialSettingsObj?.postalCode) || !!(settings?.countryCode && settings?.postalCode);
    const isUtilityConfigured = !!(initialSettingsObj?.utilityProvider && initialSettingsObj?.utilityProvider !== "") || !!(settings?.utilityProvider && settings?.utilityProvider !== "");
    const isESSConfigured = !!(initialSettingsObj?.ess && initialSettingsObj.ess !== "" && initialSettingsObj.hasCredentials?.[initialSettingsObj.ess]) || !!(settings?.ess && settings.ess !== "" && settings?.hasCredentials?.[settings.ess]);

    const showWizard = !!(settings && isInWizard && !forceFullSettings);
    const showChecklist = !!(settings && !showWizard && (!isUtilityConfigured || !isESSConfigured) && !forceFullSettings);

    const locationHighlightClass = showChecklist && !isLocationConfigured ? "highlighted-section" : "";
    const utilityHighlightClass = showChecklist && !isUtilityConfigured && !isUtilityDirty ? "highlighted-section" : "";
    const essHighlightClass = showChecklist && !isESSConfigured && !isESSDirty ? "highlighted-section" : "";

    const renderTeslaGridModal = () => (
        <Dialog.Root open={showTeslaGridModal} onOpenChange={(open) => {
            if (!open) {
                handleConfirmTeslaGridSettings();
            }
        }}>
            <Dialog.Portal>
                <Dialog.Backdrop className="dialog-backdrop" />
                <Dialog.Popup className="dialog-popup" data-testid="tesla-grid-modal">
                    <Dialog.Title className="dialog-title">Confirm Grid Settings</Dialog.Title>
                    <Dialog.Description className="dialog-description">
                        Based on your Tesla Powerwall configuration, we've detected the following grid settings. Please confirm or adjust them:
                    </Dialog.Description>
                    {settings && (
                        <div className="grid-strategy-grid" style={{ marginTop: '1rem', marginBottom: '1.5rem' }}>
                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="modal-gridChargeBatteries"
                                        checked={settings.gridChargeBatteries ?? true}
                                        onCheckedChange={(checked) => {
                                            handleChange('gridChargeBatteries', checked);
                                            setTeslaGridDirty(true);
                                        }}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="modal-gridChargeBatteries">Grid Can Charge Battery</Field.Label>
                                </div>
                            </Field.Root>

                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="modal-gridExportSolar"
                                        checked={settings.gridExportSolar ?? false}
                                        onCheckedChange={(checked) => {
                                            handleChange('gridExportSolar', checked);
                                            setTeslaGridDirty(true);
                                        }}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="modal-gridExportSolar">Export Solar to Grid</Field.Label>
                                </div>
                            </Field.Root>

                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="modal-gridExportBatteries"
                                        checked={settings.gridExportBatteries ?? false}
                                        onCheckedChange={(checked) => {
                                            handleChange('gridExportBatteries', checked);
                                            setTeslaGridDirty(true);
                                        }}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="modal-gridExportBatteries">Export Battery to Grid</Field.Label>
                                </div>
                            </Field.Root>

                            {!settings.gridChargeBatteries && !settings.gridExportSolar && !settings.gridExportBatteries && (
                                <div className="warning-notice" style={{ gridColumn: '1 / -1', marginTop: 0 }} data-testid="tesla-grid-modal-warning">
                                    Warning: All grid interactions are disabled. The system will only charge from solar and will not charge from the grid or export any energy.
                                </div>
                            )}
                        </div>
                    )}
                    <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
                        <button
                            type="button"
                            className="btn btn-primary"
                            onClick={handleConfirmTeslaGridSettings}
                            disabled={isSaving}
                        >
                            {isSaving ? 'Saving...' : 'Confirm & Finish'}
                        </button>
                    </div>
                </Dialog.Popup>
            </Dialog.Portal>
        </Dialog.Root>
    );

    if (showWizard) {
        return (
            <div className="content-container settings-container wizard-container">
                <div className="wizard-card">
                    <div className="wizard-step-indicators">
                        <div className={`wizard-step-bubble ${wizardStep >= 1 ? 'active' : ''} ${wizardStep > 1 ? 'completed' : ''}`}>
                            {wizardStep > 1 ? '✓' : '1'}
                        </div>
                        <div className="wizard-step-line" />
                        <div className={`wizard-step-bubble ${wizardStep >= 2 ? 'active' : ''} ${wizardStep > 2 ? 'completed' : ''}`}>
                            {wizardStep > 2 ? '✓' : '2'}
                        </div>
                        <div className="wizard-step-line" />
                        <div className={`wizard-step-bubble ${wizardStep >= 3 ? 'active' : ''} ${wizardStep > 3 ? 'completed' : ''}`}>
                            {wizardStep > 3 ? '✓' : '3'}
                        </div>
                    </div>

                    <div className="wizard-header">
                        <h2>
                            {wizardStep === 1 && "Step 1: Set Location"}
                            {wizardStep === 2 && "Step 2: Choose Utility"}
                            {wizardStep === 3 && "Step 3: Connect Battery"}
                        </h2>
                        <p className="wizard-subtitle">
                            {wizardStep === 1 && "Location helps us fetch the correct weather forecast for solar output predictions."}
                            {wizardStep === 2 && "Configure your electricity utility rate plan to optimize battery charging and grid exports."}
                            {wizardStep === 3 && "Connect your Energy Storage System (ESS) to enable automated charging and discharging."}
                        </p>
                    </div>

                    <form onSubmit={(e) => handleWizardSave(e, wizardStep)}>
                        {error && <div className="error-message">{error}</div>}
                        {successMessage && <div className="success-message">{successMessage}</div>}

                        {wizardStep === 1 && (
                            <div className="wizard-step-fields">
                                <LocationForm settings={settings} onChange={handleChange} />
                            </div>
                        )}

                        {wizardStep === 2 && (
                            <div className="wizard-step-fields">
                                <UtilityForm
                                    settings={settings}
                                    onChange={handleChange}
                                    utilities={utilities}
                                    isWizard={true}
                                    isUtilityDirty={isUtilityDirty}
                                    setIsUtilityDirty={setIsUtilityDirty}
                                    onUtilityChange={validateUtilityAndPeriods}
                                />
                            </div>
                        )}

                        {wizardStep === 3 && (
                            <div className="wizard-step-fields">
                                <ESSForm
                                    settings={settings}
                                    onChange={handleChange}
                                    essProviders={essProviders}
                                    essCredentials={essCredentials}
                                    setEssCredentials={setEssCredentials}
                                    isESSDirty={isESSDirty}
                                    setIsESSDirty={setIsESSDirty}
                                    isSaving={isSaving}
                                    isStaging={isStaging}
                                    oauthStatus={oauthStatus}
                                    setOauthStatus={setOauthStatus}
                                    currentStage={currentStage}
                                    setCurrentStage={setCurrentStage}
                                    handleOAuthLogin={handleOAuthLogin}
                                    handleESSContinue={handleESSContinue}
                                    isWizard={true}
                                />
                            </div>
                        )}

                        <div className="wizard-actions">
                            {wizardStep > 1 && (
                                <button
                                    type="button"
                                    className="btn btn-secondary"
                                    onClick={() => setWizardStep(prev => prev - 1)}
                                    disabled={isSaving || isStaging}
                                >
                                    Back
                                </button>
                            )}
                            <button
                                type="submit"
                                className="btn btn-primary"
                                disabled={isSaving || isStaging || (wizardStep === 2 && !settings.utilityProvider) || (wizardStep === 3 && !settings.ess)}
                            >
                                {isSaving || isStaging ? (
                                    <>
                                        <span className="loading-spinner" aria-hidden="true" style={{ marginRight: '0.5rem' }}></span>
                                        Saving...
                                    </>
                                ) : (
                                    wizardStep === 3 ? 'Complete Setup' : 'Save & Continue'
                                )}
                            </button>
                        </div>
                    </form>

                    <div className="wizard-footer">
                        <button
                            type="button"
                            className="text-button wizard-skip-link"
                            onClick={() => {
                                setForceFullSettings(true);
                                setIsInWizard(false);
                            }}
                        >
                            Skip setup wizard and view all settings
                        </button>
                    </div>
                </div>
                {renderTeslaGridModal()}
            </div>
        );
    }

    return (
        <div className="content-container settings-container">
            {showChecklist && (
                <div className="checklist-banner font-sans" data-testid="checklist-banner">
                    <h4>Getting Started Checklist</h4>
                    <p>Configure the following settings to enable RateRudder's automation:</p>
                    <div className="checklist-items">
                        <div
                            className={`checklist-item ${isLocationConfigured ? 'checked' : 'pending'}`}
                            onClick={() => scrollToSection('location-section')}
                        >
                            <span className="checklist-icon">{isLocationConfigured ? '✓' : '○'}</span>
                            <span className="checklist-text">Set Location</span>
                        </div>
                        <div
                            className={`checklist-item ${isUtilityConfigured ? 'checked' : 'pending'}`}
                            onClick={() => scrollToSection('utility-section')}
                        >
                            <span className="checklist-icon">{isUtilityConfigured ? '✓' : '○'}</span>
                            <span className="checklist-text">Choose Utility</span>
                        </div>
                        <div
                            className={`checklist-item ${isESSConfigured ? 'checked' : 'pending'}`}
                            onClick={() => scrollToSection('ess-section')}
                        >
                            <span className="checklist-icon">{isESSConfigured ? '✓' : '○'}</span>
                            <span className="checklist-text">Connect Battery (ESS)</span>
                        </div>
                    </div>
                </div>
            )}
            <h2>Settings</h2>
            <form id="settings-form" onSubmit={handleSubmit}>
                {/* Battery section at the top */}
                <div className="settings-section" data-testid="battery-section">
                    <div className="section-header">
                        <h3>Battery</h3>
                        {!editBattery && (
                            (!settings.minBatterySOCPeriods || settings.minBatterySOCPeriods.length === 0) ? (
                                <button
                                    type="button"
                                    className="text-button"
                                    id="configureReserveScheduleBtn"
                                    onClick={() => {
                                        setEditBattery(true);
                                        handleOpenReserveSchedule();
                                    }}
                                    disabled={loadingPeriods}
                                >
                                    {loadingPeriods ? 'Loading...' : 'Advanced'}
                                </button>
                            ) : (
                                <button
                                    type="button"
                                    className="text-button"
                                    onClick={() => setEditBattery(true)}
                                >
                                    Change
                                </button>
                            )
                        )}
                    </div>

                    {!editBattery && settings.minBatterySOCPeriods && settings.minBatterySOCPeriods.length > 0 ? (
                        <button type="button" className="configured-summary" onClick={() => setEditBattery(true)} aria-label="Edit Battery settings">
                            <div className="summary-info">
                                <span className="summary-label">Variable Reserve Schedule ({scheduleMode === 'named' ? 'Rate Periods' : 'Custom Hours'})</span>
                                <span className="summary-sublabel">
                                    {settings.minBatterySOCPeriods.map(p =>
                                        p.utilityPeriodName ? `${p.utilityPeriodName}: ${p.minBatterySOC}%` : `${p.hours?.[0]?.hourStart ?? 0}:00-${p.hours?.[0]?.hourEnd ?? 24}:00: ${p.minBatterySOC}%`
                                    ).join(' | ')}
                                </span>
                            </div>
                        </button>
                    ) : (
                        <div className="form-grid compact-grid" data-testid="variable-reserve-section">
                            {batteryError && (
                                <div style={{ gridColumn: '1 / -1', color: 'var(--error, #ef4444)', fontSize: '0.85rem', marginBottom: '0.75rem', fontWeight: 600 }} data-testid="battery-period-error">
                                    ⚠️ {batteryError}
                                </div>
                            )}
                            {(!settings.minBatterySOCPeriods || settings.minBatterySOCPeriods.length === 0) && (
                                <Field.Root className="form-group compact">
                                    <Field.Label htmlFor="minBatterySOC">
                                        Minimum Battery %
                                        <HelpButton
                                            title="Minimum Battery Reserve %"
                                            description="Sets the minimum state-of-charge (SOC) level that RateRudder must preserve in your battery. RateRudder will avoid discharging the battery below this reserve to protect against power outages and grid disruptions."
                                        />
                                    </Field.Label>
                                    <Input
                                        id="minBatterySOC"
                                        type="number"
                                        step="1"
                                        min="0"
                                        max="100"
                                        value={settings.minBatterySOC ?? ''}
                                        onChange={(e) => handleChange('minBatterySOC', e.target.value === '' ? ('' as any) : parseFloat(e.target.value))}
                                    />
                                    <Field.Description>Maintain battery charge at or above this level at all costs.</Field.Description>
                                </Field.Root>
                            )}

                            {settings.minBatterySOCPeriods && settings.minBatterySOCPeriods.length > 0 && (
                                <>
                                    <div style={{ gridColumn: '1 / -1', display: 'flex', alignItems: 'center', gap: '0.25rem', marginBottom: '0.5rem' }}>
                                        <span style={{ fontWeight: 700, fontSize: '0.95rem', color: 'var(--on-surface)' }}>
                                            Variable Reserve Schedule ({scheduleMode === 'named' ? 'Rate Periods' : 'Custom Hours'})
                                        </span>
                                        {scheduleMode === 'named' ? (
                                            <HelpButton
                                                title="Rate Period Reserve Schedule"
                                                description="Configures specific minimum battery reserve percentages for each utility rate period (e.g. On-Peak, Off-Peak). RateRudder ensures your battery holds higher reserves during expensive peak hours while allowing lower reserves during cheap off-peak hours for maximum savings."
                                            />
                                        ) : (
                                            <HelpButton
                                                title="Custom Hourly Reserve Schedule"
                                                description="Allows you to define custom 24-hour time windows and assign individual minimum battery reserve percentages to each window. RateRudder enforces these SOC thresholds strictly throughout the day according to your custom schedule."
                                            />
                                        )}
                                    </div>
                                    {scheduleMode === 'named' ? (
                                        <>
                                            {settings.minBatterySOCPeriods.map((p, idx) => (
                                                <Field.Root key={p.utilityPeriodName || idx} className="form-group compact">
                                                    <Field.Label htmlFor={`period-${p.utilityPeriodName}`}>
                                                        {p.utilityPeriodName} Reserve %
                                                    </Field.Label>
                                                    <Input
                                                        id={`period-${p.utilityPeriodName}`}
                                                        type="number"
                                                        step="1"
                                                        min="0"
                                                        max="100"
                                                        value={p.minBatterySOC ?? ''}
                                                        onChange={(e) => {
                                                            const val = e.target.value === '' ? ('' as any) : parseFloat(e.target.value);
                                                            const next = [...settings.minBatterySOCPeriods!];
                                                            next[idx] = { ...next[idx], minBatterySOC: val };
                                                            updateMinBatterySOCPeriods(next);
                                                        }}
                                                    />
                                                </Field.Root>
                                            ))}
                                        </>
                                    ) : (
                                        <div style={{ gridColumn: '1 / -1' }}>
                                            {(() => {
                                                const coverageError = validate24HourCoverage(settings.minBatterySOCPeriods);
                                                return (
                                                    <>
                                                        {coverageError && (
                                                            <div style={{ color: 'var(--error, #ef4444)', fontSize: '0.85rem', marginBottom: '0.75rem', fontWeight: 600 }}>
                                                                {coverageError}
                                                            </div>
                                                        )}
                                                        {settings.minBatterySOCPeriods.map((p, idx) => {
                                                            const hp = p.hours?.[0] || { hourStart: 0, hourEnd: 24 };
                                                            return (
                                                                <div key={idx} style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', marginBottom: '0.75rem', flexWrap: 'wrap' }}>
                                                                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                                                                        <span style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>From:</span>
                                                                        <Select.Root
                                                                            value={String(hp.hourStart)}
                                                                            onValueChange={(val) => {
                                                                                const start = parseInt(val as string, 10);
                                                                                const next = [...settings.minBatterySOCPeriods!];
                                                                                next[idx] = {
                                                                                    ...next[idx],
                                                                                    hours: [{ hourStart: start, minuteStart: 0, hourEnd: hp.hourEnd, minuteEnd: 0 }],
                                                                                };
                                                                                updateMinBatterySOCPeriods(next);
                                                                            }}
                                                                        >
                                                                            <Select.Trigger
                                                                                className="select-trigger"
                                                                                aria-label="Start Hour"
                                                                                style={{ padding: '0.4rem 0.625rem', height: '36px', borderRadius: 'var(--radius-md)', boxSizing: 'border-box', gap: '0.5rem', width: 'auto' }}
                                                                            >
                                                                                <Select.Value>
                                                                                    {String(hp.hourStart).padStart(2, '0')}:00
                                                                                </Select.Value>
                                                                                <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                                                    </svg>
                                                                                </Select.Icon>
                                                                            </Select.Trigger>
                                                                            <Select.Portal>
                                                                                <Select.Positioner className="select-positioner">
                                                                                    <Select.Popup className="select-popup">
                                                                                        {Array.from({ length: 24 }, (_, i) => (
                                                                                            <Select.Item key={i} className="select-item" value={String(i)}>
                                                                                                <Select.ItemText>{String(i).padStart(2, '0')}:00</Select.ItemText>
                                                                                            </Select.Item>
                                                                                        ))}
                                                                                    </Select.Popup>
                                                                                </Select.Positioner>
                                                                            </Select.Portal>
                                                                        </Select.Root>
                                                                    </div>
                                                                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                                                                        <span style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>To:</span>
                                                                        <Select.Root
                                                                            value={String(hp.hourEnd)}
                                                                            onValueChange={(val) => {
                                                                                const end = parseInt(val as string, 10);
                                                                                const next = [...settings.minBatterySOCPeriods!];
                                                                                next[idx] = {
                                                                                    ...next[idx],
                                                                                    hours: [{ hourStart: hp.hourStart, minuteStart: 0, hourEnd: end, minuteEnd: 0 }],
                                                                                };
                                                                                updateMinBatterySOCPeriods(next);
                                                                            }}
                                                                        >
                                                                            <Select.Trigger
                                                                                className="select-trigger"
                                                                                aria-label="End Hour"
                                                                                style={{ padding: '0.4rem 0.625rem', height: '36px', borderRadius: 'var(--radius-md)', boxSizing: 'border-box', gap: '0.5rem', width: 'auto' }}
                                                                            >
                                                                                <Select.Value>
                                                                                    {String(hp.hourEnd === 24 ? 24 : hp.hourEnd).padStart(2, '0')}:00
                                                                                </Select.Value>
                                                                                <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                                                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                                                    </svg>
                                                                                </Select.Icon>
                                                                            </Select.Trigger>
                                                                            <Select.Portal>
                                                                                <Select.Positioner className="select-positioner">
                                                                                    <Select.Popup className="select-popup">
                                                                                        {Array.from({ length: 25 }, (_, i) => (
                                                                                            <Select.Item key={i} className="select-item" value={String(i)}>
                                                                                                <Select.ItemText>{String(i === 24 ? 24 : i).padStart(2, '0')}:00</Select.ItemText>
                                                                                            </Select.Item>
                                                                                        ))}
                                                                                    </Select.Popup>
                                                                                </Select.Positioner>
                                                                            </Select.Portal>
                                                                        </Select.Root>
                                                                    </div>
                                                                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                                                                        <span style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>SOC %:</span>
                                                                        <Input
                                                                            type="number"
                                                                            step="1"
                                                                            min="0"
                                                                            max="100"
                                                                            className="input"
                                                                            style={{ width: '80px', height: '36px', padding: '0.4rem 0.625rem', boxSizing: 'border-box' }}
                                                                            value={p.minBatterySOC ?? ''}
                                                                            onChange={(e) => {
                                                                                const val = e.target.value === '' ? ('' as any) : parseFloat(e.target.value);
                                                                                const next = [...settings.minBatterySOCPeriods!];
                                                                                next[idx] = { ...next[idx], minBatterySOC: val };
                                                                                updateMinBatterySOCPeriods(next);
                                                                            }}
                                                                        />
                                                                    </div>
                                                                    {(settings.minBatterySOCPeriods?.length ?? 0) > 1 && (
                                                                        <button
                                                                            type="button"
                                                                            className="text-button danger"
                                                                            onClick={() => {
                                                                                const next = settings.minBatterySOCPeriods!.filter((_, i) => i !== idx);
                                                                                updateMinBatterySOCPeriods(next);
                                                                            }}
                                                                        >
                                                                            Remove
                                                                        </button>
                                                                    )}
                                                                </div>
                                                            );
                                                        })}
                                                        <button
                                                            type="button"
                                                            className="text-button"
                                                            onClick={() => {
                                                                const lastEnd = settings.minBatterySOCPeriods![settings.minBatterySOCPeriods!.length - 1]?.hours?.[0]?.hourEnd || 0;
                                                                const next = [
                                                                    ...settings.minBatterySOCPeriods!,
                                                                    { hours: [{ hourStart: lastEnd < 24 ? lastEnd : 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: settings.minBatterySOC || 20 }
                                                                ];
                                                                updateMinBatterySOCPeriods(next);
                                                            }}
                                                        >
                                                            + Add Period
                                                        </button>
                                                    </>
                                                );
                                            })()}
                                        </div>
                                    )}

                                    <p className="section-description" style={{ gridColumn: '1 / -1', marginTop: '0.75rem', marginBottom: 0 }}>
                                        {scheduleMode === 'named'
                                            ? "Maintain battery charge at or above the level for each rate period at all costs."
                                            : "Maintain battery charge at or above the level for each time period at all costs."}
                                    </p>
                                </>
                            )}

                            {editBattery && (
                                <div style={{ gridColumn: '1 / -1', marginTop: '1rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '0.5rem' }}>
                                    <button
                                        type="button"
                                        className="text-button cancel-button"
                                        onClick={() => setEditBattery(false)}
                                    >
                                        Done
                                    </button>

                                    <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
                                        {(!settings.minBatterySOCPeriods || settings.minBatterySOCPeriods.length === 0) ? (
                                            <>
                                                {hasNamedRatePeriods && (
                                                    <button
                                                        type="button"
                                                        className="text-button"
                                                        onClick={async () => {
                                                            let periods = utilityPeriods;
                                                            if (!periods) {
                                                                periods = await fetchUtilityPeriods(siteID);
                                                                setUtilityPeriods(periods || []);
                                                            }
                                                            const fetchedNames = periods ? periods.filter(p => p.name && p.name !== '').map(p => p.name!) : [];
                                                            if (fetchedNames.length === 0) {
                                                                setBatteryError(null);
                                                                setScheduleMode('custom');
                                                                const custom: MinBatterySOCPeriod[] = [
                                                                    { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: settings.minBatterySOC || 20 }
                                                                ];
                                                                updateMinBatterySOCPeriods(custom);
                                                                return;
                                                            }
                                                            const uniqueNames = Array.from(new Set(fetchedNames));
                                                            const initialPeriods: MinBatterySOCPeriod[] = uniqueNames.map(name => ({
                                                                utilityPeriodName: name,
                                                                minBatterySOC: settings.minBatterySOC || 20,
                                                            }));
                                                            updateMinBatterySOCPeriods(initialPeriods);
                                                            setScheduleMode('named');
                                                            setBatteryError(null);
                                                        }}
                                                    >
                                                        Rates Mode
                                                    </button>
                                                )}
                                                <button
                                                    type="button"
                                                    className="text-button"
                                                    onClick={() => {
                                                        setScheduleMode('custom');
                                                        const custom: MinBatterySOCPeriod[] = [
                                                            { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: settings.minBatterySOC || 20 }
                                                        ];
                                                        updateMinBatterySOCPeriods(custom);
                                                    }}
                                                >
                                                    Custom Mode
                                                </button>
                                            </>
                                        ) : (
                                            <>
                                                {scheduleMode === 'named' ? (
                                                    <button
                                                        type="button"
                                                        className="text-button"
                                                        onClick={() => {
                                                            setScheduleMode('custom');
                                                            const custom: MinBatterySOCPeriod[] = [
                                                                { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: settings.minBatterySOC || 20 }
                                                            ];
                                                            updateMinBatterySOCPeriods(custom);
                                                        }}
                                                    >
                                                        Custom Mode
                                                    </button>
                                                ) : (
                                                    hasNamedRatePeriods && (
                                                        <button
                                                            type="button"
                                                            className="text-button"
                                                            onClick={async () => {
                                                                let periods = utilityPeriods;
                                                                if (!periods) {
                                                                    periods = await fetchUtilityPeriods(siteID);
                                                                    setUtilityPeriods(periods || []);
                                                                }
                                                                const fetchedNames = periods ? periods.filter(p => p.name && p.name !== '').map(p => p.name!) : [];
                                                                if (fetchedNames.length === 0) {
                                                                    setBatteryError(null);
                                                                    setScheduleMode('custom');
                                                                    const custom: MinBatterySOCPeriod[] = [
                                                                        { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: settings.minBatterySOC || 20 }
                                                                    ];
                                                                    updateMinBatterySOCPeriods(custom);
                                                                    return;
                                                                }
                                                                const uniqueNames = Array.from(new Set(fetchedNames));
                                                                const initialPeriods: MinBatterySOCPeriod[] = uniqueNames.map(name => ({
                                                                    utilityPeriodName: name,
                                                                    minBatterySOC: settings.minBatterySOC || 20,
                                                                }));
                                                                updateMinBatterySOCPeriods(initialPeriods);
                                                                setScheduleMode('named');
                                                                setBatteryError(null);
                                                            }}
                                                        >
                                                            Rates Mode
                                                        </button>
                                                    )
                                                )}

                                                <button
                                                    type="button"
                                                    className="text-button danger"
                                                    onClick={() => {
                                                        updateMinBatterySOCPeriods(undefined);
                                                    }}
                                                >
                                                    Revert to Simple
                                                </button>
                                            </>
                                        )}
                                    </div>
                                </div>
                            )}
                        </div>
                    )}
                </div>

                {/* Location directly under Quick Settings */}
                <div className={`settings-section ${locationHighlightClass}`} data-testid="location-section">
                    <div className="section-header">
                        <h3>Location</h3>
                    </div>
                    <div className="grid-strategy-grid">
                        <LocationForm settings={settings} onChange={handleChange} />
                    </div>

                <div className="weather-attribution">
                    Weather data provided by <a href="https://open-meteo.com" target="_blank" rel="noopener noreferrer">Open-Meteo</a> to improve solar prediction
                </div>
                </div>

                {/* Utility Service Section */}
                <div className={`settings-section ${utilityHighlightClass}`} data-testid="utility-section">
                    <div className="section-header">
                        <h3>Utility Service</h3>
                        {settings.utilityProvider && settings.utilityRate && !editUtility && (
                        <button type="button" className="text-button" onClick={() => setEditUtility(true)} aria-label="Change Utility Service">Change</button>
                    )}
                </div>

                <UtilityForm
                    settings={settings}
                    onChange={handleChange}
                    utilities={utilities}
                    editUtility={editUtility}
                    setEditUtility={setEditUtility}
                    isUtilityDirty={isUtilityDirty}
                    setIsUtilityDirty={setIsUtilityDirty}
                    onUtilityChange={validateUtilityAndPeriods}
                />
                </div>

                {/* ESS Configuration Section */}
                <div className={`settings-section ${essHighlightClass}`} data-testid="ess-section">
                    <div className="section-header">
                        <h3 id="ess-credentials">Energy Storage System</h3>
                        {settings.ess && settings.hasCredentials?.[settings.ess] && !editESS && (
                        <button type="button" className="text-button" onClick={() => setEditESS(true)} aria-label="Update Energy Storage System">Update</button>
                    )}
                </div>

                <ESSForm
                    settings={settings}
                    onChange={handleChange}
                    essProviders={essProviders}
                    essCredentials={essCredentials}
                    setEssCredentials={setEssCredentials}
                    isESSDirty={isESSDirty}
                    setIsESSDirty={setIsESSDirty}
                    isSaving={isSaving}
                    isStaging={isStaging}
                    oauthStatus={oauthStatus}
                    setOauthStatus={setOauthStatus}
                    currentStage={currentStage}
                    setCurrentStage={setCurrentStage}
                    handleOAuthLogin={handleOAuthLogin}
                    handleESSContinue={handleESSContinue}
                    isWizard={false}
                    editESS={editESS}
                    setEditESS={setEditESS}
                />
                </div>

                {isEVFeatureEnabled && (
                    <div className="settings-section" data-testid="ev-charging-section">
                        <div className="section-header">
                            <h3>EV Charging</h3>
                        </div>
                        <div className="grid-strategy-grid">
                            <Field.Root className="form-group switch-group compact" style={{ gridColumn: '1 / -1' }}>
                                <div className="switch-row">
                                    <Switch.Root
                                        id="avoidBatteryForEV"
                                        className="switch-root"
                                        checked={!!settings.evChargingPeriods && settings.evChargingPeriods.length > 0}
                                        onCheckedChange={async (checked) => {
                                            if (!checked) {
                                                handleChange('evChargingPeriods', undefined);
                                                setEVEstimationNote(null);
                                                setEVEstimationError(null);
                                                return;
                                            }
                                            setEstimatingEV(true);
                                            setEVEstimationError(null);
                                            setEVEstimationNote(null);
                                            try {
                                                const res = await fetchEstimateEVCharging(siteID);
                                                if (res.detected && res.recommendedPeriod) {
                                                    handleChange('evChargingPeriods', [res.recommendedPeriod]);
                                                    setEVEstimationNote(`Auto-detected ~${res.estimatedRateKW} kW charging based on ${res.sessionsCount} recent sessions.`);
                                                } else {
                                                    const defaultPeriod: TimePeriod = {
                                                        name: 'Nighttime EV Charging',
                                                        hours: [{ hourStart: 23, minuteStart: 0, hourEnd: 6, minuteEnd: 0 }],
                                                    };
                                                    handleChange('evChargingPeriods', [defaultPeriod]);
                                                    setEVEstimationError("We couldn't detect consistent nighttime EV charging in your recent history. Please verify your scheduled hours or leave feedback.");
                                                }
                                            } catch {
                                                const defaultPeriod: TimePeriod = {
                                                    name: 'Nighttime EV Charging',
                                                    hours: [{ hourStart: 23, minuteStart: 0, hourEnd: 6, minuteEnd: 0 }],
                                                };
                                                handleChange('evChargingPeriods', [defaultPeriod]);
                                                setEVEstimationError("Unable to analyze energy history. Please verify your scheduled hours or leave feedback.");
                                            } finally {
                                                setEstimatingEV(false);
                                            }
                                        }}
                                        disabled={estimatingEV}
                                        aria-label="Avoid Battery for EV Charging"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="avoidBatteryForEV">Avoid Battery for EV Charging</Field.Label>
                                </div>
                                <Field.Description>
                                    Designed for nighttime EV charging when solar is unavailable. During the day, excess solar energy naturally powers your vehicle without depleting your home battery.
                                </Field.Description>
                            </Field.Root>

                            {settings.evChargingPeriods && settings.evChargingPeriods.length > 0 && (
                                <>
                                    <Field.Root className="form-group compact">
                                        <Field.Label htmlFor="evHourStart">EV Charging Start Time</Field.Label>
                                        <Select.Root
                                            value={String(settings.evChargingPeriods[0]?.hours?.[0]?.hourStart ?? 23)}
                                            onValueChange={(val) => {
                                                const start = parseInt(val as string, 10);
                                                const current = settings.evChargingPeriods![0]?.hours?.[0] || { hourStart: 23, hourEnd: 6 };
                                                const updated: TimePeriod = {
                                                    name: 'Nighttime EV Charging',
                                                    hours: [{ hourStart: start, minuteStart: 0, hourEnd: current.hourEnd, minuteEnd: 0 }],
                                                };
                                                handleChange('evChargingPeriods', [updated]);
                                            }}
                                        >
                                            <Select.Trigger className="select-trigger" id="evHourStart" aria-label="EV Charging Start Time">
                                                <Select.Value>
                                                    {String(settings.evChargingPeriods[0]?.hours?.[0]?.hourStart ?? 23).padStart(2, '0')}:00
                                                </Select.Value>
                                                <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                    </svg>
                                                </Select.Icon>
                                            </Select.Trigger>
                                            <Select.Portal>
                                                <Select.Positioner className="select-positioner">
                                                    <Select.Popup className="select-popup">
                                                        {Array.from({ length: 24 }, (_, i) => (
                                                            <Select.Item key={i} className="select-item" value={String(i)}>
                                                                <Select.ItemText>{String(i).padStart(2, '0')}:00</Select.ItemText>
                                                            </Select.Item>
                                                        ))}
                                                    </Select.Popup>
                                                </Select.Positioner>
                                            </Select.Portal>
                                        </Select.Root>
                                    </Field.Root>

                                    <Field.Root className="form-group compact">
                                        <Field.Label htmlFor="evHourEnd">EV Charging End Time</Field.Label>
                                        <Select.Root
                                            value={String(settings.evChargingPeriods[0]?.hours?.[0]?.hourEnd ?? 6)}
                                            onValueChange={(val) => {
                                                const end = parseInt(val as string, 10);
                                                const current = settings.evChargingPeriods![0]?.hours?.[0] || { hourStart: 23, hourEnd: 6 };
                                                const updated: TimePeriod = {
                                                    name: 'Nighttime EV Charging',
                                                    hours: [{ hourStart: current.hourStart, minuteStart: 0, hourEnd: end, minuteEnd: 0 }],
                                                };
                                                handleChange('evChargingPeriods', [updated]);
                                            }}
                                        >
                                            <Select.Trigger className="select-trigger" id="evHourEnd" aria-label="EV Charging End Time">
                                                <Select.Value>
                                                    {String((settings.evChargingPeriods[0]?.hours?.[0]?.hourEnd ?? 6) === 24 ? 24 : settings.evChargingPeriods[0]?.hours?.[0]?.hourEnd ?? 6).padStart(2, '0')}:00
                                                </Select.Value>
                                                <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                    </svg>
                                                </Select.Icon>
                                            </Select.Trigger>
                                            <Select.Portal>
                                                <Select.Positioner className="select-positioner">
                                                    <Select.Popup className="select-popup">
                                                        {Array.from({ length: 25 }, (_, i) => (
                                                            <Select.Item key={i} className="select-item" value={String(i)}>
                                                                <Select.ItemText>{String(i === 24 ? 24 : i).padStart(2, '0')}:00</Select.ItemText>
                                                            </Select.Item>
                                                        ))}
                                                    </Select.Popup>
                                                </Select.Positioner>
                                            </Select.Portal>
                                        </Select.Root>
                                    </Field.Root>

                                    {estimatingEV && (
                                        <div style={{ gridColumn: '1 / -1', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                                            Analyzing recent energy history to estimate charging schedule...
                                        </div>
                                    )}

                                    {evEstimationNote && !estimatingEV && (
                                        <div style={{ gridColumn: '1 / -1', color: 'var(--success-color, #10b981)', fontSize: '0.85rem', fontWeight: 500 }}>
                                            ✓ {evEstimationNote}
                                        </div>
                                    )}

                                    {evEstimationError && !estimatingEV && (
                                        <div style={{ gridColumn: '1 / -1', color: 'var(--warning-color, #f59e0b)', fontSize: '0.85rem', fontWeight: 500 }}>
                                            ⚠️ {evEstimationError}
                                        </div>
                                    )}
                                </>
                            )}
                        </div>
                    </div>
                )}

                {!showAdvanced ? (
                    <div className="advanced-trigger-section">
                        <hr className="settings-separator" />
                        <button
                            type="button"
                            className="btn btn-secondary show-advanced-btn"
                            onClick={() => setShowAdvanced(true)}
                        >
                            Show Advanced Settings
                        </button>
                    </div>
                ) : (
                    <>
                        <div className="section-header">
                            <h3>Advanced Automation Overrides</h3>
                        </div>

                        <div className="grid-strategy-grid">
                            <Field.Root className="form-group">
                                <Field.Label htmlFor="bufferProfile">
                                    Overcharge Profile
                                    <HelpButton
                                        title="Overcharge Profile"
                                        description={
                                            <>
                                                <p>Determines how much battery headroom RateRudder includes beyond baseline requirement to absorb usage spikes or cloudy weather:</p>
                                                <ul>
                                                    <li><strong>Tiny:</strong> Maximizes immediate savings, but risks temporary depletion during load spikes.</li>
                                                    <li><strong>Default:</strong> Balanced optimization and safety buffer.</li>
                                                    <li><strong>Conservative:</strong> Holds higher energy reserves at the expense of lower savings.</li>
                                                </ul>
                                            </>
                                        }
                                    />
                                </Field.Label>
                                <Select.Root
                                    value={settings.socBufferPercent === 2 ? "tiny" : settings.socBufferPercent === 8 ? "conservative" : "default"}
                                    onValueChange={(val) => {
                                         if (val === 'tiny') {
                                             handleChange('socBufferPercent', 2);
                                             handleChange('peakSurvivalBufferMinutes', 10);
                                             handleChange('solarCapacityBufferMinutes', 0);
                                             handleChange('vppChargingBufferMinutes', 10);
                                         } else if (val === 'conservative') {
                                             handleChange('socBufferPercent', 8);
                                             handleChange('peakSurvivalBufferMinutes', 40);
                                             handleChange('solarCapacityBufferMinutes', 30);
                                             handleChange('vppChargingBufferMinutes', 40);
                                         } else {
                                             handleChange('socBufferPercent', 4);
                                             handleChange('peakSurvivalBufferMinutes', 20);
                                             handleChange('solarCapacityBufferMinutes', 10);
                                             handleChange('vppChargingBufferMinutes', 20);
                                         }
                                     }}
                                >
                                    <Select.Trigger className="select-trigger" id="bufferProfile" aria-label="Overcharge Profile">
                                        <Select.Value>
                                            {settings.socBufferPercent === 2 ? "Tiny" : settings.socBufferPercent === 8 ? "Conservative" : "Default"}
                                        </Select.Value>
                                        <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                            </svg>
                                        </Select.Icon>
                                    </Select.Trigger>
                                    <Select.Portal>
                                        <Select.Positioner className="select-positioner">
                                            <Select.Popup className="select-popup">
                                                <Select.Item className="select-item" value="tiny">
                                                    <Select.ItemText>Tiny</Select.ItemText>
                                                </Select.Item>
                                                <Select.Item className="select-item" value="default">
                                                    <Select.ItemText>Default</Select.ItemText>
                                                </Select.Item>
                                                <Select.Item className="select-item" value="conservative">
                                                    <Select.ItemText>Conservative</Select.ItemText>
                                                </Select.Item>
                                            </Select.Popup>
                                        </Select.Positioner>
                                    </Select.Portal>
                                </Select.Root>
                                <Field.Description>
                                    How much we over charge to handle unexpected solar/usage fluctuations.
                                </Field.Description>
                                {settings.socBufferPercent === 2 && (
                                    <div className="warning-text" style={{ color: 'orange', marginTop: '4px', fontSize: '0.9em' }}>
                                        Warning: A tiny profile may cause the battery to deplete unexpectedly during usage/solar fluctuations.
                                    </div>
                                )}
                                {settings.socBufferPercent === 8 && (
                                    <div className="warning-text" style={{ color: 'orange', marginTop: '4px', fontSize: '0.9em' }}>
                                        Warning: A conservative profile will reduce your financial savings by holding more energy in reserve.
                                    </div>
                                )}
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="alwaysChargeUnder">
                                    Always Charge Below ($/kWh)
                                    <HelpButton
                                        title="Always Charge Below Price"
                                        description="Sets an absolute electricity price threshold ($/kWh). Whenever the grid electricity rate drops below this price (e.g. during negative pricing or extreme off-peak rates), RateRudder will force-charge the battery regardless. Generally should not be used unless your utility has unpredictable rates."
                                    />
                                </Field.Label>
                                <Input
                                    id="alwaysChargeUnder"
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={settings.alwaysChargeUnderDollarsPerKWH}
                                    onChange={(e) => handleChange('alwaysChargeUnderDollarsPerKWH', parseFloat(e.target.value))}
                                />
                                <Field.Description>Charge battery whenever the price is less than this threshold.</Field.Description>
                                {settings.alwaysChargeUnderDollarsPerKWH > 0.05 && (
                                    <div className="warning-text" style={{ color: 'orange', marginTop: '4px', fontSize: '0.9em' }}>
                                        Are you sure you want to force charging the batteries from the grid when it's below this price?
                                    </div>
                                )}
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="minArbitrage">
                                    Minimum Arbitrage Profit ($/kWh)
                                    <HelpButton
                                        title="Minimum Arbitrage Profit"
                                        description="The minimum price difference ($/kWh) required between charging hours and later export hours to justify charging the battery from the grid for arbitrage."
                                    />
                                </Field.Label>
                                <Input
                                    id="minArbitrage"
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={settings.minArbitrageDifferenceDollarsPerKWH}
                                    onChange={(e) => handleChange('minArbitrageDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                                />
                                <Field.Description>Required profit margin to trigger immediate charging to later use/export at a higher prices.</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="minDeficit">
                                    Charge for Deficit ($/kWh)
                                    <HelpButton
                                        title="Charge for Deficit Threshold"
                                        description="The price difference threshold ($/kWh) required to justify pre-charging the battery from the grid when RateRudder forecasts that solar generation alone won't cover your upcoming home usage during peak rate hours."
                                    />
                                </Field.Label>
                                <Input
                                    id="minDeficit"
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={settings.minDeficitPriceDifferenceDollarsPerKWH}
                                    onChange={(e) => handleChange('minDeficitPriceDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                                />
                                <Field.Description>Price difference required to justify charging now to avoid a future battery depletion.</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="minExportHold">
                                    Standby for Similar Export Price ($/kWh)
                                    <HelpButton
                                        title="Standby for Similar Price Threshold"
                                        description="Hold the battery on standby during low-cost grid hours if the grid price is within this amount of the export credit. This avoids battery round-trip cycling losses when grid power is as cheap as the export credits you would forfeit by recharging."
                                    />
                                </Field.Label>
                                <Input
                                    id="minExportHold"
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={settings.minExportHoldDifferenceDollarsPerKWH}
                                    onChange={(e) => handleChange('minExportHoldDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                                />
                                <Field.Description>Max price difference to standby during cheap hours to export solar tomorrow.</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="minStartChargeMinutes">
                                    Minimum Start Charge Duration (minutes)
                                    <HelpButton
                                        title="Minimum Start Charge Duration"
                                        description="The minimum continuous duration (in minutes) of low-cost grid pricing required before starting a battery charge cycle. This prevents inefficient short-cycling of your battery system."
                                    />
                                </Field.Label>
                                <Input
                                    id="minStartChargeMinutes"
                                    type="number"
                                    step="1"
                                    min="1"
                                    value={settings.minStartChargeMinutes}
                                    onChange={(e) => handleChange('minStartChargeMinutes', parseInt(e.target.value, 10))}
                                />
                                <Field.Description>Minimum duration in minutes of charging time needed to start charging.</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="dryRun"
                                        checked={settings.dryRun}
                                        onCheckedChange={(checked) => handleChange('dryRun', checked)}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="dryRun">Dry Run Mode</Field.Label>
                                </div>
                                <Field.Description>Simulate actions without executing them (useful for testing).</Field.Description>
                            </Field.Root>
                        </div>

                        {!(settings.postalCode?.trim() && settings.gridExportSolar) && (
                            <>
                                <div className="section-header">
                                    <h3>Advanced Solar Settings</h3>
                                </div>
                                <div className="grid-strategy-grid">
                                    {!settings.postalCode?.trim() && (
                                        <>
                                            <Field.Root className="form-group">
                                                <Field.Label htmlFor="solarTrendRatioMax">
                                                    Solar Trend Ratio Max
                                                    <HelpButton
                                                        title="Solar Trend Ratio Max"
                                                        description="Limits the maximum scaling factor applied when real-time solar generation exceeds baseline model expectations. Higher values allow more aggressive upward solar predictions on clear, high-performing days."
                                                    />
                                                </Field.Label>
                                                <Input
                                                    id="solarTrendRatioMax"
                                                    type="number"
                                                    step="0.1"
                                                    min="1"
                                                    value={settings.solarTrendRatioMax}
                                                    onChange={(e) => handleChange('solarTrendRatioMax', parseFloat(e.target.value))}
                                                />
                                                <Field.Description>Maximum ratio for solar trend adjustment. Higher values allow more aggressive upward solar predictions.</Field.Description>
                                            </Field.Root>
                                            <Field.Root className="form-group">
                                                <Field.Label htmlFor="solarBellCurveMultiplier">
                                                    Solar Bell Curve Multiplier
                                                    <HelpButton
                                                        title="Solar Bell Curve Multiplier"
                                                        description="Controls the bell-curve smoothing factor for solar generation modeling across daylight hours. A value of 1 provides full bell-curve smoothing, while 0 disables model smoothing."
                                                    />
                                                </Field.Label>
                                                <Input
                                                    id="solarBellCurveMultiplier"
                                                    type="number"
                                                    step="0.1"
                                                    min="0"
                                                    max="1"
                                                    value={settings.solarBellCurveMultiplier}
                                                    onChange={(e) => handleChange('solarBellCurveMultiplier', parseFloat(e.target.value))}
                                                />
                                                <Field.Description>Multiplier for bell curve solar smoothing. 0 disables smoothing entirely</Field.Description>
                                            </Field.Root>
                                        </>
                                    )}

                                    {!settings.gridExportSolar && (
                                        <Field.Root className="form-group">
                                            <Field.Label htmlFor="solarFullyChargeHeadroomBatterySOC">
                                                Solar Fully Charge Headroom (%)
                                                <HelpButton
                                                    title="Solar Fully Charge Headroom"
                                                    description="Percentage of battery capacity left open as headroom during solar charging when grid export is disabled, preventing premature solar power curtailment."
                                                />
                                            </Field.Label>
                                            <Input
                                                id="solarFullyChargeHeadroomBatterySOC"
                                                type="number"
                                                step="1"
                                                value={settings.solarFullyChargeHeadroomBatterySOC}
                                                onChange={(e) => handleChange('solarFullyChargeHeadroomBatterySOC', parseFloat(e.target.value))}
                                            />
                                            <Field.Description>
                                                Battery percentage to leave as headroom during solar charging when export is disabled. Negative values will remove the headroom and ignore solar curtailment.
                                            </Field.Description>
                                        </Field.Root>
                                    )}

                                    {(settings.utilityRateOptions?.netMeteringCredits || settings.utilityRateOptions?.netMeteringScheme === 'net') && (
                                        <Field.Root className="form-group">
                                            <Field.Label htmlFor="solarNetMeteringCreditsValue">
                                                Solar Net Metering Credits Value
                                                <HelpButton
                                                    title="Solar Net Metering Credits Value"
                                                    description={
                                                        <>
                                                            <p>Specifies how your utility values exported solar energy credits:</p>
                                                            <ul>
                                                                <li><strong>Lowest / Default:</strong> Values exported energy at the lowest rate of the day.</li>
                                                                <li><strong>Highest:</strong> Values credits at peak rates of the day.</li>
                                                                <li><strong>None:</strong> Treats exported solar energy as uncredited.</li>
                                                            </ul>
                                                        </>
                                                    }
                                                />
                                            </Field.Label>
                                            <Select.Root
                                                value={settings.solarNetMeteringCreditsValue || ""}
                                                onValueChange={(value) => handleChange('solarNetMeteringCreditsValue', value || '')}
                                            >
                                                <Select.Trigger className="select-trigger" id="solarNetMeteringCreditsValue">
                                                    <Select.Value>
                                                        {
                                                            settings.solarNetMeteringCreditsValue === 'highest' ? 'Highest Price' :
                                                            settings.solarNetMeteringCreditsValue === 'none' ? 'None' :
                                                            settings.solarNetMeteringCreditsValue === 'lowest' ? 'Lowest Price' :
                                                            'Lowest / Default'
                                                        }
                                                    </Select.Value>
                                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                        </svg>
                                                    </Select.Icon>
                                                </Select.Trigger>
                                                <Select.Portal>
                                                    <Select.Positioner className="select-positioner">
                                                        <Select.Popup className="select-popup">
                                                            <Select.Item className="select-item" value="">
                                                                <Select.ItemText>Lowest / Default</Select.ItemText>
                                                            </Select.Item>
                                                            <Select.Item className="select-item" value="lowest">
                                                                <Select.ItemText>Lowest Price</Select.ItemText>
                                                            </Select.Item>
                                                            <Select.Item className="select-item" value="highest">
                                                                <Select.ItemText>Highest Price</Select.ItemText>
                                                            </Select.Item>
                                                            <Select.Item className="select-item" value="none">
                                                                <Select.ItemText>None</Select.ItemText>
                                                            </Select.Item>
                                                        </Select.Popup>
                                                    </Select.Positioner>
                                                </Select.Portal>
                                            </Select.Root>
                                            <Field.Description>
                                                How to value exported solar credits. "Lowest" price of the day, "Highest" price of the day, or value them as nothing.
                                            </Field.Description>
                                        </Field.Root>
                                    )}
                                </div>
                            </>
                        )}

                        <div className="section-header">
                            <h3>Advanced Home Usage Settings</h3>
                        </div>
                        <div className="grid-strategy-grid">
                            {/*
                            <Field.Root className="form-group">
                                <Field.Label htmlFor="ignoreHourUsageOverMultiple">Ignore Usage Outlier Multiple</Field.Label>
                                <Input
                                    id="ignoreHourUsageOverMultiple"
                                    type="number"
                                    step="0.1"
                                    min="1"
                                    value={settings.ignoreHourUsageOverMultiple}
                                    onChange={(e) => handleChange('ignoreHourUsageOverMultiple', parseFloat(e.target.value))}
                                />
                                <Field.Description>If a single hour's usage is this many times greater than the average of other data points for that hour, ignore it. Must be &ge; 1.</Field.Description>
                            </Field.Root>
                            */}

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="homeLoadPredictionStrategy">
                                    Home Load Prediction Strategy
                                    <HelpButton
                                        title="Home Load Prediction Strategy"
                                        description={
                                            <>
                                                <p>Controls how conservative RateRudder is when forecasting future household electricity consumption:</p>
                                                <ul>
                                                    <li><strong>Default (Responsive/Optimized):</strong> Optimizes for maximum financial savings based on historical averages.</li>
                                                    <li><strong>Conservative (High Protection):</strong> Over-predicts consumption to ensure higher battery reserves during heavy household usage.</li>
                                                </ul>
                                            </>
                                        }
                                    />
                                </Field.Label>
                                <Select.Root
                                    value={settings.homeLoadPredictionStrategy || 'default'}
                                    onValueChange={(value) => handleChange('homeLoadPredictionStrategy', value || 'default')}
                                >
                                    <Select.Trigger className="select-trigger" id="homeLoadPredictionStrategy" aria-label="Home Load Prediction Strategy">
                                        <Select.Value>
                                            {settings.homeLoadPredictionStrategy === 'conservative'
                                                ? 'Conservative (High Protection)'
                                                : 'Default (Responsive/Optimized)'}
                                        </Select.Value>
                                        <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                            </svg>
                                        </Select.Icon>
                                    </Select.Trigger>
                                    <Select.Portal>
                                        <Select.Positioner className="select-positioner">
                                            <Select.Popup className="select-popup">
                                                <Select.Item className="select-item" value="default">
                                                    <Select.ItemText>Default (Responsive/Optimized)</Select.ItemText>
                                                </Select.Item>
                                                <Select.Item className="select-item" value="conservative">
                                                    <Select.ItemText>Conservative (High Protection)</Select.ItemText>
                                                </Select.Item>
                                            </Select.Popup>
                                        </Select.Positioner>
                                    </Select.Portal>
                                </Select.Root>
                                <Field.Description>How conservative the system is when predicting future home load.</Field.Description>
                                {settings.homeLoadPredictionStrategy === 'conservative' && (
                                    <div className="warning-notice" style={{ marginTop: '0.5rem' }} data-testid="conservative-strategy-warning">
                                        Caution: Conservative mode will result in more grid charging and higher electricity costs, but reduces the chance of running out of battery during periodic higher usage.
                                    </div>
                                )}
                            </Field.Root>
                        </div>
                    </>
                )}

                <div className="submit-section">
                    {error && <div className="error-message">{error}</div>}
                    {successMessage && <div className="success-message">{successMessage}</div>}
                    {!(editESS && maxStage > currentStage) && (
                        <button type="submit" className="save-button" disabled={isSaving}>
                            {isSaving && <span className="loading-spinner" aria-hidden="true"></span>}
                            {isSaving ? 'Saving...' : 'Save Settings'}
                        </button>
                    )}
                </div>
            </form>

            {/* Pause Automation Section right above Danger Zone */}
            <div className="settings-section" data-testid="pause-section" style={{ marginTop: '2rem' }}>
                <div className="section-header">
                    <h3>Pause Automation</h3>
                </div>
                <div className="pause-section-content" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
                    <p className="section-description" style={{ margin: 0, flex: 1 }}>
                        {settings.pause
                            ? "Automation is currently paused. The system will continue monitoring but will not change battery or solar modes."
                            : "If enabled, stop changing states but continue monitoring."}
                    </p>
                    <button
                        type="button"
                        className={`btn ${settings.pause ? 'btn-primary' : 'btn-secondary'}`}
                        id="pauseAutomationBtn"
                        onClick={handleTogglePause}
                        disabled={isSaving}
                        style={{ minWidth: '100px' }}
                    >
                        {settings.pause ? 'Resume' : 'Pause'}
                    </button>
                </div>
            </div>

            <div className="settings-section delete-site-section" data-testid="delete-section">
                <div className="section-header">
                    <h3>Danger Zone</h3>
                </div>
                <div className="danger-zone-content">
                    <p className="section-description">Deleting this site will stop all automation and delete all associated data permanently.</p>
                    <Dialog.Root open={isDeleteDialogOpen} onOpenChange={setIsDeleteDialogOpen}>
                        <Dialog.Trigger className="btn btn-danger" type="button">
                            Delete Site
                        </Dialog.Trigger>
                        <Dialog.Portal>
                            <Dialog.Backdrop className="dialog-backdrop" />
                            <Dialog.Popup className="dialog-popup">
                                <Dialog.Title className="dialog-title">Delete Site</Dialog.Title>
                                <Dialog.Description className="dialog-description">
                                    Are you sure you want to delete this site? All of the data is irrecoverable and your automation will stop.
                                </Dialog.Description>

                                <div className="delete-account-wrapper">
                                    <Field.Root className={`form-group switch-group compact ${!isLastSite ? 'disabled' : ''}`}>
                                        <div className="switch-row" title={!isLastSite ? "All sites must be deleted first" : undefined}>
                                            <Switch.Root
                                                id="deleteAccount"
                                                checked={deleteAccountChecked}
                                                onCheckedChange={setDeleteAccountChecked}
                                                disabled={!isLastSite}
                                                className="switch-root"
                                            >
                                                <Switch.Thumb className="switch-thumb" />
                                            </Switch.Root>
                                            <Field.Label htmlFor="deleteAccount" style={{ cursor: isLastSite ? 'pointer' : 'not-allowed' }}>
                                                Delete Account
                                            </Field.Label>
                                        </div>
                                        <Field.Description>
                                            {!isLastSite
                                                ? "All sites must be deleted first."
                                                : "Also completely delete your user account."}
                                        </Field.Description>
                                    </Field.Root>
                                </div>

                                {deleteError && <div className="delete-dialog-error">{deleteError}</div>}

                                <div className="delete-dialog-buttons">
                                    <Dialog.Close className="btn btn-secondary" type="button" disabled={isDeleting}>
                                        Cancel
                                    </Dialog.Close>
                                    <button
                                        type="button"
                                        className="btn btn-danger"
                                        onClick={handleConfirmDelete}
                                        disabled={isDeleting}
                                    >
                                        {isDeleting && <span className="loading-spinner" aria-hidden="true" style={{ marginRight: '0.5rem' }}></span>}
                                        {isDeleting ? 'Deleting...' : 'Delete'}
                                    </button>
                                </div>
                            </Dialog.Popup>
                        </Dialog.Portal>
                    </Dialog.Root>
                </div>
            </div>

            {isDirty && !isInWizard && (
                <div className="unsaved-changes-bar" data-testid="unsaved-changes-bar">
                    <div className="unsaved-changes-content">
                        <div className="unsaved-changes-text">
                            <span className="unsaved-dot" aria-hidden="true" />
                            <span>You have unsaved changes</span>
                        </div>
                        <div className="unsaved-changes-actions">
                            <button
                                type="button"
                                className="btn btn-secondary"
                                disabled={isSaving}
                                onClick={handleDiscard}
                            >
                                Discard
                            </button>
                            <button
                                type="submit"
                                form="settings-form"
                                className="btn btn-primary"
                                disabled={isSaving}
                            >
                                {isSaving && <span className="loading-spinner" aria-hidden="true" style={{ marginRight: '0.5rem' }}></span>}
                                {isSaving ? 'Saving...' : 'Save Changes'}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {renderTeslaGridModal()}
        </div>
    );
};
export default Settings;
