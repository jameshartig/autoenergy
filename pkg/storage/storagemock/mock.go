package storagemock

import (
	"context"
	"time"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/mock"
)

type MockDatabase struct {
	mock.Mock
}

var _ storage.Database = (*MockDatabase)(nil)

func (m *MockDatabase) GetSettings(ctx context.Context, siteID string) (types.Settings, int, time.Time, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(types.Settings), args.Int(1), args.Get(2).(time.Time), args.Error(3)
	}
	return types.Settings{}, 0, time.Time{}, nil
}

func (m *MockDatabase) SetSettings(ctx context.Context, siteID string, settings types.Settings, version int, updatedTime time.Time) error {
	args := m.Called(ctx, siteID, settings, version, updatedTime)
	return args.Error(0)
}

func (m *MockDatabase) UpsertPrices(ctx context.Context, siteID string, prices []types.Price, version int) error {
	args := m.Called(ctx, siteID, prices, version)
	return args.Error(0)
}

func (m *MockDatabase) InsertAction(ctx context.Context, siteID string, action types.Action) error {
	args := m.Called(ctx, siteID, action)
	return args.Error(0)
}

func (m *MockDatabase) UpsertEnergyHistories(ctx context.Context, siteID string, stats []types.DailyEnergyStats, version int) error {
	args := m.Called(ctx, siteID, stats, version)
	return args.Error(0)
}

func (m *MockDatabase) UpsertWeather(ctx context.Context, siteID string, weather []types.Weather, version int) error {
	args := m.Called(ctx, siteID, weather, version)
	return args.Error(0)
}

func (m *MockDatabase) UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error {
	args := m.Called(ctx, siteID, state)
	return args.Error(0)
}

func (m *MockDatabase) GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(types.ESSMockState), args.Error(1)
	}
	return types.ESSMockState{}, nil
}

func (m *MockDatabase) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.Price), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.Action), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.DailyEnergyStats, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.DailyEnergyStats), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(time.Time), args.Int(1), args.Error(2)
	}
	return time.Time{}, 0, nil
}

func (m *MockDatabase) GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(time.Time), args.Int(1), args.Error(2)
	}
	return time.Time{}, 0, nil
}

func (m *MockDatabase) GetLatestWeatherTime(ctx context.Context, siteID string) (time.Time, time.Time, int, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(time.Time), args.Get(1).(time.Time), args.Int(2), args.Error(3)
	}
	return time.Time{}, time.Time{}, 0, nil
}

func (m *MockDatabase) GetUser(ctx context.Context, email string) (types.User, error) {
	args := m.Called(ctx, email)
	if len(args) > 0 {
		return args.Get(0).(types.User), args.Error(1)
	}
	return types.User{}, nil
}

func (m *MockDatabase) GetSite(ctx context.Context, siteID string) (types.Site, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(types.Site), args.Error(1)
	}
	return types.Site{}, nil
}

func (m *MockDatabase) UpdateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *MockDatabase) CreateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *MockDatabase) CreateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockDatabase) UpdateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockDatabase) ListSites(ctx context.Context) ([]types.Site, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).([]types.Site), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) ListSitesSettings(ctx context.Context, release string, updateGroup []int) (map[string]types.Settings, map[string]int, map[string]time.Time, error) {
	args := m.Called(ctx, release, updateGroup)
	if len(args) > 0 {
		var times map[string]time.Time
		if tm, ok := args.Get(2).(map[string]time.Time); ok {
			times = tm
		}
		var setMap map[string]types.Settings
		if sm, ok := args.Get(0).(map[string]types.Settings); ok {
			setMap = sm
		}
		var verMap map[string]int
		if vm, ok := args.Get(1).(map[string]int); ok {
			verMap = vm
		}
		return setMap, verMap, times, args.Error(3)
	}
	return nil, nil, nil, nil
}

func (m *MockDatabase) GetLatestAction(ctx context.Context, siteID string) (*types.Action, error) {
	args := m.Called(ctx, siteID)
	val := args.Get(0)
	if val == nil {
		return nil, args.Error(1)
	}
	return val.(*types.Action), args.Error(1)
}

