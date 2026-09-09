package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FirestoreProvider implements the Provider interface using Google Cloud Firestore.
// It persists settings, prices, and actions to Firestore collections.
type FirestoreProvider struct {
	client    *firestore.Client
	projectID string
	database  string
}

// configuredFirestore sets up the Firestore provider.
// It registers flags for configuration.
func configuredFirestore() *FirestoreProvider {
	projectID := lflag.String("firestore-project-id", "", "Google Cloud Project ID for Firestore")
	database := lflag.String("firestore-database", "", "Google Cloud Firestore Database")
	emulator := lflag.String("firestore-emulator", "", "Use Firestore emulator")

	f := &FirestoreProvider{}

	lflag.Do(func() {
		f.projectID = *projectID
		f.database = *database

		// set this because that's how firestore client expects it
		if *emulator != "" {
			os.Setenv("FIRESTORE_EMULATOR_HOST", *emulator)
		}
	})

	return f
}

// Validate checks if the provider is properly configured.
func (f *FirestoreProvider) Validate() error {
	// Project ID verification could be here, but we allow empty if inferred.
	return nil
}

// Init initializes the Firestore client.
// This must be called before using the provider methods.
func (f *FirestoreProvider) Init(ctx context.Context) error {
	projectID := f.projectID
	if projectID == "" {
		projectID = firestore.DetectProjectID
	}
	database := f.database
	if database == "" {
		database = firestore.DefaultDatabaseID
	}
	client, err := firestore.NewClientWithDatabase(ctx, projectID, database)
	if err != nil {
		return fmt.Errorf("failed to create firestore client (project=%s, database=%s): %w", projectID, database, err)
	}
	f.client = client
	return nil
}

// FirestoreClient returns the underlying *firestore.Client.
func (f *FirestoreProvider) FirestoreClient() *firestore.Client {
	return f.client
}

// Ping checks if the Firestore client can connect to the database.
func (f *FirestoreProvider) Ping(ctx context.Context) error {
	if f.client == nil {
		return fmt.Errorf("firestore client not initialized")
	}
	// Use a simple operation to check connectivity
	_, err := f.client.Doc("health_check/non_existent").Get(ctx)
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("failed to ping firestore: %w", err)
	}
	return nil
}

// Close closes the Firestore client connection.
func (f *FirestoreProvider) Close() error {
	if f.client != nil {
		return f.client.Close()
	}
	return nil
}

func (f *FirestoreProvider) getCollection(siteID, name string) (*firestore.CollectionRef, error) {
	if siteID == "" {
		return nil, fmt.Errorf("siteID cannot be empty")
	}
	return f.client.Collection("sites").Doc(siteID).Collection(name), nil
}

// GetSettings retrieves the dynamic configuration from the "config/settings" document.
func (f *FirestoreProvider) GetSettings(ctx context.Context, siteID string) (types.Settings, int, time.Time, error) {
	coll, err := f.getCollection(siteID, "config")
	if err != nil {
		return types.Settings{}, 0, time.Time{}, err
	}
	doc, err := coll.Doc("settings").Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Return default settings if not found
			return types.Settings{}, 0, time.Time{}, nil
		}
		return types.Settings{}, 0, time.Time{}, fmt.Errorf("failed to fetch settings doc: %w", err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "settings doc missing json", slog.String("siteID", siteID))
		return types.Settings{}, 0, time.Time{}, fmt.Errorf("settings document missing 'json' field: %w", err)
	}

	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "settings doc json not string", slog.String("siteID", siteID))
		return types.Settings{}, 0, time.Time{}, fmt.Errorf("settings 'json' field is not a string")
	}

	var s types.Settings
	if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal settings json", slog.String("siteID", siteID), slog.Any("err", err))
		return types.Settings{}, 0, time.Time{}, fmt.Errorf("failed to unmarshal settings json: %w", err)
	}
	return s, version, doc.UpdateTime, nil
}

// SetSettings saves the dynamic configuration to the "config/settings" document.
// It stores the settings as a JSON string for portability.
// If updatedTime is non-zero, it runs in a transaction and ensures doc.UpdateTime matches updatedTime before writing.
func (f *FirestoreProvider) SetSettings(ctx context.Context, siteID string, settings types.Settings, version int, updatedTime time.Time) error {
	jsonBytes, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	coll, err := f.getCollection(siteID, "config")
	if err != nil {
		return err
	}

	data := map[string]any{
		"json":        string(jsonBytes),
		"version":     version,
		"updateGroup": settings.UpdateGroup,
		"release":     settings.Release,
	}

	if !updatedTime.IsZero() {
		return f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			doc, err := tx.Get(coll.Doc("settings"))
			if err != nil {
				return fmt.Errorf("failed to fetch settings doc for transaction: %w", err)
			}
			if !doc.UpdateTime.Equal(updatedTime) {
				return fmt.Errorf("%w: settings updated at %v, expected %v", ErrSettingsConflict, doc.UpdateTime, updatedTime)
			}
			return tx.Set(coll.Doc("settings"), data)
		})
	}

	_, err = coll.Doc("settings").Set(ctx, data)
	if err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}
	return nil
}

// InsertAction adds a new action record to the "actions" collection as a JSON blob.
// The document ID is the RFC3339 timestamp for efficient range queries.
func (f *FirestoreProvider) InsertAction(ctx context.Context, siteID string, action types.Action) error {
	jsonBytes, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %w", err)
	}

	coll, err := f.getCollection(siteID, "action_history")
	if err != nil {
		return err
	}
	// Use RFC3339 as document ID for lexicographic ordering and efficient range queries
	docID := action.Timestamp.UTC().Format(time.RFC3339)
	_, err = coll.Doc(docID).Set(ctx, map[string]any{
		"json":      string(jsonBytes),
		"timestamp": action.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("failed to insert action: %w", err)
	}
	return nil
}

// GetActionHistory retrieves action records within the specified time range.
func (f *FirestoreProvider) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	if !start.Before(end) {
		return nil, nil
	}
	coll, err := f.getCollection(siteID, "action_history")
	if err != nil {
		return nil, err
	}
	iter := coll.
		Where("timestamp", ">=", start).
		Where("timestamp", "<", end).
		OrderBy("timestamp", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var actions []types.Action
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating actions: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "action doc missing json", slog.String("actionID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("action document %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "action doc json not string", slog.String("actionID", doc.Ref.ID), slog.String("siteID", siteID))
			return nil, fmt.Errorf("action document %s 'json' field is not string", doc.Ref.ID)
		}

		var a types.Action
		if err := json.Unmarshal([]byte(jsonStr), &a); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal action", slog.String("actionID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("failed to unmarshal action (id=%s): %w", doc.Ref.ID, err)
		}
		actions = append(actions, a)
	}
	return actions, nil
}

