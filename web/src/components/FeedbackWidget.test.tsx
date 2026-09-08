import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Router } from 'wouter';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import FeedbackWidget from './FeedbackWidget';
import * as api from '../api';

vi.mock('../api', () => ({
    submitFeedback: vi.fn(),
}));

describe('FeedbackWidget Component', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders the floating action button', () => {
        render(
            <Router>
                <FeedbackWidget siteID="site1" />
            </Router>
        );

        const fab = screen.getByRole('button', { name: /Feedback/i });
        expect(fab).toBeInTheDocument();
    });

    it('opens popover when FAB is clicked and displays form', async () => {
        render(
            <Router>
                <FeedbackWidget siteID="site1" />
            </Router>
        );

        const fab = screen.getByRole('button', { name: /Feedback/i });
        fireEvent.click(fab);

        expect(await screen.findByText('How are you feeling about RateRudder?')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Submit' })).toBeDisabled();
    });

    it('allows selecting sentiment and submitting feedback', async () => {
        (api.submitFeedback as any).mockResolvedValue({ success: true });

        render(
            <Router>
                <FeedbackWidget siteID="site1" />
            </Router>
        );

        const fab = screen.getByRole('button', { name: /Feedback/i });
        fireEvent.click(fab);

        const happyBtn = await screen.findByRole('button', { name: 'Happy' });
        fireEvent.click(happyBtn);

        const submitBtn = screen.getByRole('button', { name: 'Submit' });
        expect(submitBtn).not.toBeDisabled();

        fireEvent.click(submitBtn);

        await waitFor(() => {
            expect(api.submitFeedback).toHaveBeenCalledWith(
                'site1',
                'happy',
                '',
                expect.any(Object)
            );
        });

        expect(await screen.findByText('Thank you!')).toBeInTheDocument();
    });

    it('allows typing a comment and submitting without sentiment', async () => {
        (api.submitFeedback as any).mockResolvedValue({ success: true });

        render(
            <Router>
                <FeedbackWidget siteID="site1" />
            </Router>
        );

        const fab = screen.getByRole('button', { name: /Feedback/i });
        fireEvent.click(fab);

        const textarea = await screen.findByPlaceholderText(/Tell us more about your experience/i);
        fireEvent.change(textarea, { target: { value: 'Great app!' } });

        const submitBtn = screen.getByRole('button', { name: 'Submit' });
        expect(submitBtn).not.toBeDisabled();

        fireEvent.click(submitBtn);

        await waitFor(() => {
            expect(api.submitFeedback).toHaveBeenCalledWith(
                'site1',
                'neutral',
                'Great app!',
                expect.any(Object)
            );
        });
    });
});
