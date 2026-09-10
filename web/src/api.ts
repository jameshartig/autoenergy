export interface SystemAlarm {
    name: string;
    description: string;
    timestamp: string;
    code: string;
}

export interface SystemStorm {
    description: string;
    tsStart: string;
    tsEnd: string;
}

export interface SystemStatus {
    alarms?: SystemAlarm[];
    storms?: SystemStorm[];
    // Add other fields from backend if useful, but alarms is what we need now
    [key: string]: any;
}

export const ActionReason = {
    AlwaysChargeBelowThreshold: 'alwaysChargeBelowThreshold',
    MissingBattery: 'missingBattery',
    DeficitCharge: 'deficitCharge',
    ArbitrageChargeExport: 'arbitrageChargeExport',
    ArbitrageChargeSave: 'arbitrageChargeSave',
    DischargeBeforeCapacity: 'dischargeBeforeCapacity',
    DeficitSaveForPeak: 'deficitSaveForPeak',
    ArbitrageSave: 'dischargeAtPeak',
    SufficientBattery: 'sufficientBattery',
    SufficientBatteryTillCharge: 'sufficientBatteryTillCharge',
    EmergencyMode: 'emergencyMode',
    HasAlarms: 'hasAlarms',
    GridUnavailable: 'gridUnavailable',
    WaitingToCharge: 'waitingToCharge',
    PreventSolarCurtailment: 'preventSolarCurtailment',
    HoldSimilarPrice: 'holdSimilarPrice',
    BatteryAtReserve: 'batteryAtReserve',
    VPPActive: 'vppActive',
    VPPPrep: 'vppPrep',
    EVChargingStandby: 'evChargingStandby',
    ArbitrageHoldExport: 'arbitrageHoldExport',
    ArbitrageHoldSave: 'arbitrageHoldSave',
    // deprecated - but we don't delete them because old actions still have them
    DeficitSave: 'deficitSave',
    ArbitrageCharge: 'arbitrageCharge',
    ArbitrageHold: 'arbitrageHold',
} as const;

export type ActionReason = typeof ActionReason[keyof typeof ActionReason];

export interface PriceInfo {
    tsStart: string;
    tsEnd: string;
    dollarsPerKWH: number;
    gridUseDollarsPerKWH: number; // delivery adder; true grid charge cost = dollarsPerKWH + gridUseDollarsPerKWH
}

export interface SimulationParams {
    clippingCapKWH: number;
    panelAzimuth: number;
    panelTilt: number;
    averageSolarEfficiency: number;
}

export interface Action {
    timestamp: string;
    systemTimestamp?: string;
    batteryMode: number;
    solarMode: number;
    targetBatteryMode?: number;
    targetSolarMode?: number;
    reason?: ActionReason;
    description: string;
    currentPrice?: PriceInfo;
    futurePrice?: PriceInfo;
    deficitAt?: string;
    hitBufferedDeficitAt?: string;
    hitThresholdDeficitAt?: string;
    capacityAt?: string;
    systemStatus?: SystemStatus;
    dryRun?: boolean;
    fault?: boolean;
    paused?: boolean;
    simulationParams?: SimulationParams;
}

export const BatteryMode = {
    NoChange: 0,
    Standby: 1,
    ChargeAny: 2,
    Load: -1,
} as const;

export type BatteryMode = typeof BatteryMode[keyof typeof BatteryMode];

export const SolarMode = {
    NoChange: 0,
    NoExport: 1,
    Any: 2,
} as const;

export type SolarMode = typeof SolarMode[keyof typeof SolarMode];

async function extractError(response: Response, fallback: string): Promise<string> {
    if (response.status === 401 && !response.url.includes('/api/auth/status')) {
        try {
            const statusRes = await fetch('/api/auth/status');
            if (statusRes.ok) {
                const status = await statusRes.json();
                if (!status.loggedIn) {
                    const from = encodeURIComponent(window.location.pathname + window.location.search);
                    window.location.href = `/login?from=${from}`;
                }
            }
        } catch { /* ignore */ }
    }

    try {
        const body = await response.json();
        if (body && typeof body.error === 'string') {
            return body.error;
        }
    } catch { /* ignore parse failures */ }
    return fallback;
}