// GetLatestAction retrieves the most recent action record. Will return nil if no actions found.
func (f *FirestoreProvider) GetLatestAction(ctx context.Context, siteID string) (*types.Action, error) {
	coll, err := f.getCollection(siteID, "action_history")
	if err != nil {
		return nil, err
	}

	iter := coll.
		OrderBy("timestamp", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		return nil, nil // No actions found
	}
	if err != nil {
		return nil, fmt.Errorf("error getting latest action: %w", err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		return nil, fmt.Errorf("action doc %s missing 'json': %w", doc.Ref.ID, err)
	}

	jsonStr, ok := val.(string)
	if !ok {
		return nil, fmt.Errorf("action doc %s 'json' field is not string", doc.Ref.ID)
	}

	var a types.Action
	if err := json.Unmarshal([]byte(jsonStr), &a); err != nil {
		return nil, fmt.Errorf("failed to unmarshal action: %w", err)
	}

	return &a, nil
}

// UpsertEnergyHistories adds or updates multiple energy history records in the "energy_history" collection.
func (f *FirestoreProvider) UpsertEnergyHistories(ctx context.Context, siteID string, stats []types.DailyEnergyStats, version int) error {
	if len(stats) == 0 {
		return nil
	}

	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return err
	}

	for _, s := range stats {
		if s.TSDayStart.IsZero() {
			return fmt.Errorf("energy stats missing tsDayStart")
		}

		slices.SortFunc(s.Hourly, func(a, b types.EnergyStats) int {
			return a.TSHourStart.Compare(b.TSHourStart)
		})

		// Check for contiguous data and warn if not 'today'
		nowSite := time.Now().In(s.TSDayStart.Location())
		todayStart := time.Date(nowSite.Year(), nowSite.Month(), nowSite.Day(), 0, 0, 0, 0, nowSite.Location())
		if s.TSDayStart.Before(todayStart) {
			contiguous := true
			if len(s.Hourly) == 0 {
				contiguous = false
			} else {
				for i := 1; i < len(s.Hourly); i++ {
					if !s.Hourly[i].TSHourStart.Equal(s.Hourly[i-1].TSHourStart.Add(time.Hour)) {
						contiguous = false
						break
					}
				}
				// Also check if the day has roughly 24 hours of data.
				// A local day can have 23, 24, or 25 hours due to DST.
				// Just checking if we have at least 23 hours.
				if len(s.Hourly) < 23 {
					contiguous = false
				}
			}
			if !contiguous {
				log.Ctx(ctx).WarnContext(
					ctx,
					"non-contiguous energy data provided",
					slog.String("siteID", siteID),
					slog.Time("tsDayStart", s.TSDayStart),
					slog.Int("hours", len(s.Hourly)),
				)
			}
		}
	}

	// For a single item, use direct Set to avoid batch overhead
	if len(stats) == 1 {
		s := stats[0]
		s.TSDayStart = time.Date(s.TSDayStart.Year(), s.TSDayStart.Month(), s.TSDayStart.Day(), 0, 0, 0, 0, s.TSDayStart.Location())
		jsonBytes, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("failed to marshal energy stats: %w", err)
		}
		docID := s.TSDayStart.Format("2006-01-02")
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": s.TSDayStart,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert energy history: %w", err)
		}
		return nil
	}

	// For multiple items, use BulkWriter
	bw := f.client.BulkWriter(ctx)
	var endOnce sync.Once
	endBW := func() { endOnce.Do(func() { bw.End() }) }
	defer endBW()

	jobs := make([]*firestore.BulkWriterJob, 0, len(stats))

	for _, s := range stats {
		s.TSDayStart = time.Date(s.TSDayStart.Year(), s.TSDayStart.Month(), s.TSDayStart.Day(), 0, 0, 0, 0, s.TSDayStart.Location())

		jsonBytes, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("failed to marshal energy stats: %w", err)
		}

		docID := s.TSDayStart.Format("2006-01-02")
		ref := coll.Doc(docID)

		job, err := bw.Set(ref, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": s.TSDayStart,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue energy history: %w", err)
		}
		jobs = append(jobs, job)
	}

	endBW()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert energy history: %w", err)
		}
	}

	return nil
}

// UpsertWeather adds or updates multiple weather records.
func (f *FirestoreProvider) UpsertWeather(ctx context.Context, siteID string, weather []types.Weather, version int) error {
	if len(weather) == 0 {
		return nil
	}
	for _, w := range weather {
		if w.TSDayStart.IsZero() {
			return errors.New("weather tsDayStart cannot be zero")
		}
	}

	coll, err := f.getCollection(siteID, "weather")
	if err != nil {
		return err
	}

	if len(weather) == 1 {
		w := weather[0]
		jsonBytes, err := json.Marshal(w)
		if err != nil {
			return fmt.Errorf("failed to marshal weather: %w", err)
		}
		docID := w.TSDayStart.Format("2006-01-02")
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": w.TSDayStart,
			"tsUpdated":  w.TSUpdated,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert weather: %w", err)
		}
		return nil
	}

	// For multiple items, use BulkWriter
	bw := f.client.BulkWriter(ctx)
	var endOnce sync.Once
	endBW := func() { endOnce.Do(func() { bw.End() }) }
	defer endBW()

	jobs := make([]*firestore.BulkWriterJob, 0, len(weather))

	for _, w := range weather {
		jsonBytes, err := json.Marshal(w)
		if err != nil {
			return fmt.Errorf("failed to marshal weather: %w", err)
		}

		docID := w.TSDayStart.Format("2006-01-02")
		ref := coll.Doc(docID)
		job, err := bw.Set(ref, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": w.TSDayStart,
			"tsUpdated":  w.TSUpdated,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue weather: %w", err)
		}
		jobs = append(jobs, job)
	}

	endBW()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert weather: %w", err)
		}
	}

	return nil
}

// GetWeather retrieves weather records within the specified time range.
func (f *FirestoreProvider) GetWeather(ctx context.Context, siteID string, start, end time.Time) ([]types.Weather, error) {
	if !start.Before(end) {
		return nil, nil
	}

	coll, err := f.getCollection(siteID, "weather")
	if err != nil {
		return nil, err
	}
	// Subtract 23 hours from start to handle timezone shifts and late-in-day queries.
	// Stored daily records have tsDayStart representing midnight in their respective
	// timezones. A query start time later in the day (or from a different timezone)
	// could fall after the stored record's tsDayStart timestamp in UTC, which would
	// incorrectly exclude the record for that day.
	queryStart := start.Add(-23 * time.Hour)
	iter := coll.
		Where("tsDayStart", ">=", queryStart).
		Where("tsDayStart", "<", end).
		OrderBy("tsDayStart", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var weather []types.Weather
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating weather: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "weather doc missing json", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "weather doc json not string", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID))
			continue
		}

		var w types.Weather
		if err := json.Unmarshal([]byte(jsonStr), &w); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal weather", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			continue
		}

		dayEnd := w.TSDayStart.AddDate(0, 0, 1)
		if !dayEnd.After(start) || !w.TSDayStart.Before(end) {
			continue
		}

		weather = append(weather, w)
	}
	return weather, nil
}