func (m *MockDatabase) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockDatabase) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDatabase) InsertFeedback(ctx context.Context, feedback types.Feedback) error {
	args := m.Called(ctx, feedback)
	return args.Error(0)
}

func (m *MockDatabase) ListFeedback(ctx context.Context, limit int, lastFeedbackID string) ([]types.Feedback, error) {
	args := m.Called(ctx, limit, lastFeedbackID)
	return args.Get(0).([]types.Feedback), args.Error(1)
}

func (m *MockDatabase) GetWeather(ctx context.Context, siteID string, start, end time.Time) ([]types.Weather, error) {
	args := m.Called(ctx, siteID, start, end)
	return args.Get(0).([]types.Weather), args.Error(1)
}

func (m *MockDatabase) UpsertUtilityPrices(ctx context.Context, utilityID string, prices []types.PriceState, version int) error {
	args := m.Called(ctx, utilityID, prices, version)
	return args.Error(0)
}

func (m *MockDatabase) GetUtilityPrices(ctx context.Context, utilityID string, start, end time.Time) ([]types.PriceState, error) {
	args := m.Called(ctx, utilityID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.PriceState), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) UpsertInterest(ctx context.Context, submission types.InterestSubmission) error {
	args := m.Called(ctx, submission)
	return args.Error(0)
}

func (m *MockDatabase) ListInterest(ctx context.Context, limit int) ([]types.InterestSubmission, error) {
	args := m.Called(ctx, limit)
	if len(args) > 0 {
		return args.Get(0).([]types.InterestSubmission), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) DeleteInterest(ctx context.Context, email string) error {
	args := m.Called(ctx, email)
	return args.Error(0)
}

func (m *MockDatabase) GetHistorySummaries(ctx context.Context, siteID string, start, end time.Time) ([]types.HistorySummary, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.HistorySummary), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) UpdateHistorySummary(ctx context.Context, siteID string, month string, newSummary types.HistorySummary) (types.HistorySummary, error) {
	args := m.Called(ctx, siteID, month, newSummary)
	if len(args) > 0 {
		return args.Get(0).(types.HistorySummary), args.Error(1)
	}
	return newSummary, nil
}

func (m *MockDatabase) DeleteSite(ctx context.Context, siteID string) error {
	args := m.Called(ctx, siteID)
	return args.Error(0)
}

func (m *MockDatabase) DeleteUser(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockDatabase) ListUsers(ctx context.Context) ([]types.User, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).([]types.User), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetAdminSettings(ctx context.Context) (types.AdminSettings, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).(types.AdminSettings), args.Error(1)
	}
	return types.AdminSettings{}, nil
}

func (m *MockDatabase) UpdateAdminSettings(ctx context.Context, settings types.AdminSettings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

func (m *MockDatabase) AddUserPushSubscription(ctx context.Context, userID string, sub types.PushSubscription) error {
	args := m.Called(ctx, userID, sub)
	return args.Error(0)
}

func (m *MockDatabase) RemoveUserPushSubscription(ctx context.Context, userID string, endpoint string) error {
	args := m.Called(ctx, userID, endpoint)
	return args.Error(0)
}

func (m *MockDatabase) UpdateSiteNotificationSettings(ctx context.Context, siteID string, userID string, settings types.UserNotificationSettings) error {
	args := m.Called(ctx, siteID, userID, settings)
	return args.Error(0)
}

func (m *MockDatabase) GetNotificationLogs(ctx context.Context, siteID string, start, end time.Time) ([]types.NotificationLog, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.NotificationLog), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) AppendNotificationLog(ctx context.Context, siteID string, log types.NotificationLog) error {
	args := m.Called(ctx, siteID, log)
	return args.Error(0)
}

func (m *MockDatabase) RecordNotificationClick(ctx context.Context, siteID string, month string, logID string, clickedAt time.Time) error {
	args := m.Called(ctx, siteID, month, logID, clickedAt)
	return args.Error(0)
}