export const fetchActions = async (start: Date, end: Date, siteID?: string): Promise<Action[]|null> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const query = new URLSearchParams({
        start: startStr,
        end: endStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/history/actions?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch actions'));
    }
    return response.json();
};

export interface SavingsStats {
    timestamp: string;
    cost: number;
    credit: number;
    batterySavings: number;
    solarSavings: number;
    avoidedCost: number;
    chargingCost: number;
    solarGenerated: number;
    gridImported: number;
    gridExported: number;
    homeUsed: number;
    batteryUsed: number;
}

export const fetchSavings = async (start: Date, end: Date, siteID?: string): Promise<SavingsStats|null> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const query = new URLSearchParams({
        start: startStr,
        end: endStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/history/savings?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch savings'));
    }
    return response.json();
};

export interface UtilityRateOptions {
    generationRate?: string;
    location?: string;
    [key: string]: any;
}

export interface UtilityOptionChoice {
    value: string;
    name: string;
}

export type UtilityOptionType = 'select' | 'switch';

export interface UtilityRateOption {
    field: string;
    name: string;
    type: UtilityOptionType;
    description?: string;
    choices?: UtilityOptionChoice[];
    default?: any;
    hidden?: boolean;
}

export interface UtilityProviderInfo {
  id: string;
  name: string;
  rates?: UtilityRateInfo[];
  hidden?: boolean;
}

export interface UtilityRateInfo {
  id: string;
  name: string;
  options?: UtilityRateOption[];
}

export interface ESSCredentialFieldChoice {
    value: string;
    name: string;
}

export type ESSCredentialFieldType = 'select' | 'string' | 'password';

export interface ESSCredentialField {
  field: string;
  name: string;
  type: ESSCredentialFieldType;
  required: boolean;
  description?: string;
  choices?: ESSCredentialFieldChoice[];
  default?: any;
  stage?: number;
  hidden?: boolean;
  oneTime?: boolean;
}

export interface ESSProviderInfo {
  id: string;
  name: string;
  credentials?: ESSCredentialField[];
  oAuthURLs?: Record<string, string>;
  oAuthKey?: ESSCredentialField;
  hidden?: boolean;
}

export interface Settings {
    dryRun: boolean;
    pause: boolean;
    countryCode: string;
    postalCode: string;
    release: string;
    alwaysChargeUnderDollarsPerKWH: number;
    minArbitrageDifferenceDollarsPerKWH: number;
    minDeficitPriceDifferenceDollarsPerKWH: number;
    minExportHoldDifferenceDollarsPerKWH: number;
    minBatterySOC: number;
    minBatterySOCPeriods?: MinBatterySOCPeriod[];
    evChargingPeriods?: TimePeriod[];
    ignoreHourUsageOverMultiple: number;
    customGridSettings?: boolean;
    gridChargeBatteries: boolean;
    gridExportSolar: boolean;
    gridExportBatteries: boolean;
    solarTrendRatioMax: number;
    solarBellCurveMultiplier: number;
    solarFullyChargeHeadroomBatterySOC: number;
    solarNetMeteringCreditsValue: string;
    utilityProvider: string;
    utilityRate: string;
    utilityRateOptions: UtilityRateOptions;
    ess: string;
    hasCredentials: {
        [key: string]: boolean;
    };
    essAuthStatus?: {
        consecutiveFailures: number;
        lastAttempt: string;
    };
    solarAzimuth?: number;
    solarTilt?: number;
    minStartChargeMinutes: number;
    peakSurvivalBufferMinutes: number;
    socBufferPercent: number;
    solarCapacityBufferMinutes: number;
    vppChargingBufferMinutes: number;
    homeLoadPredictionStrategy?: string;
}

export interface UtilityHourPeriod {
    hourStart: number;
    minuteStart?: number;
    hourEnd: number;
    minuteEnd?: number;
}

export interface TimePeriod {
    name?: string;
    start?: string;
    end?: string;
    hours?: UtilityHourPeriod[];
    daysOfTheWeek?: number[];
    specificDates?: string[];
}

export interface MinBatterySOCPeriod extends TimePeriod {
    minBatterySOC: number;
    utilityPeriodName?: string;
}