// GetEnergyHistory retrieves energy history records within the specified time range.
func (f *FirestoreProvider) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.DailyEnergyStats, error) {
	if !start.Before(end) {
		return nil, nil
	}

	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return nil, err
	}

	// Subtract 23 hours from start to handle timezone shifts and late-in-day queries.
	// Stored daily records have tsDayStart representing midnight in their respective
	// timezones. A query start time later in the day (or from a different timezone)
	// could fall after the stored record's tsDayStart timestamp in UTC, which would
	// incorrectly exclude the record for that day.
	queryStart := start.Add(-23 * time.Hour)
	iter := coll.
		Where("tsDayStart", ">=", queryStart).
		Where("tsDayStart", "<", end).
		OrderBy("tsDayStart", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var allStats []types.DailyEnergyStats
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating daily energy history: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "energy stats doc missing json", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("energy stats doc %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "energy stats doc json not string", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID))
			return nil, fmt.Errorf("energy stats doc %s 'json' field is not string", doc.Ref.ID)
		}

		var s types.DailyEnergyStats
		if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal daily energy stats", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("failed to unmarshal daily energy stats (id=%s): %w", doc.Ref.ID, err)
		}

		dayEnd := s.TSDayStart.AddDate(0, 0, 1)
		if !dayEnd.After(start) || !s.TSDayStart.Before(end) {
			continue
		}

		allStats = append(allStats, s)
	}

	return allStats, nil
}

// GetLatestEnergyHistoryTime retrieves the timestamp of the last stored energy
// history record.
func (f *FirestoreProvider) GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return time.Time{}, 0, err
	}
	iter := coll.
		OrderBy("tsDayStart", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		return time.Time{}, 0, nil
	}
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to get latest energy history doc: %w", err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	// If this doc is daily, the latest actual recorded hour needs to be extracted from JSON
	val, err := doc.DataAt("json")
	if err == nil {
		if jsonStr, ok := val.(string); ok {
			var s types.DailyEnergyStats
			if err := json.Unmarshal([]byte(jsonStr), &s); err == nil {
				if len(s.Hourly) > 0 {
					latest := s.Hourly[0].TSHourStart
					for _, h := range s.Hourly {
						if h.TSHourStart.After(latest) {
							latest = h.TSHourStart
						}
					}
					return latest, version, nil
				}
				if !s.TSDayStart.IsZero() {
					if s.TimeLocation != "" {
						if loc, err := time.LoadLocation(s.TimeLocation); err == nil {
							return s.TSDayStart.In(loc), version, nil
						}
					}
					return s.TSDayStart, version, nil
				}
			}
		}
	}

	if tsVal, err := doc.DataAt("tsDayStart"); err == nil {
		if t, ok := tsVal.(time.Time); ok && !t.IsZero() {
			return t, version, nil
		}
	}

	ts, err := time.Parse("2006-01-02", doc.Ref.ID)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid energy history doc id %s: %w", doc.Ref.ID, err)
	}

	return ts, version, nil
}

// GetSite retrieves a site from the "sites" collection.
func (f *FirestoreProvider) GetSite(ctx context.Context, siteID string) (types.Site, error) {
	if siteID == "" {
		return types.Site{}, errors.New("siteID cannot be empty")
	}
	doc, err := f.client.Collection("sites").Doc(siteID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return types.Site{}, fmt.Errorf("%w: %s", ErrSiteNotFound, siteID)
		}
		return types.Site{}, fmt.Errorf("failed to get site %s: %w", siteID, err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "site doc missing json", slog.String("siteID", siteID), slog.Any("err", err))
		return types.Site{}, fmt.Errorf("site %s missing json: %w", siteID, err)
	}
	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "site doc json not string", slog.String("siteID", siteID))
		return types.Site{}, fmt.Errorf("site %s json not string", siteID)
	}

	var site types.Site
	if err := json.Unmarshal([]byte(jsonStr), &site); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal site", slog.String("siteID", siteID), slog.Any("err", err))
		return types.Site{}, fmt.Errorf("failed to unmarshal site %s: %w", siteID, err)
	}
	return site, nil
}

// ListSites retrieves all sites from the "sites" collection.
func (f *FirestoreProvider) ListSites(ctx context.Context) ([]types.Site, error) {
	iter := f.client.Collection("sites").Documents(ctx)
	defer iter.Stop()

	var sites []types.Site
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating sites: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "site doc missing json", slog.String("siteID", doc.Ref.ID))
			// Skip malformed documents
			continue
		}
		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "site doc json not string", slog.String("siteID", doc.Ref.ID))
			continue
		}

		var site types.Site
		if err := json.Unmarshal([]byte(jsonStr), &site); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal site", slog.String("siteID", doc.Ref.ID), slog.Any("err", err))
			// Skip malformed JSON
			continue
		}
		sites = append(sites, site)
	}
	return sites, nil
}

// ListSitesSettings retrieves settings for sites, optionally filtered by updateGroup
// and release. Release is required if updateGroup is provided.
func (f *FirestoreProvider) ListSitesSettings(ctx context.Context, release string, updateGroup []int) (map[string]types.Settings, map[string]int, map[string]time.Time, error) {
	q := f.client.CollectionGroup("config").Query
	if release != "" {
		q = q.Where("release", "==", release)
	}
	if len(updateGroup) > 0 {
		if release == "" {
			return nil, nil, nil, errors.New("release cannot be empty when updateGroup is specified")
		}
		q = q.Where("updateGroup", "in", updateGroup)
	}
	iter := q.Documents(ctx)
	defer iter.Stop()

	settingsMap := make(map[string]types.Settings)
	versionsMap := make(map[string]int)
	updatedTimesMap := make(map[string]time.Time)

	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error iterating config documents: %w", err)
		}

		// Only look at the settings document
		if doc.Ref.ID != "settings" {
			continue
		}

		parentSite := doc.Ref.Parent.Parent
		if parentSite == nil {
			log.Ctx(ctx).WarnContext(ctx, "settings doc missing parent site", slog.String("doc", doc.Ref.Path))
			continue
		}
		if parentSite.Parent == nil || parentSite.Parent.ID != "sites" {
			log.Ctx(ctx).WarnContext(ctx, "settings doc missing parent sites collection", slog.String("doc", doc.Ref.Path))
			continue
		}
		siteID := parentSite.ID
		if siteID == "" {
			log.Ctx(ctx).WarnContext(ctx, "settings doc missing parent site id", slog.String("doc", doc.Ref.Path))
			continue
		}

		var version int
		if v, err := doc.DataAt("version"); err == nil {
			if vInt, ok := v.(int64); ok {
				version = int(vInt)
			}
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "settings doc missing json", slog.String("siteID", siteID))
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "settings doc json not string", slog.String("siteID", siteID))
			continue
		}

		var s types.Settings
		if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal settings json", slog.String("siteID", siteID), slog.Any("err", err))
			continue
		}

		settingsMap[siteID] = s
		versionsMap[siteID] = version
		updatedTimesMap[siteID] = doc.UpdateTime
	}

	return settingsMap, versionsMap, updatedTimesMap, nil
}

// GetUser retrieves a user from the "users" collection.
func (f *FirestoreProvider) GetUser(ctx context.Context, userID string) (types.User, error) {
	if userID == "" {
		return types.User{}, errors.New("userID cannot be empty")
	}
	doc, err := f.client.Collection("users").Doc(userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return types.User{}, fmt.Errorf("%w: %s", ErrUserNotFound, userID)
		}
		return types.User{}, fmt.Errorf("failed to get user %s: %w", userID, err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "user doc missing json", slog.String("userID", userID))
		return types.User{}, fmt.Errorf("user %s missing json: %w", userID, err)
	}
	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "user doc json not string", slog.String("userID", userID))
		return types.User{}, fmt.Errorf("user %s json not string", userID)
	}

	var user types.User
	if err := json.Unmarshal([]byte(jsonStr), &user); err != nil {
		return types.User{}, fmt.Errorf("failed to unmarshal user %s: %w", userID, err)
	}
	return user, nil
}

