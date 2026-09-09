import React from 'react';
import { Link, useLocation, useSearchParams } from 'wouter';
import { Select } from '@base-ui/react/select';
import { NotificationModal } from './NotificationModal';
import { isIOSHomeScreen } from '../utils/pwaUtils';
import './Header.css';
import type { UserSite } from '../api';

interface HeaderProps {
    loggedIn: boolean;
    sites: UserSite[];
    selectedSiteID: string;
    onSiteChange: (siteID: string) => void;
    onLogout: () => void;
}

const Header: React.FC<HeaderProps> = ({ loggedIn, sites, selectedSiteID, onSiteChange, onLogout }) => {
    const [isMenuOpen, setIsMenuOpen] = React.useState(false);
    const [isNotificationModalOpen, setIsNotificationModalOpen] = React.useState(false);
    const [location] = useLocation();
    const [searchParams] = useSearchParams();

    const showNotifications = searchParams.get('notifications') === 'true' || isIOSHomeScreen();

    const currentSite = sites.find(s => s.id === selectedSiteID) || (sites.length > 0 ? sites[0] : undefined);
    const activeSiteID = currentSite?.id;
    const activeSiteName = currentSite?.name || currentSite?.id;

    const toggleMenu = () => {
        setIsMenuOpen(!isMenuOpen);
    };

    return (
        <header className={`raterudder-header ${loggedIn ? 'logged-in' : 'logged-out'}`}>
            <div className="content-container header-inner">
                <div className="header-left">
                    <Link to="/" className="brand-logo" onClick={() => setIsMenuOpen(false)}>
                        <img src="/logo.svg" alt="RateRudder Logo" className="header-logo-img" />
                        RateRudder
                    </Link>
                    {loggedIn && sites.length > 1 && (
                        <Select.Root
                            value={selectedSiteID}
                            items={{
                                ...Object.fromEntries(sites.map(site => [site.id, site.name || site.id])),
                                "ALL": "Overview"
                            }}
                            onValueChange={(value) => onSiteChange(value as string)}
                        >
                            <Select.Trigger className="site-selector-header" aria-label="Select Site">
                                <Select.Value />
                                <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                    </svg>
                                </Select.Icon>
                            </Select.Trigger>
                            <Select.Portal>
                                <Select.Positioner className="select-positioner">
                                    <Select.Popup className="select-popup">
                                        <Select.Item className="select-item" value="ALL">
                                            <Select.ItemText>Overview</Select.ItemText>
                                        </Select.Item>
                                        {sites.map(site => (
                                            <Select.Item key={site.id} className="select-item" value={site.id}>
                                                <Select.ItemText>{site.name || site.id}</Select.ItemText>
                                            </Select.Item>
                                        ))}
                                    </Select.Popup>
                                </Select.Positioner>
                            </Select.Portal>
                        </Select.Root>
                    )}
                    {loggedIn && sites.length === 1 && (
                        <div className="site-badge-header" data-testid="header-site-name">
                            {sites[0].name || sites[0].id}
                        </div>
                    )}
                </div>

                {loggedIn && (
                    <button
                        className={`mobile-menu-toggle ${isMenuOpen ? 'open' : ''}`}
                        onClick={toggleMenu}
                        aria-label={isMenuOpen ? "Close navigation menu" : "Open navigation menu"}
                        aria-expanded={isMenuOpen}
                        aria-controls="mobile-menu-content">
                        <span className="hamburger-line" aria-hidden="true"></span>
                        <span className="hamburger-line" aria-hidden="true"></span>
                        <span className="hamburger-line" aria-hidden="true"></span>
                    </button>
                )}

                <div id="mobile-menu-content" className={`header-content ${isMenuOpen ? 'open' : ''}`}>
                    <nav className="header-nav">
                        {loggedIn ? (
                            <>
                                <Link to="/dashboard" className={`nav-link ${location === '/dashboard' ? 'active' : ''}`} aria-current={location === '/dashboard' ? 'page' : undefined} onClick={() => setIsMenuOpen(false)}>Dashboard</Link>
                                {selectedSiteID !== 'ALL' && (
                                    <>
                                        <Link to="/forecast" className={`nav-link ${location === '/forecast' ? 'active' : ''}`} aria-current={location === '/forecast' ? 'page' : undefined} onClick={() => setIsMenuOpen(false)}>Forecast</Link>
                                        <Link to="/settings" className={`nav-link ${location === '/settings' ? 'active' : ''}`} aria-current={location === '/settings' ? 'page' : undefined} onClick={() => setIsMenuOpen(false)}>Settings</Link>
                                    </>
                                )}
                            </>
                        ) : (
                            <span className="nav-empty-spacer"></span>
                        )}
                    </nav>

                    <div className="header-right">
                        {loggedIn ? (
                            <>
                                {showNotifications && (
                                    <button
                                        type="button"
                                        className="notification-bell-btn"
                                        onClick={() => {
                                            setIsNotificationModalOpen(true);
                                            setIsMenuOpen(false);
                                        }}
                                        aria-label="Notification settings"
                                        data-testid="header-bell-btn"
                                        title="Notification settings"
                                    >
                                        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"></path>
                                            <path d="M13.73 21a2 2 0 0 1-3.46 0"></path>
                                        </svg>
                                    </button>
                                )}
                                <button onClick={() => { onLogout(); setIsMenuOpen(false); }} className="logout-link">Log Out</button>
                            </>
                        ) : (
                            <Link to="/login" className="login-link" onClick={() => setIsMenuOpen(false)}>
                                <span className="hide-on-mobile">Log In / Sign Up</span>
                                <span className="hide-on-desktop">Get Started</span>
                            </Link>
                        )}
                    </div>
                </div>
            </div>

            {loggedIn && showNotifications && (
                <NotificationModal
                    open={isNotificationModalOpen}
                    onClose={() => setIsNotificationModalOpen(false)}
                    siteID={activeSiteID}
                    siteName={activeSiteName}
                />
            )}
        </header>
    );
};

export default Header;
