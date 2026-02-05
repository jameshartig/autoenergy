export interface Action {
    timestamp: string;
    batteryMode: number;
    solarMode: number;
    description: string;
    currentPrice?: {
        tsStart: string;
        tsEnd: string;
        dollarsPerKWH: number;
    };
    systemStatus?: any;
    dryRun?: boolean;
}

export const BatteryMode = {
    NoChange: 0,
    Standby: 1,
    ChargeAny: 2,
    ChargeSolar: 3,
    Load: -1,
} as const;

export type BatteryMode = typeof BatteryMode[keyof typeof BatteryMode];

export const SolarMode = {
    NoChange: 0,
    NoExport: 1,
    Any: 2,
} as const;

export type SolarMode = typeof SolarMode[keyof typeof SolarMode];

export const fetchActions = async (start: Date, end: Date): Promise<Action[]|null> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const response = await fetch(`/api/history/actions?start=${startStr}&end=${endStr}`);
    if (!response.ok) {
        throw new Error('Failed to fetch actions');
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

export const fetchSavings = async (start: Date, end: Date): Promise<SavingsStats|null> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const response = await fetch(`/api/history/savings?start=${startStr}&end=${endStr}`);
    if (!response.ok) {
        throw new Error('Failed to fetch savings');
    }
    return response.json();
};

export interface Settings {
    dryRun: boolean;
    pause: boolean;
    alwaysChargeUnderDollarsPerKWH: number;
    additionalFeesDollarsPerKWH: number;
    minArbitrageDifferenceDollarsPerKWH: number;
    minBatterySOC: number;
    ignoreHourUsageOverMultiple: number;
    gridChargeBatteries: boolean;
    gridExportSolar: boolean;
}

export const fetchSettings = async (): Promise<Settings> => {
    const response = await fetch('/api/settings');
    if (!response.ok) {
        throw new Error('Failed to fetch settings');
    }
    return response.json();
};

export const updateSettings = async (settings: Settings): Promise<void> => {
    const response = await fetch('/api/settings', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(settings),
    });
    if (!response.ok) {
        throw new Error('Failed to update settings');
    }
};

export interface AuthStatus {
    isAdmin: boolean;
    loggedIn: boolean;
    email: string;
    authRequired: boolean;
    clientID: string;
}

export const fetchAuthStatus = async (): Promise<AuthStatus> => {
    const response = await fetch('/api/auth/status');
    if (!response.ok) {
        throw new Error('Failed to fetch auth status');
    }
    return response.json();
};

export const login = async (token: string): Promise<void> => {
    const response = await fetch('/api/auth/login', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ token }),
    });
    if (!response.ok) {
        throw new Error('Login failed');
    }
};

export const logout = async (): Promise<void> => {
    const response = await fetch('/api/auth/logout', {
        method: 'POST',
    });
    if (!response.ok) {
        throw new Error('Logout failed');
    }
};