// UpsertPrices adds or updates multiple price records in the "price_history" sub-collection of the site.
func (f *FirestoreProvider) UpsertPrices(ctx context.Context, siteID string, prices []types.Price, version int) error {
	if len(prices) == 0 {
		return nil
	}

	coll, err := f.getCollection(siteID, "price_history")
	if err != nil {
		return err
	}

	// For a single item, use direct Set to avoid batch overhead
	if len(prices) == 1 {
		p := prices[0]
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal price: %w", err)
		}
		docID := p.TSStart.UTC().Format(time.RFC3339)
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert price: %w", err)
		}
		return nil
	}

	// For multiple items, use BulkWriter
	bw := f.client.BulkWriter(ctx)
	var endOnce sync.Once
	endBW := func() { endOnce.Do(func() { bw.End() }) }
	defer endBW()

	jobs := make([]*firestore.BulkWriterJob, 0, len(prices))

	for _, p := range prices {
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal price: %w", err)
		}

		docID := p.TSStart.UTC().Format(time.RFC3339)
		ref := coll.Doc(docID)
		job, err := bw.Set(ref, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue price: %w", err)
		}
		jobs = append(jobs, job)
	}

	endBW()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert prices: %w", err)
		}
	}

	return nil
}

// GetPriceHistory retrieves price records within the specified time range for a site.
func (f *FirestoreProvider) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	if !start.Before(end) {
		return nil, nil
	}

	coll, err := f.getCollection(siteID, "price_history")
	if err != nil {
		return nil, err
	}

	iter := coll.
		Where("timestamp", ">=", start).
		Where("timestamp", "<", end).
		OrderBy("timestamp", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var prices []types.Price
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating prices: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "price doc missing json", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("price document %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "price doc json not string", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID))
			return nil, fmt.Errorf("price document %s 'json' field is not string", doc.Ref.ID)
		}

		var p types.Price
		if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal price", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("failed to unmarshal price (id=%s): %w", doc.Ref.ID, err)
		}
		prices = append(prices, p)
	}
	return prices, nil
}

// GetLatestPriceHistoryTime retrieves the timestamp of the last stored price record for a site.
func (f *FirestoreProvider) GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	coll, err := f.getCollection(siteID, "price_history")
	if err != nil {
		return time.Time{}, 0, err
	}

	// firestore automatically creates indexes for top-level fields
	iter := coll.
		OrderBy("timestamp", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		return time.Time{}, 0, nil
	}
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to get latest price doc: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, doc.Ref.ID)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid price doc id %s: %w", doc.Ref.ID, err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	return ts, version, nil
}

// GetLatestWeatherTime retrieves the timestamp of the last stored weather record for a site, along with its update time.
func (f *FirestoreProvider) GetLatestWeatherTime(ctx context.Context, siteID string) (time.Time, time.Time, int, error) {
	coll, err := f.getCollection(siteID, "weather")
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	iter := coll.
		OrderBy("tsDayStart", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		return time.Time{}, time.Time{}, 0, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("failed to get latest weather doc: %w", err)
	}

	ts, err := time.Parse("2006-01-02", doc.Ref.ID)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid weather doc id %s: %w", doc.Ref.ID, err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	// Read tsUpdated if available
	var tsUpdated time.Time
	if tUp, err := doc.DataAt("tsUpdated"); err == nil {
		if tTime, ok := tUp.(time.Time); ok {
			tsUpdated = tTime
		}
	}

	if tsUpdated.IsZero() {
		// Fallback to reading it from JSON
		if val, err := doc.DataAt("json"); err == nil {
			if jsonStr, ok := val.(string); ok {
				var w types.Weather
				if err := json.Unmarshal([]byte(jsonStr), &w); err == nil {
					tsUpdated = w.TSUpdated
				}
			}
		}
	}

	return ts, tsUpdated, version, nil
}

// CreateSite creates a new site document in the "sites" collection.
// It fails atomically if a document with the same siteID already exists,
// preventing race conditions.
func (f *FirestoreProvider) CreateSite(ctx context.Context, siteID string, site types.Site) error {
	if siteID == "" {
		return errors.New("siteID cannot be empty")
	}
	siteJSON, err := json.Marshal(site)
	if err != nil {
		return fmt.Errorf("failed to marshal site %s: %w", siteID, err)
	}
	_, err = f.client.Collection("sites").Doc(siteID).Create(ctx, map[string]any{
		"json": string(siteJSON),
	})
	if err != nil {
		return fmt.Errorf("failed to create site %s: %w", siteID, err)
	}
	return nil
}

// UpdateSite updates a site document in the "sites" collection.
func (f *FirestoreProvider) UpdateSite(ctx context.Context, siteID string, site types.Site) error {
	if siteID == "" {
		return errors.New("siteID cannot be empty")
	}
	siteJSON, err := json.Marshal(site)
	if err != nil {
		return fmt.Errorf("failed to marshal site %s: %w", siteID, err)
	}
	_, err = f.client.Collection("sites").Doc(siteID).Set(ctx, map[string]any{
		"json": string(siteJSON),
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("failed to update site %s: %w", siteID, err)
	}
	return nil
}

// CreateUser creates a new user document in the "users" collection.
func (f *FirestoreProvider) CreateUser(ctx context.Context, user types.User) error {
	if user.ID == "" {
		return errors.New("userID cannot be empty")
	}
	userJSON, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user %s: %w", user.ID, err)
	}
	_, err = f.client.Collection("users").Doc(user.ID).Create(ctx, map[string]any{
		"json": string(userJSON),
	})
	if err != nil {
		return fmt.Errorf("failed to create user %s: %w", user.ID, err)
	}
	return nil
}

// UpdateUser updates an existing user document in the "users" collection.
func (f *FirestoreProvider) UpdateUser(ctx context.Context, user types.User) error {
	if user.ID == "" {
		return errors.New("userID cannot be empty")
	}
	userJSON, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user %s: %w", user.ID, err)
	}
	_, err = f.client.Collection("users").Doc(user.ID).Set(ctx, map[string]any{
		"json": string(userJSON),
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("failed to update user %s: %w", user.ID, err)
	}
	return nil
}

// ListUsers retrieves all users from the "users" collection.
func (f *FirestoreProvider) ListUsers(ctx context.Context) ([]types.User, error) {
	iter := f.client.Collection("users").Documents(ctx)
	defer iter.Stop()

	var users []types.User
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating users: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "user doc missing json", slog.String("userID", doc.Ref.ID))
			continue
		}
		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "user doc json not string", slog.String("userID", doc.Ref.ID))
			continue
		}

		var user types.User
		if err := json.Unmarshal([]byte(jsonStr), &user); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal user", slog.String("userID", doc.Ref.ID), slog.Any("err", err))
			continue
		}
		users = append(users, user)
	}
	return users, nil
}

