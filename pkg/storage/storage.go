package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/types"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrSiteNotFound     = errors.New("site not found")
	ErrSettingsConflict = errors.New("settings conflict: cannot overwrite")
)

// Database defines the interface for persisting data and retrieving settings.
type Database interface {
	// Settings
	GetSettings(ctx context.Context, siteID string) (types.Settings, int, time.Time, error)
	SetSettings(ctx context.Context, siteID string, settings types.Settings, version int, updatedTime time.Time) error
	ListSitesSettings(ctx context.Context, release string, updateGroup []int) (map[string]types.Settings, map[string]int, map[string]time.Time, error)

	// Data Persistence
	// UpsertPrices adds or updates multiple price records.
	UpsertPrices(ctx context.Context, siteID string, prices []types.Price, version int) error
	// UpsertUtilityPrices adds or updates multiple price records for a utility.
	UpsertUtilityPrices(ctx context.Context, utilityID string, prices []types.PriceState, version int) error
	InsertAction(ctx context.Context, siteID string, action types.Action) error
	// UpsertEnergyHistories adds or updates multiple energy history records.
	UpsertEnergyHistories(ctx context.Context, siteID string, stats []types.DailyEnergyStats, version int) error
	UpsertWeather(ctx context.Context, siteID string, weather []types.Weather, version int) error
	UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error
	GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error)

	// History
	GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error)
	// GetUtilityPrices retrieves price records within the specified time range for a utility.
	GetUtilityPrices(ctx context.Context, utilityID string, start, end time.Time) ([]types.PriceState, error)
	GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error)
	GetLatestAction(ctx context.Context, siteID string) (*types.Action, error)
	GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.DailyEnergyStats, error)
	GetWeather(ctx context.Context, siteID string, start, end time.Time) ([]types.Weather, error)
	GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error)
	GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error)
	GetLatestWeatherTime(ctx context.Context, siteID string) (time.Time, time.Time, int, error)

	// History summaries
	GetHistorySummaries(ctx context.Context, siteID string, start, end time.Time) ([]types.HistorySummary, error)
	UpdateHistorySummary(ctx context.Context, siteID string, month string, newSummary types.HistorySummary) (types.HistorySummary, error)

	// Sites & Users
	GetSite(ctx context.Context, siteID string) (types.Site, error)
	ListSites(ctx context.Context) ([]types.Site, error)
	CreateSite(ctx context.Context, siteID string, site types.Site) error
	UpdateSite(ctx context.Context, siteID string, site types.Site) error
	DeleteSite(ctx context.Context, siteID string) error
	GetUser(ctx context.Context, userID string) (types.User, error)
	CreateUser(ctx context.Context, user types.User) error
	UpdateUser(ctx context.Context, user types.User) error
	DeleteUser(ctx context.Context, userID string) error
	ListUsers(ctx context.Context) ([]types.User, error)
	GetAdminSettings(ctx context.Context) (types.AdminSettings, error)
	UpdateAdminSettings(ctx context.Context, settings types.AdminSettings) error

	// Feedback
	InsertFeedback(ctx context.Context, feedback types.Feedback) error
	ListFeedback(ctx context.Context, limit int, lastFeedbackID string) ([]types.Feedback, error)

	// Interest
	UpsertInterest(ctx context.Context, submission types.InterestSubmission) error
	ListInterest(ctx context.Context, limit int) ([]types.InterestSubmission, error)
	DeleteInterest(ctx context.Context, email string) error

	// Notifications
	AddUserPushSubscription(ctx context.Context, userID string, sub types.PushSubscription) error
	RemoveUserPushSubscription(ctx context.Context, userID string, endpoint string) error
	UpdateSiteNotificationSettings(ctx context.Context, siteID string, userID string, settings types.UserNotificationSettings) error
	GetNotificationLogs(ctx context.Context, siteID string, start, end time.Time) ([]types.NotificationLog, error)
	AppendNotificationLog(ctx context.Context, siteID string, log types.NotificationLog) error
	RecordNotificationClick(ctx context.Context, siteID string, month string, logID string, clickedAt time.Time) error

	// Lifecycle
	Ping(ctx context.Context) error
	Close() error
}

// Configured sets up the Storage provider based on flags.
func Configured() Database {
	provider := lflag.String("storage-provider", "firestore", "Storage provider to use (available: firestore)")

	var p struct{ Database }

	fs := configuredFirestore()

	lflag.Do(func() {
		switch *provider {
		case "firestore":
			if err := fs.Validate(); err != nil {
				panic(fmt.Sprintf("firestore validation failed: %v", err))
			}
			p.Database = fs
			if err := fs.Init(context.Background()); err != nil {
				panic(fmt.Sprintf("firestore init failed: %v", err))
			}
		default:
			panic(fmt.Sprintf("unknown storage provider: %s", *provider))
		}
	})

	return &p
}
