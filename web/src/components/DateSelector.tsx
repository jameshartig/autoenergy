import React, { useState, useEffect } from 'react';
import './DateSelector.css';

interface DateSelectorProps {
    currentDate: Date;
    onDateChange: (days: number) => void;
    loading?: boolean;
    isToday: boolean;
}

const DateSelector: React.FC<DateSelectorProps> = ({
    currentDate,
    onDateChange,
    loading = false,
    isToday
}) => {
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    const formattedDate = currentDate.toLocaleDateString(undefined, isMobile ? {
        weekday: 'short',
        month: 'short',
        day: 'numeric',
        year: 'numeric'
    } : {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric'
    });

    useEffect(() => {
        const handleKeyDown = (e: KeyboardEvent) => {
            if (document.activeElement?.tagName === 'INPUT' || document.activeElement?.tagName === 'TEXTAREA') return;

            if (e.key === 'ArrowLeft' && !loading) {
                onDateChange(-1);
            } else if (e.key === 'ArrowRight' && !loading && !isToday) {
                onDateChange(1);
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        return () => window.removeEventListener('keydown', handleKeyDown);
    }, [onDateChange, loading, isToday]);

    return (
        <div className="date-controls" role="group" aria-label="Date navigation">
            <button
                onClick={() => onDateChange(-1)}
                disabled={loading}
                aria-label="Previous day"
                title="Previous day (Left Arrow)"
            >
                {loading && <span className="loading-spinner" aria-hidden="true"></span>}
                <span aria-hidden="true">&lt;</span> Prev
                {!isMobile && <kbd aria-hidden="true" className="shortcut-hint">←</kbd>}
            </button>
            <h2 aria-live="polite" aria-atomic="true">{formattedDate}</h2>
            <button
                onClick={() => onDateChange(1)}
                disabled={loading || isToday}
                aria-label="Next day"
                title={isToday ? "Cannot view future dates" : "Next day (Right Arrow)"}
            >
                {loading && <span className="loading-spinner" aria-hidden="true"></span>}
                Next <span aria-hidden="true">&gt;</span>
                {!isMobile && <kbd aria-hidden="true" className="shortcut-hint">→</kbd>}
            </button>
        </div>
    );
};

export default DateSelector;