// GetAdminSettings retrieves global system configuration for admin functions.
func (f *FirestoreProvider) GetAdminSettings(ctx context.Context) (types.AdminSettings, error) {
	doc, err := f.client.Collection("admin").Doc("settings").Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return types.AdminSettings{Aliases: make(map[string]string)}, nil
		}
		return types.AdminSettings{}, fmt.Errorf("failed to fetch admin settings: %w", err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		return types.AdminSettings{Aliases: make(map[string]string)}, nil
	}

	jsonStr, ok := val.(string)
	if !ok {
		return types.AdminSettings{}, fmt.Errorf("admin settings JSON is not a string")
	}

	var settings types.AdminSettings
	if err := json.Unmarshal([]byte(jsonStr), &settings); err != nil {
		return types.AdminSettings{}, fmt.Errorf("failed to unmarshal admin settings JSON: %w", err)
	}
	return settings, nil
}

// UpdateAdminSettings saves global system configuration for admin functions.
func (f *FirestoreProvider) UpdateAdminSettings(ctx context.Context, settings types.AdminSettings) error {
	if settings.Aliases == nil {
		settings.Aliases = make(map[string]string)
	}

	jsonBytes, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal admin settings: %w", err)
	}

	_, err = f.client.Collection("admin").Doc("settings").Set(ctx, map[string]any{
		"json": string(jsonBytes),
	})
	if err != nil {
		return fmt.Errorf("failed to save admin settings: %w", err)
	}
	return nil
}

// UpdateESSMockState saves the internal state of a mock ESS provider.

func (f *FirestoreProvider) UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal mock state %s: %w", siteID, err)
	}

	coll, err := f.getCollection(siteID, "mocks")
	if err != nil {
		return err
	}
	_, err = coll.Doc("mock_ess").Set(ctx, map[string]any{
		"json": string(stateJSON),
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("failed to save mock state: %w", err)
	}
	return nil
}

// GetESSMockState retrieves the internal state of a mock ESS provider.
func (f *FirestoreProvider) GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error) {
	coll, err := f.getCollection(siteID, "mocks")
	if err != nil {
		return types.ESSMockState{}, err
	}
	doc, err := coll.Doc("mock_ess").Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return types.ESSMockState{}, nil
		}
		return types.ESSMockState{}, fmt.Errorf("failed to fetch mock state: %w", err)
	}
	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "mock state doc missing json", slog.String("siteID", siteID))
		return types.ESSMockState{}, fmt.Errorf("mock state %s missing json: %w", siteID, err)
	}
	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "mock state doc json not string", slog.String("siteID", siteID))
		return types.ESSMockState{}, fmt.Errorf("mock state %s json not string", siteID)
	}

	var state types.ESSMockState
	if err := json.Unmarshal([]byte(jsonStr), &state); err != nil {
		return types.ESSMockState{}, fmt.Errorf("failed to unmarshal mock state %s: %w", siteID, err)
	}
	return state, nil
}

// InsertFeedback adds a new feedback record to the "feedback" collection.
func (f *FirestoreProvider) InsertFeedback(ctx context.Context, feedback types.Feedback) error {
	if feedback.ID == "" {
		return errors.New("feedback ID cannot be empty")
	}
	jsonBytes, err := json.Marshal(feedback)
	if err != nil {
		return fmt.Errorf("failed to marshal feedback: %w", err)
	}

	coll := f.client.Collection("feedback")
	_, err = coll.Doc(feedback.ID).Set(ctx, map[string]any{
		"json": string(jsonBytes),
		"id":   feedback.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to insert feedback: %w", err)
	}
	return nil
}

// ListFeedback retrieves feedback records, sorted by ID descending.
func (f *FirestoreProvider) ListFeedback(ctx context.Context, limit int, lastFeedbackID string) ([]types.Feedback, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be greater than 0")
	}
	coll := f.client.Collection("feedback")

	q := coll.OrderBy("id", firestore.Desc).Limit(limit)
	if lastFeedbackID != "" {
		q = q.StartAfter(lastFeedbackID)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var feedbacks []types.Feedback
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating feedback: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "feedback doc missing json", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "feedback doc json not string", slog.String("docID", doc.Ref.ID))
			continue
		}

		var fb types.Feedback
		if err := json.Unmarshal([]byte(jsonStr), &fb); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal feedback", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}
		feedbacks = append(feedbacks, fb)
	}

	return feedbacks, nil
}

// UpsertUtilityPrices adds or updates multiple price records for a utility.
func (f *FirestoreProvider) UpsertUtilityPrices(ctx context.Context, utilityID string, prices []types.PriceState, version int) error {
	if utilityID == "" {
		return errors.New("utilityID cannot be empty")
	}
	if len(prices) == 0 {
		return nil
	}

	coll := f.client.Collection("utilities").Doc(utilityID).Collection("hourly_prices")

	if len(prices) == 1 {
		p := prices[0]
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal utility price: %w", err)
		}
		docID := p.TSStart.UTC().Format(time.RFC3339)
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert utility price: %w", err)
		}
		return nil
	}

	bw := f.client.BulkWriter(ctx)
	var endOnce sync.Once
	endBW := func() { endOnce.Do(func() { bw.End() }) }
	defer endBW()

	jobs := make([]*firestore.BulkWriterJob, 0, len(prices))

	for _, p := range prices {
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal utility price: %w", err)
		}

		docID := p.TSStart.UTC().Format(time.RFC3339)
		ref := coll.Doc(docID)
		job, err := bw.Set(ref, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue utility price: %w", err)
		}
		jobs = append(jobs, job)
	}

	endBW()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert utility prices: %w", err)
		}
	}

	return nil
}

// GetUtilityPrices retrieves price records within the specified time range for a utility.
func (f *FirestoreProvider) GetUtilityPrices(ctx context.Context, utilityID string, start, end time.Time) ([]types.PriceState, error) {
	if utilityID == "" {
		return nil, errors.New("utilityID cannot be empty")
	}
	if !start.Before(end) {
		return nil, nil
	}

	coll := f.client.Collection("utilities").Doc(utilityID).Collection("hourly_prices")

	iter := coll.
		Where("timestamp", ">=", start).
		Where("timestamp", "<", end).
		OrderBy("timestamp", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var prices []types.PriceState
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating utility prices: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			continue
		}

		var p types.PriceState
		if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
			continue
		}
		prices = append(prices, p)
	}
	return prices, nil
}

// UpsertInterest adds or updates an interest submission record in the "interest" collection.
// It uses the email as the document ID to ensure one record per email.
func (f *FirestoreProvider) UpsertInterest(ctx context.Context, submission types.InterestSubmission) error {
	if submission.Email == "" {
		return fmt.Errorf("email cannot be empty for interest submission")
	}

	jsonBytes, err := json.Marshal(submission)
	if err != nil {
		return fmt.Errorf("failed to marshal interest submission: %w", err)
	}

	coll := f.client.Collection("interest")
	_, err = coll.Doc(submission.Email).Set(ctx, map[string]any{
		"json":      string(jsonBytes),
		"timestamp": submission.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert interest submission: %w", err)
	}
	return nil
}

// ListInterest retrieves interest submissions, sorted by timestamp descending.
func (f *FirestoreProvider) ListInterest(ctx context.Context, limit int) ([]types.InterestSubmission, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be greater than 0")
	}
	coll := f.client.Collection("interest")

	iter := coll.OrderBy("timestamp", firestore.Desc).Limit(limit).Documents(ctx)
	defer iter.Stop()

	var submissions []types.InterestSubmission
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating interest submissions: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "interest doc missing json", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "interest doc json not string", slog.String("docID", doc.Ref.ID))
			continue
		}

		var sub types.InterestSubmission
		if err := json.Unmarshal([]byte(jsonStr), &sub); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal interest submission", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}
		submissions = append(submissions, sub)
	}

	return submissions, nil
}