export const fetchUtilityPeriods = async (
    siteID?: string,
    utilityProvider?: string,
    utilityRate?: string,
    utilityRateOptions?: Record<string, any>
): Promise<TimePeriod[] | null> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    if (utilityProvider) {
        query.append('utilityProvider', utilityProvider);
    }
    if (utilityRate) {
        query.append('utilityRate', utilityRate);
    }
    if (utilityRateOptions && Object.keys(utilityRateOptions).length > 0) {
        query.append('utilityRateOptions', JSON.stringify(utilityRateOptions));
    }
    const response = await fetch(`/api/utility/periods${query.toString() ? `?${query.toString()}` : ''}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch utility periods'));
    }
    return response.json();
};

export interface FranklinCredentials {
    username: string;
    md5Password: string;
    gatewayID: string;
}

export type CredentialsPayload = Record<string, Record<string, any>>;

export interface SettingsUpdate {
    settings: Settings;
    credentials?: CredentialsPayload;
    siteID?: string;
}

export const fetchSettings = async (siteID?: string): Promise<Settings> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/settings?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch settings'));
    }
    return response.json();
};

export const updateSettings = async (settings: Settings, siteID?: string, credentials?: CredentialsPayload): Promise<void> => {
    const payload: any = {
        ...settings,
        siteID: siteID,
    };

    if (siteID) {
        payload.siteID = siteID;
    }

    if (credentials) {
        payload.credentials = credentials;
    }

    const response = await fetch('/api/settings', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to update settings'));
    }
};

export interface Site {
    id: string;
}

export interface UserSite extends Site {
    name: string;
}

export interface AuthStatus {
    loggedIn: boolean;
    email: string;
    authRequired: boolean;
    clientIDs: Record<string, string>;
    sites?: UserSite[];
}

export const fetchAuthStatus = async (): Promise<AuthStatus> => {
    const response = await fetch('/api/auth/status');
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch auth status'));
    }
    return response.json();
};

export const login = async (token: string, client?: string): Promise<void> => {
    const response = await fetch('/api/auth/login', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ token, client }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Login failed'));
    }
};

export const logout = async (): Promise<void> => {
    const response = await fetch('/api/auth/logout', {
        method: 'POST',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Logout failed'));
    }
};

export const deleteSite = async (siteID: string): Promise<void> => {
    const response = await fetch('/api/delete/site', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to delete site'));
    }
};

export const deleteUser = async (): Promise<void> => {
    const response = await fetch('/api/delete/user', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to delete user'));
    }
};

export interface AdminSite extends Site {
    lastAction?: Action;
    alias?: string;
}

export const listSites = async (): Promise<AdminSite[]> => {
    const response = await fetch('/api/list/sites', {
        method: 'GET',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to list sites'));
    }
    return response.json();
};

export const setSiteAlias = async (siteID: string, alias: string): Promise<void> => {
    const response = await fetch('/api/site/alias', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID, alias }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to update site alias'));
    }
};

export const listUserSites = async (): Promise<UserSite[]> => {
    const response = await fetch('/api/list/userSites', {
        method: 'GET',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to list user sites'));
    }
    return response.json();
};

export interface ModelingHour {
    ts: string;
    hour: number;
    netLoadSolarKWH: number;
    gridChargeDollarsPerKWH: number;
    solarOppDollarsPerKWH: number;
    avgHomeLoadKWH: number;
    predictedSolarKWH: number;
    batteryKWH: number;
    startBatteryKWH?: number;
    batteryCapacityKWH: number;
    batteryReserveKWH: number;
    todaySolarTrend: number;
    startedVPPChargingAt?: string;
    vppStandbyAt?: string;
    vppEndAt?: string;
}

export const fetchModeling = async (siteID?: string, overrideHomeLoadPredictionStrategy?: string): Promise<ForecastResponse> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    if (overrideHomeLoadPredictionStrategy) {
        query.append('overrideHomeLoadPredictionStrategy', overrideHomeLoadPredictionStrategy);
    }
    const response = await fetch(`/api/forecast?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch modeling data'));
    }
    return response.json();
};

