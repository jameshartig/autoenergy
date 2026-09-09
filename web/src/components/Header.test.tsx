import { render, screen, fireEvent } from '@testing-library/react';
import { Router } from 'wouter';
import { memoryLocation } from 'wouter/memory-location';
import Header from './Header';
import { describe, it, expect, beforeEach, vi } from 'vitest';

describe('Header Component', () => {
    const mockOnSiteChange = vi.fn();
    const mockOnLogout = vi.fn();

    beforeEach(() => {
        vi.restoreAllMocks();
    });

    const renderHeader = (path: string, loggedIn: boolean) => {
        const { hook } = memoryLocation({ static: true, path: path });
        return render(
            <Router hook={hook}>
                <Header
                    loggedIn={loggedIn}
                    sites={[{ id: 'site1', name: 'Site 1' }]}
                    selectedSiteID="site1"
                    onSiteChange={mockOnSiteChange}
                    onLogout={mockOnLogout}
                />
            </Router>
        );
    };

    it('renders correctly when logged out on homepage', () => {
        renderHeader('/', false);

        expect(screen.getByText('RateRudder')).toBeInTheDocument();
        expect(screen.queryByText('Dashboard')).not.toBeInTheDocument();
        expect(screen.getByText(/Log In/)).toBeInTheDocument();
    });

    it('shows nav links when logged in on dashboard', () => {
        renderHeader('/dashboard', true);

        expect(screen.getByText('Dashboard')).toBeInTheDocument();
        expect(screen.getByText('Forecast')).toBeInTheDocument();
        expect(screen.getByText('Settings')).toBeInTheDocument();
    });

    it('shows active styling and aria-current for the current route', () => {
        renderHeader('/dashboard', true);
        const dashboardLink = screen.getByText('Dashboard');
        expect(dashboardLink).toHaveClass('active');
        expect(dashboardLink).toHaveAttribute('aria-current', 'page');

        const forecastLink = screen.getByText('Forecast');
        expect(forecastLink).not.toHaveClass('active');
        expect(forecastLink).not.toHaveAttribute('aria-current');
    });

    it('calls onLogout when logout button is clicked', () => {
        renderHeader('/dashboard', true);
        fireEvent.click(screen.getByText('Log Out'));
        expect(mockOnLogout).toHaveBeenCalledTimes(1);
    });

    it('has correct aria attributes on mobile menu button', () => {
        renderHeader('/dashboard', true);
        const menuButton = screen.getByLabelText('Open navigation menu');
        expect(menuButton).toBeInTheDocument();
        expect(menuButton).toHaveAttribute('aria-controls', 'mobile-menu-content');
        expect(menuButton).toHaveAttribute('aria-expanded', 'false');
    });

    it('shows static site badge when there is only one site', () => {
        const { hook } = memoryLocation({ static: true, path: '/dashboard' });
        render(
            <Router hook={hook}>
                <Header
                    loggedIn={true}
                    sites={[{ id: 'site1', name: 'Only Site' }]}
                    selectedSiteID="site1"
                    onSiteChange={mockOnSiteChange}
                    onLogout={mockOnLogout}
                />
            </Router>
        );

        const siteNameElement = screen.getByTestId('header-site-name');
        expect(siteNameElement).toBeInTheDocument();
        expect(siteNameElement).toHaveTextContent('Only Site');
        expect(screen.queryByLabelText('Select Site')).not.toBeInTheDocument();
    });

    it('shows site selector dropdown when there are multiple sites', () => {
        const { hook } = memoryLocation({ static: true, path: '/dashboard' });
        render(
            <Router hook={hook}>
                <Header
                    loggedIn={true}
                    sites={[
                        { id: 'site1', name: 'Site 1' },
                        { id: 'site2', name: 'Site 2' },
                    ]}
                    selectedSiteID="site1"
                    onSiteChange={mockOnSiteChange}
                    onLogout={mockOnLogout}
                />
            </Router>
        );

        expect(screen.queryByTestId('header-site-name')).not.toBeInTheDocument();
        expect(screen.getByLabelText('Select Site')).toBeInTheDocument();
    });

    it('does not render notification bell by default', () => {
        renderHeader('/dashboard', true);
        expect(screen.queryByTestId('header-bell-btn')).not.toBeInTheDocument();
    });

    it('renders notification bell when ?notifications=true is in the query params', () => {
        renderHeader('/dashboard?notifications=true', true);
        expect(screen.getByTestId('header-bell-btn')).toBeInTheDocument();
    });

    it('renders notification bell when on iOS home screen even without query params', () => {
        vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
        (window as any).Notification = { permission: 'default' };

        renderHeader('/dashboard', true);
        expect(screen.getByTestId('header-bell-btn')).toBeInTheDocument();
    });

    it('opens notification modal when bell button is clicked', async () => {
        renderHeader('/dashboard?notifications=true', true);
        const bellBtn = screen.getByTestId('header-bell-btn');
        fireEvent.click(bellBtn);

        expect(await screen.findByText('Notifications')).toBeInTheDocument();
    });
});