// DeleteInterest removes an interest submission record from the "interest" collection.
func (f *FirestoreProvider) DeleteInterest(ctx context.Context, email string) error {
	if email == "" {
		return fmt.Errorf("email cannot be empty for interest deletion")
	}

	coll := f.client.Collection("interest")
	_, err := coll.Doc(email).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete interest submission: %w", err)
	}
	return nil
}

// GetHistorySummaries retrieves the monthly summaries that overlap with a range of dates.
// We do not discard any dates from the history summaries, even if they fall outside
// of [start, end)
func (f *FirestoreProvider) GetHistorySummaries(ctx context.Context, siteID string, start, end time.Time) ([]types.HistorySummary, error) {
	if !start.Before(end) {
		return nil, nil
	}

	coll, err := f.getCollection(siteID, "history_summary")
	if err != nil {
		return nil, err
	}

	// Each summary document covers at most one calendar month. Any summary with data overlapping
	// [start, end) must have earliestDate < end. To avoid scanning all prior history, we bound the
	// lower end to the 1st of start's month. We only need to include the previous month if start is
	// within 23 hours of the month start (to account for site timezone offsets relative to UTC).
	// The alternative is calculating all of the months and then using GetAll but we would pay for
	// a lookup for each month even if the site has no data. By doing a query that can return nothing
	// we save Firestore reads.
	loc := start.Location()
	monthStart := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc)
	queryStart := monthStart
	if start.Sub(monthStart) < 23*time.Hour {
		queryStart = monthStart.AddDate(0, -1, 0)
	}

	iter := coll.
		Where("earliestDate", ">=", queryStart).
		Where("earliestDate", "<", end).
		OrderBy("earliestDate", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var summaries []types.HistorySummary
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating history summaries: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return nil, fmt.Errorf("history summary doc %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("history summary doc %s 'json' field is not a string", doc.Ref.ID)
		}

		var summary types.HistorySummary
		if err := json.Unmarshal([]byte(jsonStr), &summary); err != nil {
			return nil, fmt.Errorf("failed to unmarshal history summary for month %s: %w", doc.Ref.ID, err)
		}
		summaries = append(summaries, summary)
	}

	slices.SortFunc(summaries, func(a, b types.HistorySummary) int {
		return a.TSMonthStart.Compare(b.TSMonthStart)
	})

	return summaries, nil
}

func isTransactionExpiredErr(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.InvalidArgument && strings.Contains(st.Message(), "referenced transaction has expired") {
			return true
		}
	}
	return false
}

// UpdateHistorySummary reads the existing summary, merges it with newSummary (overwriting matching days), and saves it.
func (f *FirestoreProvider) UpdateHistorySummary(ctx context.Context, siteID string, month string, newSummary types.HistorySummary) (types.HistorySummary, error) {
	coll, err := f.getCollection(siteID, "history_summary")
	if err != nil {
		return types.HistorySummary{}, err
	}
	docRef := coll.Doc(month)

	maxRetries := 3
	backoff := 100 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		var mergedSummary types.HistorySummary

		err = f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			doc, err := tx.Get(docRef)
			var existing types.HistorySummary
			exists := true
			if err != nil {
				if status.Code(err) == codes.NotFound {
					exists = false
				} else {
					return err
				}
			}

			if exists {
				val, err := doc.DataAt("json")
				if err != nil {
					return fmt.Errorf("history summary doc missing 'json' field: %w", err)
				}
				jsonStr, ok := val.(string)
				if !ok {
					return fmt.Errorf("history summary doc 'json' field is not string")
				}
				if err := json.Unmarshal([]byte(jsonStr), &existing); err != nil {
					return fmt.Errorf("failed to unmarshal history summary: %w", err)
				}
			}

			// Merge Energy data, favoring the newSummary's days.
			energyMap := make(map[string]types.DailyEnergyStats)
			for _, e := range existing.Energy {
				energyMap[e.TSDayStart.Format("2006-01-02")] = e
			}
			for _, e := range newSummary.Energy {
				energyMap[e.TSDayStart.Format("2006-01-02")] = e
			}
			var mergedEnergy []types.DailyEnergyStats
			for _, e := range energyMap {
				mergedEnergy = append(mergedEnergy, e)
			}
			slices.SortFunc(mergedEnergy, func(a, b types.DailyEnergyStats) int {
				return a.TSDayStart.Compare(b.TSDayStart)
			})

			// Merge Weather data, favoring the newSummary's days.
			weatherMap := make(map[string]types.Weather)
			for _, w := range existing.Weather {
				weatherMap[w.TSDayStart.Format("2006-01-02")] = w
			}
			for _, w := range newSummary.Weather {
				weatherMap[w.TSDayStart.Format("2006-01-02")] = w
			}
			var mergedWeather []types.Weather
			for _, w := range weatherMap {
				mergedWeather = append(mergedWeather, w)
			}
			slices.SortFunc(mergedWeather, func(a, b types.Weather) int {
				return a.TSDayStart.Compare(b.TSDayStart)
			})

			var monthStart time.Time
			if monthTime, err := time.Parse("2006-01", month); err == nil {
				monthStart = monthTime
			}

			mergedSummary = types.HistorySummary{
				TSMonthStart: monthStart,
				Energy:       mergedEnergy,
				Weather:      mergedWeather,
			}

			// Determine latestDate and earliestDate.
			var latestDate, earliestDate time.Time
			if len(mergedSummary.Energy) > 0 {
				earliestDate = mergedSummary.Energy[0].TSDayStart
				latestDate = mergedSummary.Energy[0].TSDayStart
				for _, day := range mergedSummary.Energy {
					if day.TSDayStart.Before(earliestDate) {
						earliestDate = day.TSDayStart
					}
					if day.TSDayStart.After(latestDate) {
						latestDate = day.TSDayStart
					}
				}
			}

			jsonBytes, err := json.Marshal(mergedSummary)
			if err != nil {
				return fmt.Errorf("failed to marshal history summary: %w", err)
			}

			data := map[string]any{
				"json": string(jsonBytes),
			}
			if !latestDate.IsZero() {
				data["latestDate"] = latestDate
			}
			if !earliestDate.IsZero() {
				data["earliestDate"] = earliestDate
			}

			return tx.Set(docRef, data)
		})

		if err == nil {
			return mergedSummary, nil
		}

		lastErr = err
		if isTransactionExpiredErr(err) && attempt < maxRetries-1 {
			log.Ctx(ctx).WarnContext(ctx, "history summary transaction expired, retrying",
				slog.String("siteID", siteID),
				slog.String("month", month),
				slog.Int("attempt", attempt+1),
				slog.Any("error", err),
			)
			select {
			case <-ctx.Done():
				return types.HistorySummary{}, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
				continue
			}
		}
		break
	}

	return types.HistorySummary{}, lastErr
}