export const joinSite = async (joinSiteID: string, inviteCode: string, name: string): Promise<string> => {
    const response = await fetch('/api/join', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ joinSiteID, inviteCode, name }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to join site'));
    }
    const data = await response.json();
    return data.siteID;
};

export const createSite = async (name: string): Promise<string> => {
    const response = await fetch('/api/join', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ create: true, name }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to create site'));
    }
    const data = await response.json();
    return data.siteID;
};

export const fetchUtilities = async (siteID?: string): Promise<UtilityProviderInfo[]> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/list/utilities?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch utilities'));
    }
    return response.json();
};

export const fetchESSList = async (siteID?: string): Promise<ESSProviderInfo[]> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/list/ess?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch ESS systems'));
    }
    return response.json();
};

export interface Feedback {
    id: string;
    sentiment: string;
    comment: string;
    siteID: string;
    userID: string;
    extra: Record<string, string>;
    timestamp: string;
}

export async function submitFeedback(siteID: string, sentiment: string, comment: string, extra: Record<string, string>): Promise<void> {
    const response = await fetch('/api/feedback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ siteID, sentiment, comment, extra })
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to submit feedback'));
    }
}

export async function listFeedback(limit?: number, lastFeedbackID?: string): Promise<Feedback[]> {
    let url = '/api/list/feedback?';
    if (limit !== undefined) {
        url += `limit=${limit}&`;
    }
    if (lastFeedbackID) {
        url += `lastFeedbackID=${lastFeedbackID}`;
    }
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch feedback'));
    }
    return response.json();
}

export interface InterestSubmission {
    email: string;
    utility: string;
    battery: string;
    utilityProviderName: string;
    state: string;
    planName: string;
    batteryName: string;
    comments: string;
    timestamp: string;
}

export async function submitInterest(submission: Omit<InterestSubmission, 'email' | 'timestamp'>): Promise<void> {
    const response = await fetch('/api/interest', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(submission)
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to submit interest'));
    }
}

export async function listInterest(limit?: number): Promise<InterestSubmission[]> {
    let url = '/api/list/interest?';
    if (limit !== undefined) {
        url += `limit=${limit}`;
    }
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch interest submissions'));
    }
    return response.json();
}

export interface EnergyHistoryRes {
    tsHourStart: string;
    avgBatterySOC: number;
    solarKWH: number;
}

export interface PriceHistoryRes {
    tsHourStart: string;
    dollarsPerKWH: number;
    gridUseDollarsPerKWH: number;
}

export interface WeatherRes {
    tsHourStart: string;
    irradiance: number;
    temperatureC?: number;
    temperatureCellC?: number;
    snowfallCM?: number;
    improvedSolarGeneration?: number;
    unclippedSolarGeneration?: number;
    improvedHomeLoad?: number;
    snowDepthCM?: number;
    tempFactor?: number;
    snowFactor?: number;
}

export interface ForecastResponse {
    simulation: ModelingHour[];
    energyHistory: EnergyHistoryRes[];
    priceHistory: PriceHistoryRes[];
    weather: WeatherRes[];
    solar1hForecast?: WeatherRes[];
    updated?: string;
}
export interface EnergyStats {
    tsHourStart: string;
    minBatterySOC: number;
    maxBatterySOC: number;
    batteryChargedKWH: number;
    batteryUsedKWH: number;
    solarKWH: number;
    homeKWH: number;
    gridExportKWH: number;
    gridImportKWH: number;
    vppExportKWH?: number;
    batteryToHomeKWH: number;
    solarToHomeKWH: number;
    solarToBatteryKWH: number;
    solarToGridKWH: number;
    batteryToGridKWH: number;
    alarms?: SystemAlarm[];
}

export interface HistoryEnergyResponse {
    energy: EnergyStats[];
    weather: WeatherRes[];
}