// DeleteSite deletes a site document and recursively deletes all its associated subcollections using BulkWriter, with config deleted last.
func (f *FirestoreProvider) DeleteSite(ctx context.Context, siteID string) error {
	if siteID == "" {
		return fmt.Errorf("siteID cannot be empty for site deletion")
	}

	// 1. Delete updateGroup field from config/settings so we don't accidentally run update anymore.
	configColl, err := f.getCollection(siteID, "config")
	if err != nil {
		return err
	}
	_, err = configColl.Doc("settings").Update(ctx, []firestore.Update{
		{Path: "updateGroup", Value: firestore.Delete},
	})
	if err != nil && status.Code(err) != codes.NotFound {
		log.Ctx(ctx).WarnContext(ctx, "failed to delete updateGroup field, proceeding with site deletion", slog.String("siteID", siteID), slog.Any("error", err))
	}

	// 2. Cleanup all of the collections, with config coming last
	subcollsIter := f.client.Collection("sites").Doc(siteID).Collections(ctx)
	var configCollRef *firestore.CollectionRef
	for {
		collRef, err := subcollsIter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to list subcollections: %w", err)
		}
		if collRef.ID == "config" {
			configCollRef = collRef
			continue
		}
		if err := f.deleteCollection(ctx, collRef); err != nil {
			return fmt.Errorf("failed to delete subcollection %s of site %s: %w", collRef.ID, siteID, err)
		}
	}
	if configCollRef != nil {
		if err := f.deleteCollection(ctx, configCollRef); err != nil {
			return fmt.Errorf("failed to delete config subcollection of site %s: %w", siteID, err)
		}
	}

	// 3. Delete the site document itself
	_, err = f.client.Collection("sites").Doc(siteID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete site document %s: %w", siteID, err)
	}

	return nil
}

// DeleteUser deletes the user document.
func (f *FirestoreProvider) DeleteUser(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("userID cannot be empty for user deletion")
	}
	_, err := f.client.Collection("users").Doc(userID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete user %s: %w", userID, err)
	}
	return nil
}

// deleteCollection deletes all documents in a collection using BulkWriter.
func (f *FirestoreProvider) deleteCollection(ctx context.Context, coll *firestore.CollectionRef) error {
	iter := coll.DocumentRefs(ctx)
	bw := f.client.BulkWriter(ctx)
	var endOnce sync.Once
	endBW := func() { endOnce.Do(func() { bw.End() }) }
	defer endBW()

	var jobs []*firestore.BulkWriterJob
	for {
		docRef, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return err
		}
		job, err := bw.Delete(docRef)
		if err != nil {
			return err
		}
		jobs = append(jobs, job)
	}
	endBW()
	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return err
		}
	}
	return nil
}

// AddUserPushSubscription adds or updates a push subscription on a user in a transaction.
func (f *FirestoreProvider) AddUserPushSubscription(ctx context.Context, userID string, sub types.PushSubscription) error {
	if userID == "" {
		return fmt.Errorf("userID cannot be empty")
	}
	docRef := f.client.Collection("users").Doc(userID)

	return f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return fmt.Errorf("%w: %s", ErrUserNotFound, userID)
			}
			return fmt.Errorf("failed to get user %s: %w", userID, err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return fmt.Errorf("user %s missing json: %w", userID, err)
		}
		jsonStr, ok := val.(string)
		if !ok {
			return fmt.Errorf("user %s json is not a string", userID)
		}

		var user types.User
		if err := json.Unmarshal([]byte(jsonStr), &user); err != nil {
			return fmt.Errorf("failed to unmarshal user %s: %w", userID, err)
		}

		// Update existing subscription or append
		found := false
		for i, existing := range user.Subscriptions {
			if existing.Endpoint == sub.Endpoint || (existing.ID != "" && existing.ID == sub.ID) {
				user.Subscriptions[i] = sub
				found = true
				break
			}
		}
		if !found {
			user.Subscriptions = append(user.Subscriptions, sub)
		}

		userJSON, err := json.Marshal(user)
		if err != nil {
			return fmt.Errorf("failed to marshal user %s: %w", userID, err)
		}

		return tx.Set(docRef, map[string]any{
			"json": string(userJSON),
		}, firestore.MergeAll)
	})
}

// RemoveUserPushSubscription removes a push subscription by endpoint from a user in a transaction.
// If the user has no remaining push subscriptions after removal, their userID is also cleaned up from
// site.Notifications across all sites they belong to.
func (f *FirestoreProvider) RemoveUserPushSubscription(ctx context.Context, userID string, endpoint string) error {
	if userID == "" {
		return fmt.Errorf("userID cannot be empty")
	}
	docRef := f.client.Collection("users").Doc(userID)

	var userSites []types.UserSite
	noRemainingSubscriptions := false

	err := f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return fmt.Errorf("%w: %s", ErrUserNotFound, userID)
			}
			return fmt.Errorf("failed to get user %s: %w", userID, err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return fmt.Errorf("user %s missing json: %w", userID, err)
		}
		jsonStr, ok := val.(string)
		if !ok {
			return fmt.Errorf("user %s json is not a string", userID)
		}

		var user types.User
		if err := json.Unmarshal([]byte(jsonStr), &user); err != nil {
			return fmt.Errorf("failed to unmarshal user %s: %w", userID, err)
		}

		filtered := slices.DeleteFunc(user.Subscriptions, func(sub types.PushSubscription) bool {
			return sub.Endpoint == endpoint || sub.ID == endpoint
		})
		user.Subscriptions = filtered
		if len(filtered) == 0 {
			noRemainingSubscriptions = true
			userSites = user.Sites
		}

		userJSON, err := json.Marshal(user)
		if err != nil {
			return fmt.Errorf("failed to marshal user %s: %w", userID, err)
		}

		return tx.Set(docRef, map[string]any{
			"json": string(userJSON),
		}, firestore.MergeAll)
	})
	if err != nil {
		return err
	}

	if noRemainingSubscriptions && len(userSites) > 0 {
		for _, s := range userSites {
			if s.ID != "" {
				err := f.UpdateSiteNotificationSettings(ctx, s.ID, userID, types.UserNotificationSettings{})
				if err != nil {
					log.Ctx(ctx).Error(
						"failed to cleanup user from site notifications",
						slog.String("siteID", s.ID),
						slog.String("userID", userID),
						slog.Any("error", err),
					)
				}
			}
		}
	}

	return nil
}

// UpdateSiteNotificationSettings updates notification preferences for a specific user on a site in a transaction.
// If settings is an empty struct (types.UserNotificationSettings{}), the user's notification entry is deleted.
func (f *FirestoreProvider) UpdateSiteNotificationSettings(ctx context.Context, siteID string, userID string, settings types.UserNotificationSettings) error {
	if siteID == "" {
		return fmt.Errorf("siteID cannot be empty")
	}
	if userID == "" {
		return fmt.Errorf("userID cannot be empty")
	}
	docRef := f.client.Collection("sites").Doc(siteID)

	return f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return fmt.Errorf("%w: %s", ErrSiteNotFound, siteID)
			}
			return fmt.Errorf("failed to get site %s: %w", siteID, err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return fmt.Errorf("site %s missing json: %w", siteID, err)
		}
		jsonStr, ok := val.(string)
		if !ok {
			return fmt.Errorf("site %s json is not a string", siteID)
		}

		var site types.Site
		if err := json.Unmarshal([]byte(jsonStr), &site); err != nil {
			return fmt.Errorf("failed to unmarshal site %s: %w", siteID, err)
		}

		if settings == (types.UserNotificationSettings{}) {
			if site.Notifications == nil {
				return nil
			}
			if _, exists := site.Notifications[userID]; !exists {
				return nil
			}
			delete(site.Notifications, userID)
			if len(site.Notifications) == 0 {
				site.Notifications = nil
			}
		} else {
			if site.Notifications != nil {
				if existing, exists := site.Notifications[userID]; exists && existing == settings {
					return nil
				}
			} else {
				site.Notifications = make(map[string]types.UserNotificationSettings)
			}
			site.Notifications[userID] = settings
		}

		siteJSON, err := json.Marshal(site)
		if err != nil {
			return fmt.Errorf("failed to marshal site %s: %w", siteID, err)
		}

		return tx.Set(docRef, map[string]any{
			"json": string(siteJSON),
		}, firestore.MergeAll)
	})
}