export const fetchHistoryEnergy = async (date: Date, siteID?: string): Promise<HistoryEnergyResponse> => {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const dateStr = `${year}-${month}-${day}`;

    const query = new URLSearchParams({
        date: dateStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/history/energy?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch energy history'));
    }
    return response.json();
};

export const submitESSStage = async (ess: string, credentials: Record<string, any>, siteID?: string): Promise<void> => {
    const payload: any = {
        ess,
        credentials: {
            [ess]: credentials,
        },
    };
    if (siteID) {
        payload.siteID = siteID;
    }
    const response = await fetch('/api/ess/stage', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to trigger next stage'));
    }
};

export interface ActionsAndSavingsResponse {
    actions: Action[];
    savings: SavingsStats;
}

export const fetchActionsAndSavings = async (start: Date, end: Date, siteID?: string): Promise<ActionsAndSavingsResponse> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const query = new URLSearchParams({
        start: startStr,
        end: endStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/history/actionsAndSavings?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch actions and savings'));
    }
    return response.json();
};

export interface EVSession {
    tsStartHour: string;
    tsEndHour: string;
    durationHr: number;
    peakKW: number;
    avgKW: number;
    totalKWH: number;
    netStepKW: number;
}

export interface EVDetectionResult {
    detected: boolean;
    recommendedPeriod?: TimePeriod;
    allDetectedPeriods?: TimePeriod[];
    estimatedRateKW?: number;
    sessionsCount?: number;
    sessions?: EVSession[];
    message?: string;
}

export const fetchEstimateEVCharging = async (siteID?: string): Promise<EVDetectionResult> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const queryString = query.toString() ? `?${query.toString()}` : '';
    const response = await fetch(`/api/history/estimateEVCharging${queryString}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to estimate EV charging'));
    }
    return response.json();
};

export type MorningSummaryFlavor = 'metrics_heavy' | 'home_planner' | 'executive' | 'pilot';
export type EveningSummaryFlavor = 'metrics_heavy' | 'home_planner' | 'executive' | 'pilot';
export type AnomalyAlertSensitivity = '' | 'disabled' | 'low' | 'medium' | 'high';

export interface UserNotificationSettings {
    morningSummaryEnabled: boolean;
    morningSummaryHour: number;
    morningSummaryFlavor: MorningSummaryFlavor;
    eveningSummaryEnabled?: boolean;
    eveningSummaryHour?: number;
    eveningSummaryFlavor?: EveningSummaryFlavor;
    gridOutageAlert?: boolean;
    priceSpikeAlert?: AnomalyAlertSensitivity;
    solarUnderproductionAlert?: AnomalyAlertSensitivity;
    vppDispatchAlert?: boolean;
}

export interface PushSubscriptionKeys {
    p256dh: string;
    auth: string;
}

export interface PushSubscription {
    id?: string;
    endpoint: string;
    keys: PushSubscriptionKeys;
    userAgent?: string;
    tsCreated?: string;
}

export interface GetNotificationSettingsResponse {
    settings: UserNotificationSettings;
    subscriptions: PushSubscription[];
    vapidEnabled: boolean;
}

export interface NotificationLog {
    id: string;
    tsCreated: string;
    userID: string;
    endpoint: string;
    type: string;
    flavor: string;
    title: string;
    body: string;
    success: boolean;
    statusCode: number;
    error?: string;
    clicked?: boolean;
    tsClicked?: string;
    metadata?: Record<string, string>;
}

export interface MonthlyNotificationLogs {
    tsMonthStart: string;
    logs: NotificationLog[];
}

export const fetchVAPIDPublicKey = async (): Promise<ArrayBuffer> => {
    const response = await fetch('/api/notifications/vapidPublicKey');
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch VAPID public key'));
    }
    return await response.arrayBuffer();
};

export const fetchNotificationSettings = async (siteID?: string): Promise<GetNotificationSettingsResponse> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const queryString = query.toString() ? `?${query.toString()}` : '';
    const response = await fetch(`/api/notifications/settings${queryString}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch notification settings'));
    }
    return response.json();
};

export const subscribePushNotification = async (subscription: PushSubscription, sendTest: boolean = false): Promise<void> => {
    const response = await fetch('/api/notifications/subscribe', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ subscription, sendTest }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to register push subscription'));
    }
};

export const unsubscribePushNotification = async (endpoint: string): Promise<void> => {
    const response = await fetch('/api/notifications/unsubscribe', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ endpoint }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to remove push subscription'));
    }
};

export const updateNotificationSettings = async (siteID: string, settings: UserNotificationSettings): Promise<void> => {
    const response = await fetch('/api/notifications/settings', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID, settings }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to update notification settings'));
    }
};