// GetNotificationLogs retrieves notification logs for a site within the [start, end) time range.
func (f *FirestoreProvider) GetNotificationLogs(ctx context.Context, siteID string, start, end time.Time) ([]types.NotificationLog, error) {
	if siteID == "" {
		return nil, fmt.Errorf("siteID cannot be empty")
	}
	coll, err := f.getCollection(siteID, "notification_logs")
	if err != nil {
		return nil, err
	}

	startUTC := start.UTC()
	endUTC := end.UTC()
	if !startUTC.Before(endUTC) {
		return nil, nil
	}

	// Notification log documents cover a calendar month and are partitioned strictly in UTC
	// (document IDs are 'YYYY-MM' in UTC, with earliestDate and latestDate stored in UTC).
	// Because everything is in UTC, there are no site timezone differences that could cause a log
	// with TSCreated >= startUTC to reside in a preceding month. Thus, queryStart is simply the 1st
	// of startUTC's month at 00:00:00 UTC without ever needing to look back to the previous month.
	queryStart := time.Date(startUTC.Year(), startUTC.Month(), 1, 0, 0, 0, 0, time.UTC)

	iter := coll.
		Where("earliestDate", ">=", queryStart).
		Where("earliestDate", "<", endUTC).
		OrderBy("earliestDate", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var allLogs []types.NotificationLog
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating notification logs: %w", err)
		}
		val, err := doc.DataAt("json")
		if err != nil {
			continue
		}
		jsonStr, ok := val.(string)
		if !ok {
			continue
		}
		var monthlyLogs types.MonthlyNotificationLogs
		if err := json.Unmarshal([]byte(jsonStr), &monthlyLogs); err != nil {
			continue
		}
		for _, l := range monthlyLogs.Logs {
			if !l.TSCreated.Before(startUTC) && l.TSCreated.Before(endUTC) {
				allLogs = append(allLogs, l)
			}
		}
	}

	slices.SortFunc(allLogs, func(a, b types.NotificationLog) int {
		return a.TSCreated.Compare(b.TSCreated)
	})

	return allLogs, nil
}

// AppendNotificationLog appends a notification log entry to the monthly log document in a transaction.
func (f *FirestoreProvider) AppendNotificationLog(ctx context.Context, siteID string, logEntry types.NotificationLog) error {
	if siteID == "" {
		return fmt.Errorf("siteID cannot be empty")
	}
	coll, err := f.getCollection(siteID, "notification_logs")
	if err != nil {
		return err
	}

	ts := logEntry.TSCreated.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
		logEntry.TSCreated = ts
	}

	month := ts.Format("2006-01")
	monthStart := time.Date(ts.Year(), ts.Month(), 1, 0, 0, 0, 0, time.UTC)
	docRef := coll.Doc(month)

	return f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		var monthlyLogs types.MonthlyNotificationLogs
		if err != nil {
			if status.Code(err) == codes.NotFound {
				monthlyLogs = types.MonthlyNotificationLogs{
					TSMonthStart: monthStart,
					Logs:         []types.NotificationLog{},
				}
			} else {
				return fmt.Errorf("failed to get notification logs doc %s: %w", month, err)
			}
		} else {
			val, err := doc.DataAt("json")
			if err != nil {
				return fmt.Errorf("notification logs doc missing json: %w", err)
			}
			jsonStr, ok := val.(string)
			if !ok {
				return fmt.Errorf("notification logs doc json not string")
			}
			if err := json.Unmarshal([]byte(jsonStr), &monthlyLogs); err != nil {
				return fmt.Errorf("failed to unmarshal monthly notification logs: %w", err)
			}
		}

		monthlyLogs.Logs = append(monthlyLogs.Logs, logEntry)

		var earliestDate, latestDate time.Time
		if len(monthlyLogs.Logs) > 0 {
			earliestDate = monthlyLogs.Logs[0].TSCreated.UTC()
			latestDate = monthlyLogs.Logs[0].TSCreated.UTC()
			for _, l := range monthlyLogs.Logs {
				if l.TSCreated.Before(earliestDate) {
					earliestDate = l.TSCreated.UTC()
				}
				if l.TSCreated.After(latestDate) {
					latestDate = l.TSCreated.UTC()
				}
			}
		}

		jsonBytes, err := json.Marshal(monthlyLogs)
		if err != nil {
			return fmt.Errorf("failed to marshal monthly notification logs: %w", err)
		}

		data := map[string]any{
			"json": string(jsonBytes),
		}
		if !earliestDate.IsZero() {
			data["earliestDate"] = earliestDate
		}
		if !latestDate.IsZero() {
			data["latestDate"] = latestDate
		}

		return tx.Set(docRef, data)
	})
}

// RecordNotificationClick marks a notification log entry as clicked in a transaction.
func (f *FirestoreProvider) RecordNotificationClick(
	ctx context.Context,
	siteID string,
	month string,
	logID string,
	clickedAt time.Time,
) error {
	if siteID == "" {
		return fmt.Errorf("siteID cannot be empty")
	}
	coll, err := f.getCollection(siteID, "notification_logs")
	if err != nil {
		return err
	}

	ts := clickedAt.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	if month == "" {
		return fmt.Errorf("month cannot be empty")
	}
	docRef := coll.Doc(month)

	return f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return nil
			}
			return fmt.Errorf("failed to get notification logs doc %s: %w", month, err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return fmt.Errorf("notification logs doc missing json: %w", err)
		}
		jsonStr, ok := val.(string)
		if !ok {
			return fmt.Errorf("notification logs doc json not string")
		}

		var monthlyLogs types.MonthlyNotificationLogs
		if err := json.Unmarshal([]byte(jsonStr), &monthlyLogs); err != nil {
			return fmt.Errorf("failed to unmarshal monthly notification logs: %w", err)
		}

		found := false
		for i, entry := range monthlyLogs.Logs {
			if entry.ID == logID {
				if entry.Clicked {
					return nil
				}
				monthlyLogs.Logs[i].Clicked = true
				monthlyLogs.Logs[i].TSClicked = ts
				found = true
				break
			}
		}
		if !found {
			return nil
		}

		jsonBytes, err := json.Marshal(monthlyLogs)
		if err != nil {
			return fmt.Errorf("failed to marshal monthly notification logs: %w", err)
		}

		return tx.Set(docRef, map[string]any{
			"json": string(jsonBytes),
		}, firestore.MergeAll)
	})
}
